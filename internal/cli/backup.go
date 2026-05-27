package cli

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/denisakp/sentinel/internal/backup"
	backupIncremental "github.com/denisakp/sentinel/internal/backup/incremental"
	backupMongo "github.com/denisakp/sentinel/internal/backup/mongo"
	backupSQL "github.com/denisakp/sentinel/internal/backup/sql"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/notifier"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/retention"
	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/utils"
	"github.com/denisakp/sentinel/pkg/backup/mariadb_dump"
	"github.com/denisakp/sentinel/pkg/backup/mongo_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysqlbinlog"
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
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

			pda := &pg_dump.PgDumpArgs{
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

			_, err = pg_dump.Backup(pda)
			if err != nil {
				cmd.PrintErrln(err)
				return err
			}
		}

		if dbType == "mysql" {
			mda := &mysql_dump.MySqlDumpArgs{
				Host:           host,
				Port:           port,
				Username:       user,
				Password:       password,
				Database:       database,
				AdditionalArgs: additionalArgs,
				Storage:        params,
			}

			_, err = mysql_dump.Backup(mda)
			if err != nil {
				cmd.PrintErrln(err)
				return err
			}
		}

		if dbType == "mariadb" {
			mda := &mariadb_dump.MariaDBDumpArgs{
				Host:           host,
				Port:           port,
				Username:       user,
				Password:       password,
				Database:       database,
				AdditionalArgs: additionalArgs,
				Storage:        params,
			}

			_, err = mariadb_dump.Backup(mda)
			if err != nil {
				cmd.PrintErrln(err)
				return err
			}
		}

		if dbType == "mongodb" {
			compress, _ = cmd.Flags().GetBool("compress") // get the compress flag value
			uri, _ := cmd.Flags().GetString("uri")        // get the uri flag value

			da := &mongo_dump.DumpMongoArgs{
				Compress:       compress,
				AdditionalArgs: additionalArgs,
				Uri:            uri,
				Storage:        params,
			}

			_, err = mongo_dump.Backup(da)
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

func executeSingleBackupJob(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, mode backupExecutionMode, opts backupRunOptions) error {
	start := time.Now()
	storageParams := config.BuildStorageParams(job)
	if err := applyStorageOverrides(cmd, storageParams, &job); err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}
	applyScheduledOutputName(job, storageParams, mode, start)

	additionalArgs := config.BuildAdditionalArgs(job)
	if cmd.Flags().Changed("args") {
		additionalArgs, _ = cmd.Flags().GetString("args")
	}

	password, err := resolvePassword(cmd, job)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	var backupErr error
	var digest string
	switch job.Type {
	case "postgres":
		pgArgs, err := config.BuildPgDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
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
		digest, backupErr = pg_dump.Backup(pgArgs)
	case "mysql":
		mysqlArgs, err := config.BuildMySQLDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		digest, backupErr = mysql_dump.Backup(mysqlArgs)
	case "mariadb":
		mariaArgs, err := config.BuildMariaDBDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		digest, backupErr = mariadb_dump.Backup(mariaArgs)
	case "mongodb":
		mongoArgs, err := config.BuildMongoDumpArgs(job, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		if cmd.Flags().Changed("compress") {
			mongoArgs.Compress, _ = cmd.Flags().GetBool("compress")
		}
		if cmd.Flags().Changed("uri") {
			mongoArgs.Uri, _ = cmd.Flags().GetString("uri")
		}
		digest, backupErr = mongo_dump.Backup(mongoArgs)
	default:
		return fmt.Errorf("backup '%s': unsupported database type '%s'", job.Name, job.Type)
	}

	end := time.Now()
	var security *backupSecurityResult
	if backupErr == nil {
		security, err = applyBackupSecurity(cfg, job, storageParams, opts.forceFull, digest)
		if err != nil {
			backupErr = fmt.Errorf("backup '%s': security processing failed: %w", job.Name, err)
		}
	}
	if notifyErr := notifyBackupResult(cmd, cfg, job, storageParams, start, end, backupErr, security, opts.forceFull); notifyErr != nil {
		cmd.PrintErrln("notification error:", notifyErr)
	}
	if mode == executionModeScheduled && backupErr == nil {
		runScheduledRetention(cmd, cfg, job)
	}

	return backupErr
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

	excluded := map[string]struct{}{}
	for _, name := range job.Exclude {
		excluded[name] = struct{}{}
	}

	for _, dbName := range databaseNames {
		if _, skip := excluded[dbName]; skip {
			continue
		}
		childJob := job
		childJob.Database = dbName
		childJob.Strategy = ""
		if err := executeSingleBackupJob(cmd, cfg, childJob, mode, opts); err != nil {
			return err
		}
	}

	return nil
}

func executeAutoDiscoverySingle(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, mode backupExecutionMode, opts backupRunOptions) error {
	start := time.Now()
	storageParams := config.BuildStorageParams(job)
	if err := applyStorageOverrides(cmd, storageParams, &job); err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}
	applyScheduledOutputName(job, storageParams, mode, start)

	additionalArgs := config.BuildAdditionalArgs(job)
	if cmd.Flags().Changed("args") {
		additionalArgs, _ = cmd.Flags().GetString("args")
	}

	password, err := resolvePassword(cmd, job)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	var backupErr error
	var digest string
	switch job.Type {
	case "postgres":
		pgArgs, err := config.BuildPgDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		digest, backupErr = pg_dump.BackupAll(&pg_dump.PgDumpAllArgs{
			Host:           pgArgs.Host,
			Port:           pgArgs.Port,
			Username:       pgArgs.Username,
			Password:       pgArgs.Password,
			AdditionalArgs: pgArgs.AdditionalArgs,
			Storage:        pgArgs.Storage,
		})
	case "mysql":
		mysqlArgs, err := config.BuildMySQLDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		digest, backupErr = mysql_dump.BackupAll(&mysql_dump.MySqlDumpAllArgs{
			Host:           mysqlArgs.Host,
			Port:           mysqlArgs.Port,
			Username:       mysqlArgs.Username,
			Password:       mysqlArgs.Password,
			AdditionalArgs: mysqlArgs.AdditionalArgs,
			Storage:        mysqlArgs.Storage,
		})
	case "mariadb":
		mariaArgs, err := config.BuildMariaDBDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		digest, backupErr = mariadb_dump.BackupAll(&mariadb_dump.MariaDBDumpAllArgs{
			Host:           mariaArgs.Host,
			Port:           mariaArgs.Port,
			Username:       mariaArgs.Username,
			Password:       mariaArgs.Password,
			AdditionalArgs: mariaArgs.AdditionalArgs,
			Storage:        mariaArgs.Storage,
		})
	case "mongodb":
		mongoArgs, err := config.BuildMongoDumpArgs(job, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		mongoArgs.Database = ""
		if cmd.Flags().Changed("compress") {
			mongoArgs.Compress, _ = cmd.Flags().GetBool("compress")
		}
		if cmd.Flags().Changed("uri") {
			mongoArgs.Uri, _ = cmd.Flags().GetString("uri")
		}
		digest, backupErr = mongo_dump.Backup(mongoArgs)
	default:
		return fmt.Errorf("backup '%s': unsupported database type '%s'", job.Name, job.Type)
	}

	end := time.Now()
	var security *backupSecurityResult
	if backupErr == nil {
		security, err = applyBackupSecurity(cfg, job, storageParams, opts.forceFull, digest)
		if err != nil {
			backupErr = fmt.Errorf("backup '%s': security processing failed: %w", job.Name, err)
		}
	}
	if notifyErr := notifyBackupResult(cmd, cfg, job, storageParams, start, end, backupErr, security, opts.forceFull); notifyErr != nil {
		cmd.PrintErrln("notification error:", notifyErr)
	}
	if mode == executionModeScheduled && backupErr == nil {
		runScheduledRetention(cmd, cfg, job)
	}

	return backupErr
}

func applyScheduledOutputName(job config.BackupJob, storageParams *storage.Params, mode backupExecutionMode, timestamp time.Time) {
	if mode != executionModeScheduled || storageParams == nil {
		return
	}

	canonicalExt := canonicalScheduledExtension(job)
	storageParams.OutName = utils.BuildScheduledOutName(storageParams.OutName, canonicalExt, job.Name, timestamp)
}

func canonicalScheduledExtension(job config.BackupJob) string {
	switch job.Type {
	case "postgres":
		format := "p"
		if value, ok := job.DatabaseOptions["pg_out_format"].(string); ok && value != "" {
			format = value
		}
		switch format {
		case "c":
			return ".backup"
		case "t":
			return ".tar"
		case "d":
			return ""
		default:
			return ".sql"
		}
	case "mysql", "mariadb":
		return ".sql"
	default:
		return ""
	}
}

func runScheduledRetention(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob) {
	if cfg == nil {
		return
	}
	if job.Retention.KeepLast == 0 && job.Retention.KeepDays == 0 {
		return
	}

	cmd.Printf("Retention: evaluating backup '%s' (keep_last=%d, keep_days=%d)\n", job.Name, job.Retention.KeepLast, job.Retention.KeepDays)

	manager, err := retention.NewManager(cfg)
	if err != nil {
		cmd.PrintErrf("warning: retention manager init failed for backup '%s': %v\n", job.Name, err)
		return
	}
	defer func() {
		if closeErr := manager.Close(); closeErr != nil {
			cmd.PrintErrf("warning: retention manager close failed for backup '%s': %v\n", job.Name, closeErr)
		}
	}()

	deleted, err := manager.Apply(context.Background(), job.Name, false)
	if err != nil {
		cmd.PrintErrf("warning: retention apply failed for backup '%s': %v\n", job.Name, err)
		return
	}

	if len(deleted) > 0 {
		cmd.Printf("Retention: deleted %d artifact(s) for backup '%s'\n", len(deleted), job.Name)
	}
}

func notifyBackupResult(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, storageParams *storage.Params, start, end time.Time, backupErr error, security *backupSecurityResult, forceFull bool) error {
	if err := recordBackupExecution(cfg, job, storageParams, start, end, backupErr, security, forceFull); err != nil {
		cmd.PrintErrln("monitor error:", err)
	}

	if len(job.Notifications) == 0 {
		return nil
	}

	status := notifier.StatusSuccess
	errorMessage := ""
	if backupErr != nil {
		status = notifier.StatusFailure
		errorMessage = backupErr.Error()
	}

	filePath, fileSize := localBackupInfo(storageParams)
	ctx := &notifier.BackupContext{
		BackupName:   job.Name,
		DatabaseType: job.Type,
		DatabaseName: job.Database,
		Status:       status,
		StartTime:    start,
		EndTime:      end,
		Error:        errorMessage,
		FilePath:     filePath,
		FileSize:     fileSize,
	}

	dispatcher, err := notifier.NewDispatcherFromConfig(job.Notifications)
	if err != nil {
		return err
	}

	return dispatcher.Notify(ctx)
}

func recordBackupExecution(cfg *config.Configuration, job config.BackupJob, storageParams *storage.Params, start, end time.Time, backupErr error, security *backupSecurityResult, forceFull bool) error {
	status := "success"
	errorMessage := ""
	if backupErr != nil {
		status = "failure"
		errorMessage = backupErr.Error()
	}

	filePath, fileSize := resolveBackupPath(storageParams)
	storageBackend := ""
	if storageParams != nil {
		storageBackend = storageParams.StorageType
	}

	exec := &ports.Execution{
		BackupName:     job.Name,
		DatabaseType:   job.Type,
		Timestamp:      start.UTC(),
		DurationMs:     end.Sub(start).Milliseconds(),
		Status:         status,
		ErrorMessage:   errorMessage,
		StorageBackend: storageBackend,
		FilePath:       filePath,
		FileSizeBytes:  fileSize,
	}

	incrementalMeta := deriveIncrementalBackupContext(cfg, job, fileSize, forceFull)
	exec.BackupType = incrementalMeta.BackupType
	exec.ChainID = incrementalMeta.ChainID
	exec.ChainIndex = incrementalMeta.ChainIndex
	exec.DeltaSizeBytes = incrementalMeta.DeltaSizeBytes
	exec.FullBackupSizeBytes = incrementalMeta.FullBackupSizeBytes

	if security != nil {
		if security.backupType != "" {
			exec.BackupType = security.backupType
		}
		if security.chainID != "" {
			exec.ChainID = security.chainID
		}
		exec.ChainIndex = security.chainIndex
		exec.DeltaSizeBytes = security.deltaSize
		exec.FullBackupSizeBytes = security.fullSize
	}

	monitor.ObserveIncrementalBackup(job.Name, exec)

	if cfg == nil || cfg.HistoryDBPath == "" {
		return nil
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return err
	}
	defer mon.Close()

	ctx := context.Background()
	if err := mon.RecordExecution(ctx, exec); err != nil {
		return err
	}

	if security != nil && exec.ID != "" {
		if secErr := mon.RecordSecurityInfo(ctx, exec.ID, security.hashAlgo, security.hashValue, "", security.manifestPath, security.encrypted, security.keyHint); secErr != nil {
			fmt.Printf("Warning: failed to record security info for '%s': %v\n", job.Name, secErr)
		}
	}

	return nil
}

func resolveBackupPath(storageParams *storage.Params) (string, int64) {
	if storageParams == nil {
		return "unknown", 0
	}
	if storageParams.StorageType == "" || storageParams.StorageType == "local" {
		path, size := localBackupInfo(storageParams)
		if path == "" {
			return "unknown", size
		}
		return path, size
	}

	if storageParams.OutName != "" {
		if storageParams.StorageType == "gcs" {
			return fmt.Sprintf("gs://%s/%s", storageParams.GCSBucket, storageParams.OutName), 0
		}
		return storageParams.OutName, 0
	}

	return "unknown", 0
}

func localBackupInfo(storageParams *storage.Params) (string, int64) {
	if storageParams == nil {
		return "", 0
	}
	if storageParams.StorageType != "" && storageParams.StorageType != "local" {
		return "", 0
	}

	path := storageParams.OutName
	if path == "" {
		return "", 0
	}

	if storageParams.LocalPath != "" && !filepath.IsAbs(path) {
		path = filepath.Join(storageParams.LocalPath, path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return path, 0
	}
	if info.IsDir() {
		return path, 0
	}
	return path, info.Size()
}

func listDatabases(job config.BackupJob) ([]string, error) {
	switch job.Type {
	case "postgres":
		password, err := config.PasswordFromEnv(job.PasswordEnv)
		if err != nil {
			return nil, err
		}
		return backupSQL.ListDatabases("postgres", job.Host, portString(job.Port), job.Username, password)
	case "mysql", "mariadb":
		password, err := config.PasswordFromEnv(job.PasswordEnv)
		if err != nil {
			return nil, err
		}
		return backupSQL.ListDatabases("mysql", job.Host, portString(job.Port), job.Username, password)
	case "mongodb":
		return backupMongo.ListDatabases(job.URI)
	default:
		return nil, fmt.Errorf("unsupported database type '%s'", job.Type)
	}
}

func portString(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
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

// backupSecurityResult holds hash and encryption metadata produced by applyBackupSecurity.
type backupSecurityResult struct {
	hashAlgo     string
	hashValue    string
	manifestPath string
	encrypted    bool
	keyHint      string
	backupType   string
	chainID      string
	chainIndex   int
	deltaSize    int64
	fullSize     int64
}

var verifyIncrementalArtifactHash = manifest.VerifyBackupHash

// applyBackupSecurity records the supplied plaintext digest in a manifest and
// optionally encrypts the local backup in-place using AES-256-GCM. The
// plaintextDigest is the hex SHA-256 the dump adapter computed inline over the
// bytes written to storage; an empty digest is treated the same as a non-local
// artefact (no manifest emitted).
func applyBackupSecurity(cfg *config.Configuration, job config.BackupJob, storageParams *storage.Params, forceFull bool, plaintextDigest string) (*backupSecurityResult, error) {
	filePath, fileSize := localBackupInfo(storageParams)
	if filePath == "" {
		return nil, nil
	}
	if _, err := os.Stat(filePath); err != nil {
		return nil, nil
	}

	hashValue := plaintextDigest
	plaintextHash := hashValue

	result := &backupSecurityResult{
		hashAlgo:  "sha256",
		hashValue: hashValue,
	}

	incrementalMeta := deriveIncrementalBackupContext(cfg, job, fileSize, forceFull)
	if incrementalMeta.BackupType == "incremental" && (job.Type == "mysql" || job.Type == "mariadb") {
		binlogMeta, archiveErr := archiveMySQLBinlogArtifacts(context.Background(), job, filePath)
		if archiveErr != nil {
			return nil, archiveErr
		}
		incrementalMeta.BinlogStartFile = binlogMeta.BinlogStartFile
		incrementalMeta.BinlogEndFile = binlogMeta.BinlogEndFile
		incrementalMeta.BinlogArtifacts = append([]string{}, binlogMeta.BinlogArtifacts...)
	}
	if incrementalMeta.BackupType == "incremental" && job.Type == "mongodb" {
		oplogMeta, archiveErr := archiveMongoOplogArtifacts(context.Background(), job, filePath)
		if archiveErr != nil {
			return nil, archiveErr
		}
		incrementalMeta.OplogArtifactPath = oplogMeta.OplogArtifactPath
	}
	result.backupType = incrementalMeta.BackupType
	result.chainID = incrementalMeta.ChainID
	result.chainIndex = incrementalMeta.ChainIndex
	result.deltaSize = incrementalMeta.DeltaSizeBytes
	result.fullSize = incrementalMeta.FullBackupSizeBytes

	// Encryption is opt-in: only attempt encryption when an explicit key source is configured.
	var encInfo *ports.EncryptionInfo
	if cfg != nil && (cfg.EncryptionKeyEnv != "" || cfg.EncryptionKeyFile != "") {
		encrypted, encMeta, encHash, encErr := encryptBackupFile(cfg, filePath, job.Name)
		if encErr != nil {
			return nil, fmt.Errorf("failed to encrypt backup: %w", encErr)
		} else if encrypted {
			result.encrypted = true
			result.hashValue = encHash
			result.keyHint = cfg.EncryptionKeyEnv
			encInfo = encMeta
		}
	}

	if incrementalMeta.BackupType == "incremental" {
		if verifyErr := verifyIncrementalArtifactHash(filePath, result.hashAlgo, result.hashValue); verifyErr != nil {
			return nil, fmt.Errorf("failed to verify incremental artifact hash: %w", verifyErr)
		}
	}

	// Write manifest alongside the backup file
	manifestPath := filePath + ".manifest.json"
	m := &ports.BackupManifest{
		BackupID:     job.Name,
		Database:     job.Database,
		DatabaseType: job.Type,
		CreatedAt:    time.Now().UTC(),
		SizeBytes:    fileSize,
		Hash: ports.HashInfo{
			Algorithm:      "sha256",
			Value:          result.hashValue,
			PlaintextValue: plaintextHash,
		},
		Encryption: encInfo,
		AdvancedRestore: &ports.AdvancedRestoreMetadata{
			Capabilities: []string{"full", "incremental"},
			IncrementalLineage: &ports.IncrementalLineageMetadata{
				Enabled:             incrementalMeta.Enabled,
				ChainID:             incrementalMeta.ChainID,
				ChainIndex:          incrementalMeta.ChainIndex,
				MaxChainDepth:       incrementalMeta.MaxChainDepth,
				BaselineBackupID:    incrementalMeta.BaselineBackupID,
				RequiredBackupIDs:   incrementalMeta.RequiredBackupIDs,
				DeltaSizeBytes:      incrementalMeta.DeltaSizeBytes,
				FullBackupSizeBytes: incrementalMeta.FullBackupSizeBytes,
				CompressionRatio:    incrementalMeta.CompressionRatio,
				Engine:              job.Type,
				BinlogStartFile:     incrementalMeta.BinlogStartFile,
				BinlogEndFile:       incrementalMeta.BinlogEndFile,
				BinlogArtifacts:     incrementalMeta.BinlogArtifacts,
				OplogArtifactPath:   incrementalMeta.OplogArtifactPath,
				ExecutionSupported:  true,
			},
		},
	}
	if writeErr := manifest.WriteManifest(manifestPath, m); writeErr != nil {
		fmt.Printf("Warning: failed to write manifest for '%s': %v\n", job.Name, writeErr)
	} else {
		result.manifestPath = manifestPath
	}

	return result, nil
}

type backupIncrementalContext struct {
	Enabled             bool
	BackupType          string
	ChainID             string
	ChainIndex          int
	MaxChainDepth       int
	BaselineBackupID    string
	RequiredBackupIDs   []string
	DeltaSizeBytes      int64
	FullBackupSizeBytes int64
	CompressionRatio    float64
	BinlogStartFile     string
	BinlogEndFile       string
	BinlogArtifacts     []string
	OplogArtifactPath   string
}

func archiveMySQLBinlogArtifacts(ctx context.Context, job config.BackupJob, backupFilePath string) (backupIncrementalContext, error) {
	binlogPath := strings.TrimSpace(job.MySQL.BinlogPath)
	if binlogPath == "" {
		return backupIncrementalContext{}, fmt.Errorf("mysql.binlog_path is required for incremental %s backup", job.Type)
	}

	archiveName := fmt.Sprintf("%s.binlogs.tar", filepath.Base(backupFilePath))
	archiveResult, err := mysqlbinlog.Archive(ctx, &mysqlbinlog.ArchiveArgs{
		BinlogDir:   binlogPath,
		OutputDir:   filepath.Dir(backupFilePath),
		ArchiveName: archiveName,
	})
	if err != nil {
		return backupIncrementalContext{}, fmt.Errorf("failed to archive %s binlogs: %w", job.Type, err)
	}

	return backupIncrementalContext{
		BinlogStartFile: archiveResult.StartFile,
		BinlogEndFile:   archiveResult.EndFile,
		BinlogArtifacts: []string{archiveResult.ArchivePath},
	}, nil
}

func archiveMongoOplogArtifacts(ctx context.Context, job config.BackupJob, backupFilePath string) (backupIncrementalContext, error) {
	mongoURI := strings.TrimSpace(job.URI)
	if mongoURI == "" {
		return backupIncrementalContext{}, fmt.Errorf("mongodb uri is required for incremental oplog archival")
	}

	archiveName := fmt.Sprintf("%s.oplog.archive", filepath.Base(backupFilePath))
	archiveResult, err := mongo_dump.ArchiveOplog(ctx, &mongo_dump.OplogArchiveArgs{
		URI:         mongoURI,
		OutputDir:   filepath.Dir(backupFilePath),
		ArchiveName: archiveName,
	})
	if err != nil {
		return backupIncrementalContext{}, fmt.Errorf("failed to archive mongodb oplog: %w", err)
	}

	return backupIncrementalContext{
		OplogArtifactPath: archiveResult.ArchivePath,
	}, nil
}

func deriveIncrementalBackupContext(cfg *config.Configuration, job config.BackupJob, fileSize int64, forceFull bool) backupIncrementalContext {
	normalized := config.NormalizeIncrementalBackupConfig(job)
	if normalized.IncrementalBackup == nil || !normalized.IncrementalBackup.Enabled {
		return backupIncrementalContext{BackupType: "full"}
	}

	previous := backupIncremental.ChainState{MaxDepth: normalized.IncrementalBackup.MaxChainDepth}
	latest := latestIncrementalExecution(cfg, normalized.Name)
	if latest != nil {
		previous.ChainID = latest.ChainID
		previous.CurrentIndex = latest.ChainIndex
		previous.BaselineBackupID = resolveBaselineBackupID(cfg, normalized.Name, latest)
	}

	decision, err := backupIncremental.Decide(previous, forceFull)
	if err != nil {
		return backupIncrementalContext{Enabled: true, BackupType: "full", MaxChainDepth: normalized.IncrementalBackup.MaxChainDepth}
	}

	ctx := backupIncrementalContext{
		Enabled:       true,
		BackupType:    decision.Type,
		ChainID:       decision.ChainID,
		ChainIndex:    decision.ChainIndex,
		MaxChainDepth: normalized.IncrementalBackup.MaxChainDepth,
	}

	if decision.Type == "incremental" {
		ctx.BaselineBackupID = decision.BaselineBackupID
		ctx.RequiredBackupIDs = []string{decision.BaselineBackupID}
		ctx.DeltaSizeBytes = fileSize
	} else {
		ctx.FullBackupSizeBytes = fileSize
	}

	if ctx.FullBackupSizeBytes > 0 && ctx.DeltaSizeBytes > 0 {
		ctx.CompressionRatio = float64(ctx.DeltaSizeBytes) / float64(ctx.FullBackupSizeBytes)
	}

	return ctx
}

func latestIncrementalExecution(cfg *config.Configuration, jobName string) *ports.Execution {
	if cfg == nil || strings.TrimSpace(cfg.HistoryDBPath) == "" || strings.TrimSpace(jobName) == "" {
		return nil
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return nil
	}
	defer mon.Close()

	executions, err := mon.ListExecutions(context.Background(), &ports.Filter{BackupName: jobName}, 100, 0)
	if err != nil {
		return nil
	}

	for i := range executions {
		exec := executions[i]
		if exec.Status != "success" && exec.Status != ports.StatusCompleted {
			continue
		}
		if strings.TrimSpace(exec.ChainID) == "" {
			continue
		}
		if exec.BackupType != "full" && exec.BackupType != "incremental" {
			continue
		}
		return &exec
	}

	return nil
}

func resolveBaselineBackupID(cfg *config.Configuration, jobName string, latest *ports.Execution) string {
	if latest == nil {
		return ""
	}

	if latest.BackupType == "full" {
		if latest.FilePath != "" {
			return latest.FilePath
		}
		return latest.ID
	}

	if cfg == nil || strings.TrimSpace(cfg.HistoryDBPath) == "" {
		return latest.ID
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return latest.ID
	}
	defer mon.Close()

	executions, err := mon.ListExecutions(context.Background(), &ports.Filter{BackupName: jobName}, 300, 0)
	if err != nil {
		return latest.ID
	}

	for i := range executions {
		exec := executions[i]
		if exec.Status != "success" && exec.Status != ports.StatusCompleted {
			continue
		}
		if exec.ChainID != latest.ChainID {
			continue
		}
		if exec.BackupType == "full" || exec.ChainIndex == 0 {
			if exec.FilePath != "" {
				return exec.FilePath
			}
			return exec.ID
		}
	}

	return latest.ID
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

// encryptBackupFile encrypts filePath in-place using AES-256-GCM via ChunkEncryptWriter.
// Returns (encrypted, encInfo, hashOfEncryptedFile, err).
func encryptBackupFile(cfg *config.Configuration, filePath, backupID string) (bool, *ports.EncryptionInfo, string, error) {
	kp := &crypto.FileKeyProvider{
		EnvVar:   cfg.EncryptionKeyEnv,
		FilePath: cfg.EncryptionKeyFile,
	}
	masterKey, err := kp.GetKey()
	if err != nil {
		return false, nil, "", fmt.Errorf("failed to get encryption key: %w", err)
	}

	salt, err := crypto.GenerateSalt()
	if err != nil {
		return false, nil, "", err
	}
	derivedKey := crypto.DeriveKey(masterKey, salt)

	in, err := os.Open(filePath)
	if err != nil {
		return false, nil, "", fmt.Errorf("failed to open file for encryption: %w", err)
	}

	encPath := filePath + ".enc"
	out, err := os.Create(encPath)
	if err != nil {
		in.Close()
		return false, nil, "", fmt.Errorf("failed to create encrypted output: %w", err)
	}

	hw := crypto.NewHashingWriter(out)
	enc, err := crypto.NewChunkEncryptWriter(hw, derivedKey, backupID)
	if err != nil {
		in.Close()
		out.Close()
		os.Remove(encPath)
		return false, nil, "", err
	}

	_, copyErr := io.Copy(enc, in)
	in.Close()
	if copyErr != nil {
		out.Close()
		os.Remove(encPath)
		return false, nil, "", fmt.Errorf("failed during encryption: %w", copyErr)
	}

	if flushErr := enc.Flush(); flushErr != nil {
		out.Close()
		os.Remove(encPath)
		return false, nil, "", fmt.Errorf("failed to flush encrypted data: %w", flushErr)
	}

	encHash := hw.Sum()
	nonce := enc.BaseNonce()
	authTag := enc.LastAuthTag()
	out.Close()

	if renameErr := os.Rename(encPath, filePath); renameErr != nil {
		os.Remove(encPath)
		return false, nil, "", fmt.Errorf("failed to replace file with encrypted version: %w", renameErr)
	}

	encInfo := &ports.EncryptionInfo{
		Algorithm:       "AES-256-GCM",
		KeyDerivation:   "PBKDF2-HMAC-SHA256",
		Iterations:      100_000,
		Salt:            base64.StdEncoding.EncodeToString(salt),
		IV:              hex.EncodeToString(nonce),
		AuthTag:         hex.EncodeToString(authTag),
		EnvelopeVersion: 2,
	}

	return true, encInfo, encHash, nil
}
