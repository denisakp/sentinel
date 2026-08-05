package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	backup "github.com/denisakp/sentinel/internal/domain/backup"
	dbprobe "github.com/denisakp/sentinel/internal/adapters/db_probe"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/utils"
	"github.com/denisakp/sentinel/internal/adapters/dump"
	"github.com/denisakp/sentinel/internal/adapters/dump/mariadb"
	"github.com/denisakp/sentinel/internal/adapters/dump/mongo"
	"github.com/denisakp/sentinel/internal/adapters/dump/mysql"
	"github.com/denisakp/sentinel/internal/adapters/dump/pg"
	"github.com/spf13/cobra"
)

var dbType, host, port, user, password, database,
	pgOutFormat, pgCompressionAlgo, uri,
	output, storageType, localPath, gDriveSaFile, gDriveFolderId,
	gcsBucket, gcsProjectID, gcsCredentialsFile,
	awsSecretAccessKey, awsAccessKeyID, awsRegion, awsBucket, awsBucketEndpoint,
	additionalArgs, configPath string
var compress bool
var pgCompressionLevel int
var err error

type backupExecutionMode string

type backupRunOptions struct {
	forceFull bool
}

const (
	executionModeConfig    backupExecutionMode = "config"
	executionModeScheduled backupExecutionMode = "scheduled"
)

// passwordFlagDeprecationSuffix is the message body appended to pflag's
// "Flag --password has been deprecated, " prefix on use.
const passwordFlagDeprecationSuffix = "will be removed in the next minor release; passing a password on the command line exposes it via `ps`, /proc/<pid>/cmdline, and shell history. Use --password-env <VAR>, --password-file <PATH>, or set databases.<id>.password_env in the config file instead."

var passwordFilePermWarnOnce sync.Once

var backupForceFullJob string
var backupChainStatusJob string
var backupChainListID string

var backupForceFullCmd = &cobra.Command{
	Use:   "force-full",
	Short: "Run a forced full backup for a configured job",
	Long:  "Run a full backup immediately for a configured job and reset incremental chain state.",
	RunE:  handleBackupForceFull,
}

var backupChainStatusCmd = &cobra.Command{
	Use:   "chain-status",
	Short: "Show incremental chain status for a backup job",
	Long:  "Display active chain id, depth, and latest backup metadata for a configured backup job.",
	RunE:  handleBackupChainStatus,
}

var backupChainListCmd = &cobra.Command{
	Use:   "chain-list",
	Short: "List backups belonging to a chain",
	Long:  "List backup executions that belong to the provided incremental chain id.",
	RunE:  handleBackupChainList,
}

var BackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Run a database backup",
	Long:  "Run a database backup using CLI flags or a YAML config.\n\nExamples:\n  sentinel backup --config sentinel.yaml\n  sentinel backup --type postgres --host db --port 5432 --user backup --database app",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ = cmd.Flags().GetString("config")
		cfg, cfgErr := LoadAndValidateConfig(configPath)
		if cfgErr == nil {
			if err := runBackupJobsFromConfig(cmd, cfg); err != nil {
				cmd.PrintErrln(err)
				return err
			}
			return nil
		}
		if configPath != "" {
			cmd.PrintErrln(cfgErr)
			return cfgErr
		}

		dbType, _ = cmd.Flags().GetString("type")
		if dbType == "" {
			cmd.PrintErrln(cfgErr)
			return cfgErr
		}

		// validate the database type
		if err = backup.ValidateDbType(dbType); err != nil {
			cmd.PrintErrln(err)
			return err
		}

		host, _ = cmd.Flags().GetString("host")           // get the host flag value
		port, _ = cmd.Flags().GetString("port")           // get the port flag value
		user, _ = cmd.Flags().GetString("user")           // get the user flag value
		password, _ = cmd.Flags().GetString("password")   // get the password flag value
		database, _ = cmd.Flags().GetString("database")   // get the database flag value
		additionalArgs, _ = cmd.Flags().GetString("args") // get the args flag value

		// storage
		storageType, _ = cmd.Flags().GetString("storage")  // get the storage flag value
		localPath, _ = cmd.Flags().GetString("local-path") // get the local-path flag value
		output, _ = cmd.Flags().GetString("output")        // get the output flag value
		// google drive
		gDriveFolderId, _ = cmd.Flags().GetString("gdrive-folder-id")
		gDriveSaFile, _ = cmd.Flags().GetString("gdrive-sa-file")
		// google cloud storage
		gcsBucket, _ = cmd.Flags().GetString("gcs-bucket")
		gcsProjectID, _ = cmd.Flags().GetString("gcs-project-id")
		gcsCredentialsFile, _ = cmd.Flags().GetString("gcs-credentials-file")
		//aws s3 storage
		awsBucket, _ = cmd.Flags().GetString("aws-bucket")
		awsRegion, _ = cmd.Flags().GetString("aws-region")
		awsBucketEndpoint, _ = cmd.Flags().GetString("aws-bucket-endpoint")
		awsAccessKeyID, _ = cmd.Flags().GetString("aws-access-key-id")
		awsSecretAccessKey, _ = cmd.Flags().GetString("aws-secret")

		params := &storage.Params{
			StorageType:          storageType,
			LocalPath:            localPath,
			OutName:              output,
			GoogleServiceAccount: gDriveSaFile,
			GoogleDriveFolderId:  gDriveFolderId,
			GCSBucket:            gcsBucket,
			GCSProjectID:         gcsProjectID,
			GCSCredentialsFile:   gcsCredentialsFile,
			AWSBucket:            awsBucket,
			AWSRegion:            awsRegion,
			AWSBucketEndpoint:    awsBucketEndpoint,
			AWSAccessKeyID:       awsAccessKeyID,
			AWSSecretAccessKey:   awsSecretAccessKey,
		}

		if err = storage.ValidateStorage(params); err != nil {
			cmd.PrintErrln(err)
			return err
		}

		// validate the storage parameters

		if dbType == "postgres" {
			compress, _ = cmd.Flags().GetBool("compress")                       // get the compress flag value
			pgOutFormat, _ = cmd.Flags().GetString("pg-out-format")             // get the pg-out-format flag value
			pgCompressionAlgo, _ = cmd.Flags().GetString("pg-compression-algo") // get the pg-compression-algo flag value
			pgCompressionLevel, _ = cmd.Flags().GetInt("pg-compression-level")  // get the pg-compression-level flag value

			pda := &pg.PgDumpArgs{
				Host:                 host,
				Port:                 port,
				Username:             user,
				Password:             password,
				Database:             database,
				PgOutFormat:          pgOutFormat,
				Compress:             compress,
				CompressionAlgorithm: pgCompressionAlgo,
				CompressionLevel:     pgCompressionLevel,
				AdditionalArgs:       additionalArgs,
				Storage:              params,
			}

			_, err = pg.Backup(dbprobe.NewAdapter(), pda)
			if err != nil {
				cmd.PrintErrln(err)
				return err
			}
		}

		if dbType == "mysql" {
			mda := &mysql.MySqlDumpArgs{
				Host:           host,
				Port:           port,
				Username:       user,
				Password:       password,
				Database:       database,
				AdditionalArgs: additionalArgs,
				Storage:        params,
			}

			_, err = mysql.Backup(dbprobe.NewAdapter(), mda)
			if err != nil {
				cmd.PrintErrln(err)
				return err
			}
		}

		if dbType == "mariadb" {
			mda := &mariadb.MariaDBDumpArgs{
				Host:           host,
				Port:           port,
				Username:       user,
				Password:       password,
				Database:       database,
				AdditionalArgs: additionalArgs,
				Storage:        params,
			}

			_, err = mariadb.Backup(dbprobe.NewAdapter(), mda)
			if err != nil {
				cmd.PrintErrln(err)
				return err
			}
		}

		if dbType == "mongodb" {
			compress, _ = cmd.Flags().GetBool("compress") // get the compress flag value
			uri, _ := cmd.Flags().GetString("uri")        // get the uri flag value

			da := &mongo.DumpMongoArgs{
				Compress:       compress,
				AdditionalArgs: additionalArgs,
				Uri:            uri,
				Storage:        params,
			}

			_, err = mongo.Backup(dbprobe.NewAdapter(), da)
			if err != nil {
				cmd.PrintErrln(err)
				return err
			}
		}
		return nil
	},
}

func init() {
	BackupCmd.AddCommand(backupForceFullCmd, backupChainStatusCmd, backupChainListCmd)

	BackupCmd.Flags().StringVarP(&dbType, "type", "t", "", "Database type (mysql, postgres, mariadb, mongodb)")
	BackupCmd.Flags().StringVar(&configPath, "config", "", "Path to YAML configuration file (preferred)")

	BackupCmd.Flags().StringVarP(&host, "host", "H", "127.0.0.1", "Database host")
	BackupCmd.Flags().StringVarP(&port, "port", "P", "", "Database port")
	BackupCmd.Flags().StringVarP(&user, "user", "u", "root", "Database user")
	BackupCmd.Flags().StringVarP(&password, "password", "p", "", "Database password")
	BackupCmd.Flags().String("password-env", "", "Name of environment variable holding the database password")
	BackupCmd.Flags().String("password-file", "", "Path to a file whose first line is the database password")
	_ = BackupCmd.Flags().MarkDeprecated("password", passwordFlagDeprecationSuffix)
	BackupCmd.Flags().StringVarP(&database, "database", "d", "", "Database name")

	BackupCmd.Flags().BoolVarP(&compress, "compress", "c", false, "Compress the backup")
	BackupCmd.Flags().StringVar(&additionalArgs, "args", "", "Additional arguments for the dump command")

	// postgresql flags
	BackupCmd.Flags().StringVar(&pgOutFormat, "pg-out-format", "", "PostgresSQL output format [p (plain), c (custom), d (directory), t (tar)] ")
	BackupCmd.Flags().StringVar(&pgCompressionAlgo, "pg-compression-algo", "", "PostgresSQL compression algorithm [gzip, lz4, zstd, none]")
	BackupCmd.Flags().IntVar(&pgCompressionLevel, "pg-compression-level", 1, "PostgresSQL compression level [1-9]")

	// mongodb flags
	BackupCmd.Flags().StringVarP(&uri, "uri", "", "mongodb://localhost:27017", "MongoDB URI")

	// storage flags
	BackupCmd.Flags().StringVarP(&storageType, "storage", "s", "local", "storage type (local, s3, gcs, google-drive)")
	BackupCmd.Flags().StringVarP(&localPath, "local-path", "", "", "Local path to store the backup")
	BackupCmd.Flags().StringVarP(&output, "output", "o", "", "Output name")
	// google cloud storage
	BackupCmd.Flags().StringVarP(&gcsBucket, "gcs-bucket", "", "", "Google Cloud Storage bucket name")
	BackupCmd.Flags().StringVarP(&gcsProjectID, "gcs-project-id", "", "", "Google Cloud project ID (optional)")
	BackupCmd.Flags().StringVarP(&gcsCredentialsFile, "gcs-credentials-file", "", "", "Google Cloud service account key file")
	//google drive
	BackupCmd.Flags().StringVarP(&gDriveFolderId, "gdrive-folder-id", "", "", "Google Drive folder ID")
	BackupCmd.Flags().StringVarP(&gDriveSaFile, "gdrive-sa-file", "", "", "Google Drive service account file")
	//aws s3 storage
	BackupCmd.Flags().StringVarP(&awsBucket, "aws-bucket", "", "", "AWS S3 bucket name")
	BackupCmd.Flags().StringVarP(&awsRegion, "aws-region", "", "us-east-1", "AWS region")
	BackupCmd.Flags().StringVarP(&awsBucketEndpoint, "aws-bucket-endpoint", "", "", "AWS S3 bucket endpoint")
	BackupCmd.Flags().StringVarP(&awsAccessKeyID, "aws-access-key-id", "", "", "AWS access key ID")
	BackupCmd.Flags().StringVarP(&awsSecretAccessKey, "aws-secret", "", "", "AWS secret")

	backupForceFullCmd.Flags().StringVar(&backupForceFullJob, "job", "", "Configured backup job name")
	backupForceFullCmd.MarkFlagRequired("job")

	backupChainStatusCmd.Flags().StringVar(&backupChainStatusJob, "job", "", "Configured backup job name")
	backupChainStatusCmd.MarkFlagRequired("job")

	backupChainListCmd.Flags().StringVar(&backupChainListID, "chain-id", "", "Incremental chain ID")
	backupChainListCmd.MarkFlagRequired("chain-id")

	// required args are enforced at runtime when --config is not provided
}

func runBackupJobsFromConfig(cmd *cobra.Command, cfg *config.Configuration) error {
	for _, job := range cfg.Databases {
		if job.Enabled != nil && !*job.Enabled {
			continue
		}
		if err := executeBackupJobWithMode(cmd, cfg, job, executionModeConfig, backupRunOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func executeBackupJobWithMode(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, mode backupExecutionMode, opts backupRunOptions) error {
	if err := applyCLIOverrides(cmd, &job); err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	if job.Database == "*" {
		return executeAutoDiscovery(cmd, cfg, job, mode, opts)
	}

	return executeSingleBackupJob(cmd, cfg, job, mode, opts)
}

// executeSingleBackupJob is the carved driving-adapter body: parse flags →
// translate to domain Job → factory → Executor.Run.
// Orchestration (dump, manifest, encryption, record, notify) lives in
// internal/domain/backup.Executor.
func executeSingleBackupJob(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, mode backupExecutionMode, opts backupRunOptions) error {
	storageParams := config.BuildStorageParams(job)
	if err := applyStorageOverrides(cmd, storageParams, &job); err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}
	applyScheduledOutputName(job, storageParams, mode, time.Now())

	engineOpts, dumps, err := buildSingleDump(cmd, job, storageParams)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	be, err := NewBackupExecutorFromConfig(cfg, job, storageParams, mode == executionModeScheduled, opts.forceFull, engineOpts, dumps)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	res, backupErr := be.exec.Run(context.Background(), be.job)
	reportBackupRunDiagnostics(cmd, be, res)
	if mode == executionModeScheduled && backupErr == nil {
		runScheduledRetention(cmd, cfg, job)
	}

	return backupErr
}

// buildSingleDump resolves the engine arg bag (config + CLI flag overrides)
// and the matching ports.DumpBuilder for a single-database job.
func buildSingleDump(cmd *cobra.Command, job config.BackupJob, storageParams *storage.Params) (ports.EngineOptions, ports.DumpBuilder, error) {
	additionalArgs := config.BuildAdditionalArgs(job)
	if cmd.Flags().Changed("args") {
		additionalArgs, _ = cmd.Flags().GetString("args")
	}

	password, err := resolvePassword(cmd, job)
	if err != nil {
		return nil, nil, err
	}

	spec := config.BuildDumpJobSpec(job, password, additionalArgs)
	factory, err := dump.NewArgsFactory(job.Type)
	if err != nil {
		return nil, nil, err
	}
	opts, err := factory.BuildDumpArgs(spec)
	if err != nil {
		return nil, nil, err
	}

	switch job.Type {
	case "postgres":
		pgArgs := opts.(*pg.PgDumpArgs)
		pgArgs.Storage = storageParams
		if cmd.Flags().Changed("pg-out-format") {
			pgArgs.PgOutFormat, _ = cmd.Flags().GetString("pg-out-format")
		}
		if cmd.Flags().Changed("compress") {
			pgArgs.Compress, _ = cmd.Flags().GetBool("compress")
		}
		if cmd.Flags().Changed("pg-compression-algo") {
			pgArgs.CompressionAlgorithm, _ = cmd.Flags().GetString("pg-compression-algo")
		}
		if cmd.Flags().Changed("pg-compression-level") {
			pgArgs.CompressionLevel, _ = cmd.Flags().GetInt("pg-compression-level")
		}
		return pgArgs, pg.NewBuilder(dbprobe.NewAdapter()), nil
	case "mysql":
		mysqlArgs := opts.(*mysql.MySqlDumpArgs)
		mysqlArgs.Storage = storageParams
		return mysqlArgs, mysql.NewBuilder(dbprobe.NewAdapter()), nil
	case "mariadb":
		mariaArgs := opts.(*mariadb.MariaDBDumpArgs)
		mariaArgs.Storage = storageParams
		return mariaArgs, mariadb.NewBuilder(dbprobe.NewAdapter()), nil
	case "mongodb":
		mongoArgs := opts.(*mongo.DumpMongoArgs)
		mongoArgs.Storage = storageParams
		if cmd.Flags().Changed("compress") {
			mongoArgs.Compress, _ = cmd.Flags().GetBool("compress")
		}
		if cmd.Flags().Changed("uri") {
			mongoArgs.Uri, _ = cmd.Flags().GetString("uri")
		}
		return mongoArgs, mongo.NewBuilder(dbprobe.NewAdapter()), nil
	default:
		return nil, nil, fmt.Errorf("unsupported database type '%s'", job.Type)
	}
}

// reportBackupRunDiagnostics prints the non-fatal Run signals with the
// pre-carve wording.
func reportBackupRunDiagnostics(cmd *cobra.Command, be *backupExecution, res backup.RunResult) {
	for _, w := range res.Warnings {
		fmt.Println(w)
	}
	if res.RecordErr != nil {
		cmd.PrintErrln("monitor error:", res.RecordErr)
	}
	if be.notifWarn != nil {
		cmd.PrintErrln("notification error:", be.notifWarn)
	}
	if res.NotifyErr != nil {
		cmd.PrintErrln("notification error:", res.NotifyErr)
	}
}

func executeAutoDiscovery(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, mode backupExecutionMode, opts backupRunOptions) error {
	strategy := job.Strategy
	if strategy == "" {
		strategy = "individual"
	}

	if strategy == "single" {
		return executeAutoDiscoverySingle(cmd, cfg, job, mode, opts)
	}

	return executeAutoDiscoveryIndividual(cmd, cfg, job, mode, opts)
}

func executeAutoDiscoveryIndividual(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, mode backupExecutionMode, opts backupRunOptions) error {
	databaseNames, err := listDatabases(job)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	for _, dbName := range backup.FilterExcludedSources(databaseNames, job.Exclude) {
		childJob := job
		childJob.Database = dbName
		childJob.Strategy = ""
		if err := executeSingleBackupJob(cmd, cfg, childJob, mode, opts); err != nil {
			return err
		}
	}

	return nil
}

// executeAutoDiscoverySingle handles the "single" auto-discovery strategy
// (one dump-all artifact). Same factory + Run shape as
// executeSingleBackupJob; the dump-all entry points (no port-side Build)
// are adapted via a dumpBuilderFunc closure.
func executeAutoDiscoverySingle(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, mode backupExecutionMode, opts backupRunOptions) error {
	storageParams := config.BuildStorageParams(job)
	if err := applyStorageOverrides(cmd, storageParams, &job); err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}
	applyScheduledOutputName(job, storageParams, mode, time.Now())

	engineOpts, dumps, err := buildAllDump(cmd, job, storageParams)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	be, err := NewBackupExecutorFromConfig(cfg, job, storageParams, mode == executionModeScheduled, opts.forceFull, engineOpts, dumps)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	res, backupErr := be.exec.Run(context.Background(), be.job)
	reportBackupRunDiagnostics(cmd, be, res)
	if mode == executionModeScheduled && backupErr == nil {
		runScheduledRetention(cmd, cfg, job)
	}

	return backupErr
}

// buildAllDump resolves the engine arg bag + dump-all builder for the
// auto-discovery "single" strategy.
func buildAllDump(cmd *cobra.Command, job config.BackupJob, storageParams *storage.Params) (ports.EngineOptions, ports.DumpBuilder, error) {
	additionalArgs := config.BuildAdditionalArgs(job)
	if cmd.Flags().Changed("args") {
		additionalArgs, _ = cmd.Flags().GetString("args")
	}

	password, err := resolvePassword(cmd, job)
	if err != nil {
		return nil, nil, err
	}

	spec := config.BuildDumpJobSpec(job, password, additionalArgs)
	factory, err := dump.NewArgsFactory(job.Type)
	if err != nil {
		return nil, nil, err
	}
	opts, err := factory.BuildDumpArgs(spec)
	if err != nil {
		return nil, nil, err
	}

	switch job.Type {
	case "postgres":
		pgArgs := opts.(*pg.PgDumpArgs)
		pgArgs.Storage = storageParams
		allArgs := &pg.PgDumpAllArgs{
			Host:           pgArgs.Host,
			Port:           pgArgs.Port,
			Username:       pgArgs.Username,
			Password:       pgArgs.Password,
			AdditionalArgs: pgArgs.AdditionalArgs,
			Storage:        pgArgs.Storage,
		}
		return pgArgs, dumpBuilderFunc(func(_ ports.BuildContext) (ports.BuildResult, error) {
			digest, err := pg.BackupAll(allArgs)
			return ports.BuildResult{Digest: digest}, err
		}), nil
	case "mysql":
		mysqlArgs := opts.(*mysql.MySqlDumpArgs)
		mysqlArgs.Storage = storageParams
		allArgs := &mysql.MySqlDumpAllArgs{
			Host:           mysqlArgs.Host,
			Port:           mysqlArgs.Port,
			Username:       mysqlArgs.Username,
			Password:       mysqlArgs.Password,
			AdditionalArgs: mysqlArgs.AdditionalArgs,
			Storage:        mysqlArgs.Storage,
		}
		return mysqlArgs, dumpBuilderFunc(func(_ ports.BuildContext) (ports.BuildResult, error) {
			digest, err := mysql.BackupAll(allArgs)
			return ports.BuildResult{Digest: digest}, err
		}), nil
	case "mariadb":
		mariaArgs := opts.(*mariadb.MariaDBDumpArgs)
		mariaArgs.Storage = storageParams
		allArgs := &mariadb.MariaDBDumpAllArgs{
			Host:           mariaArgs.Host,
			Port:           mariaArgs.Port,
			Username:       mariaArgs.Username,
			Password:       mariaArgs.Password,
			AdditionalArgs: mariaArgs.AdditionalArgs,
			Storage:        mariaArgs.Storage,
		}
		return mariaArgs, dumpBuilderFunc(func(_ ports.BuildContext) (ports.BuildResult, error) {
			digest, err := mariadb.BackupAll(allArgs)
			return ports.BuildResult{Digest: digest}, err
		}), nil
	case "mongodb":
		mongoArgs := opts.(*mongo.DumpMongoArgs)
		mongoArgs.Storage = storageParams
		mongoArgs.Database = ""
		if cmd.Flags().Changed("compress") {
			mongoArgs.Compress, _ = cmd.Flags().GetBool("compress")
		}
		if cmd.Flags().Changed("uri") {
			mongoArgs.Uri, _ = cmd.Flags().GetString("uri")
		}
		return mongoArgs, mongo.NewBuilder(dbprobe.NewAdapter()), nil
	default:
		return nil, nil, fmt.Errorf("unsupported database type '%s'", job.Type)
	}
}

func applyScheduledOutputName(job config.BackupJob, storageParams *storage.Params, mode backupExecutionMode, timestamp time.Time) {
	if mode != executionModeScheduled || storageParams == nil {
		return
	}

	pgOutFormat := ""
	if value, ok := job.DatabaseOptions["pg_out_format"].(string); ok {
		pgOutFormat = value
	}
	canonicalExt := backup.CanonicalScheduledExtension(job.Type, pgOutFormat)
	storageParams.OutName = utils.BuildScheduledOutName(storageParams.OutName, canonicalExt, job.Name, timestamp)
}

func runScheduledRetention(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob) {
	if cfg == nil {
		return
	}
	if !retentionEnabled(job.Retention) {
		return
	}

	gfsSuffix := ""
	if g := job.Retention.GFS; g != nil {
		gfsSuffix = fmt.Sprintf(", gfs=[daily=%d weekly=%d monthly=%d yearly=%d]", g.KeepDaily, g.KeepWeekly, g.KeepMonthly, g.KeepYearly)
	}
	cmd.Printf("Retention: evaluating backup '%s' (keep_last=%d, keep_days=%d%s)\n", job.Name, job.Retention.KeepLast, job.Retention.KeepDays, gfsSuffix)

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		cmd.PrintErrf("warning: retention monitor init failed for backup '%s': %v\n", job.Name, err)
		return
	}
	defer func() {
		if closeErr := mon.Close(); closeErr != nil {
			cmd.PrintErrf("warning: retention monitor close failed for backup '%s': %v\n", job.Name, closeErr)
		}
	}()

	deleted, err := applyJobRetention(context.Background(), cfg, mon, job.Name, false)
	if err != nil {
		cmd.PrintErrf("warning: retention apply failed for backup '%s': %v\n", job.Name, err)
		return
	}

	if len(deleted) > 0 {
		cmd.Printf("Retention: deleted %d artifact(s) for backup '%s'\n", len(deleted), job.Name)
	}
}

// listDatabases enumerates databases for auto-discovery. SQL engines go
// through the domain source resolver over ports.DBProber; Mongo stays
// adapter-direct because the prober port's
// DatabaseConfig carries no URI.
func listDatabases(job config.BackupJob) ([]string, error) {
	switch job.Type {
	case "postgres", "mysql", "mariadb":
		password, err := config.ResolveJobPassword(job)
		if err != nil {
			return nil, err
		}
		djob := backup.Job{
			Engine: job.Type,
			DBConn: ports.DatabaseConfig{
				Type:     job.Type,
				Host:     job.Host,
				Port:     job.Port,
				Username: job.Username,
				Password: password,
			},
		}
		return backup.ListSQLSources(context.Background(), dbprobe.NewAdapter(), djob)
	case "mongodb":
		return dbprobe.ListMongoDatabases(job.URI)
	default:
		return nil, fmt.Errorf("unsupported database type '%s'", job.Type)
	}
}

func applyCLIOverrides(cmd *cobra.Command, job *config.BackupJob) error {
	if cmd.Flags().Changed("type") {
		value, _ := cmd.Flags().GetString("type")
		if value != "" {
			job.Type = value
		}
	}
	if cmd.Flags().Changed("host") {
		job.Host, _ = cmd.Flags().GetString("host")
	}
	if cmd.Flags().Changed("port") {
		value, _ := cmd.Flags().GetString("port")
		if value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid port '%s'", value)
			}
			job.Port = parsed
		}
	}
	if cmd.Flags().Changed("user") {
		job.Username, _ = cmd.Flags().GetString("user")
	}
	if cmd.Flags().Changed("database") {
		job.Database, _ = cmd.Flags().GetString("database")
	}
	if cmd.Flags().Changed("output") {
		job.Output, _ = cmd.Flags().GetString("output")
	}
	if cmd.Flags().Changed("uri") {
		job.URI, _ = cmd.Flags().GetString("uri")
	}

	if cmd.Flags().Changed("pg-out-format") {
		value, _ := cmd.Flags().GetString("pg-out-format")
		ensureDatabaseOptions(job)
		job.DatabaseOptions["pg_out_format"] = value
	}
	if cmd.Flags().Changed("pg-compression-level") {
		value, _ := cmd.Flags().GetInt("pg-compression-level")
		ensureDatabaseOptions(job)
		job.DatabaseOptions["pg_compression_level"] = value
	}
	if cmd.Flags().Changed("pg-compression-algo") {
		value, _ := cmd.Flags().GetString("pg-compression-algo")
		ensureDatabaseOptions(job)
		job.DatabaseOptions["pg_compression_algo"] = value
	}
	if cmd.Flags().Changed("compress") {
		value, _ := cmd.Flags().GetBool("compress")
		if job.Type == "postgres" {
			ensureDatabaseOptions(job)
			if value {
				job.DatabaseOptions["compress"] = 1
			} else {
				job.DatabaseOptions["compress"] = 0
			}
		}
		if job.Type == "mongodb" {
			ensureDatabaseOptions(job)
			job.DatabaseOptions["gzip"] = value
		}
	}

	return nil
}

func applyStorageOverrides(cmd *cobra.Command, params *storage.Params, job *config.BackupJob) error {
	if cmd.Flags().Changed("storage") {
		value, _ := cmd.Flags().GetString("storage")
		params.StorageType = value
		job.Storage.Type = value
	}
	if cmd.Flags().Changed("local-path") {
		params.LocalPath, _ = cmd.Flags().GetString("local-path")
		job.Storage.LocalPath = params.LocalPath
	}
	if cmd.Flags().Changed("output") {
		params.OutName, _ = cmd.Flags().GetString("output")
	}
	if cmd.Flags().Changed("gdrive-folder-id") {
		params.GoogleDriveFolderId, _ = cmd.Flags().GetString("gdrive-folder-id")
		job.Storage.GDriveFolderID = params.GoogleDriveFolderId
	}
	if cmd.Flags().Changed("gdrive-sa-file") {
		params.GoogleServiceAccount, _ = cmd.Flags().GetString("gdrive-sa-file")
		job.Storage.GDriveSAFile = params.GoogleServiceAccount
	}
	if cmd.Flags().Changed("gcs-bucket") {
		params.GCSBucket, _ = cmd.Flags().GetString("gcs-bucket")
		job.Storage.GCSBucket = params.GCSBucket
	}
	if cmd.Flags().Changed("gcs-project-id") {
		params.GCSProjectID, _ = cmd.Flags().GetString("gcs-project-id")
		job.Storage.GCSProjectID = params.GCSProjectID
	}
	if cmd.Flags().Changed("gcs-credentials-file") {
		params.GCSCredentialsFile, _ = cmd.Flags().GetString("gcs-credentials-file")
		job.Storage.GCSCredentialsFile = params.GCSCredentialsFile
	}
	if cmd.Flags().Changed("aws-bucket") {
		params.AWSBucket, _ = cmd.Flags().GetString("aws-bucket")
		job.Storage.S3Bucket = params.AWSBucket
	}
	if cmd.Flags().Changed("aws-region") {
		params.AWSRegion, _ = cmd.Flags().GetString("aws-region")
		job.Storage.S3Region = params.AWSRegion
	}
	if cmd.Flags().Changed("aws-bucket-endpoint") {
		params.AWSBucketEndpoint, _ = cmd.Flags().GetString("aws-bucket-endpoint")
		job.Storage.S3BucketEndpoint = params.AWSBucketEndpoint
	}
	if cmd.Flags().Changed("aws-access-key-id") {
		params.AWSAccessKeyID, _ = cmd.Flags().GetString("aws-access-key-id")
		job.Storage.S3AccessKeyID = params.AWSAccessKeyID
	}
	if cmd.Flags().Changed("aws-secret") {
		params.AWSSecretAccessKey, _ = cmd.Flags().GetString("aws-secret")
		job.Storage.S3SecretAccessKey = params.AWSSecretAccessKey
	}
	return nil
}

func resolvePassword(cmd *cobra.Command, job config.BackupJob) (string, error) {
	flags := config.ResolveFlags{
		PasswordSet:     cmd.Flags().Changed("password"),
		PasswordEnvSet:  cmd.Flags().Changed("password-env"),
		PasswordFileSet: cmd.Flags().Changed("password-file"),
	}
	flags.Password, _ = cmd.Flags().GetString("password")
	flags.PasswordEnv, _ = cmd.Flags().GetString("password-env")
	flags.PasswordFile, _ = cmd.Flags().GetString("password-file")

	res, err := config.Resolve(flags, job)
	if err != nil {
		return "", err
	}

	if res.Source == config.SourceFileFlag && res.FileMode.Perm()&0o044 != 0 {
		passwordFilePermWarnOnce.Do(func() {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: password file '%s' has permissions 0o%03o (group- or world-readable); recommend chmod 0600\n", res.FilePath, res.FileMode.Perm())
		})
	}
	return res.Password, nil
}

func ensureDatabaseOptions(job *config.BackupJob) {
	if job.DatabaseOptions == nil {
		job.DatabaseOptions = make(map[string]interface{})
	}
}


func handleBackupForceFull(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	if strings.TrimSpace(cfgPath) == "" {
		return fmt.Errorf("--config is required")
	}

	cfg, err := LoadAndValidateConfig(cfgPath)
	if err != nil {
		return err
	}

	jobName, _ := cmd.Flags().GetString("job")
	job, ok := cfg.Databases[jobName]
	if !ok {
		return fmt.Errorf("backup job %q not found", jobName)
	}

	if err := executeBackupJobWithMode(cmd, cfg, job, executionModeConfig, backupRunOptions{forceFull: true}); err != nil {
		return err
	}

	cmd.Printf("Forced full backup completed for job %s\n", jobName)
	return nil
}

func handleBackupChainStatus(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	if strings.TrimSpace(cfgPath) == "" {
		return fmt.Errorf("--config is required")
	}

	cfg, err := LoadAndValidateConfig(cfgPath)
	if err != nil {
		return err
	}

	jobName, _ := cmd.Flags().GetString("job")
	if _, ok := cfg.Databases[jobName]; !ok {
		return fmt.Errorf("backup job %q not found", jobName)
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize monitor: %w", err)
	}
	defer mon.Close()

	executions, err := mon.ListExecutions(cmd.Context(), &ports.Filter{BackupName: jobName}, 300, 0)
	if err != nil {
		return fmt.Errorf("failed to list backup history: %w", err)
	}

	latest := latestSuccessfulExecution(executions)
	if latest == nil {
		cmd.Printf("No successful backup executions found for job %s\n", jobName)
		return nil
	}

	depth := 0
	if latest.ChainID != "" {
		for i := range executions {
			exec := executions[i]
			if !isSuccessfulStatus(exec.Status) {
				continue
			}
			if exec.ChainID == latest.ChainID {
				depth++
			}
		}
	}

	cmd.Printf("Job: %s\n", jobName)
	cmd.Printf("Latest Backup Type: %s\n", latest.BackupType)
	cmd.Printf("Chain ID: %s\n", latest.ChainID)
	cmd.Printf("Chain Index: %d\n", latest.ChainIndex)
	cmd.Printf("Chain Depth: %d\n", depth)
	cmd.Printf("Last Success: %s\n", latest.Timestamp.UTC().Format(time.RFC3339))
	return nil
}

func handleBackupChainList(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	if strings.TrimSpace(cfgPath) == "" {
		return fmt.Errorf("--config is required")
	}

	cfg, err := LoadAndValidateConfig(cfgPath)
	if err != nil {
		return err
	}

	chainID, _ := cmd.Flags().GetString("chain-id")
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		return fmt.Errorf("--chain-id is required")
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize monitor: %w", err)
	}
	defer mon.Close()

	executions, err := mon.ListExecutions(cmd.Context(), nil, 2000, 0)
	if err != nil {
		return fmt.Errorf("failed to list backup history: %w", err)
	}

	chainExecutions := make([]ports.Execution, 0)
	for i := range executions {
		exec := executions[i]
		if !isSuccessfulStatus(exec.Status) {
			continue
		}
		if exec.ChainID == chainID {
			chainExecutions = append(chainExecutions, exec)
		}
	}

	if len(chainExecutions) == 0 {
		cmd.Printf("No executions found for chain %s\n", chainID)
		return nil
	}

	sort.Slice(chainExecutions, func(i, j int) bool {
		return chainExecutions[i].ChainIndex < chainExecutions[j].ChainIndex
	})

	cmd.Printf("Chain: %s\n", chainID)
	cmd.Println("INDEX\tTYPE\tBACKUP\tTIME\tPATH")
	for i := range chainExecutions {
		exec := chainExecutions[i]
		cmd.Printf("%d\t%s\t%s\t%s\t%s\n", exec.ChainIndex, exec.BackupType, exec.BackupName, exec.Timestamp.UTC().Format(time.RFC3339), exec.FilePath)
	}

	return nil
}

func latestSuccessfulExecution(executions []ports.Execution) *ports.Execution {
	for i := range executions {
		exec := executions[i]
		if isSuccessfulStatus(exec.Status) {
			return &exec
		}
	}
	return nil
}

func isSuccessfulStatus(status string) bool {
	return status == "success" || status == ports.StatusCompleted
}
