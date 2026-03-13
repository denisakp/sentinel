package cli

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/denisakp/sentinel/internal/backup"
	backupMongo "github.com/denisakp/sentinel/internal/backup/mongo"
	backupSQL "github.com/denisakp/sentinel/internal/backup/sql"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/notifier"
	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/pkg/backup/mariadb_dump"
	"github.com/denisakp/sentinel/pkg/backup/mongo_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
	"github.com/spf13/cobra"
)

var dbType, host, port, user, password, database,
	pgOutFormat, pgCompressionAlgo, uri,
	output, storageType, localPath, gDriveSaFile, gDriveFolderId,
	awsSecretAccessKey, awsAccessKeyID, awsRegion, awsBucket, awsBucketEndpoint,
	additionalArgs, configPath string
var compress bool
var pgCompressionLevel int
var err error

var BackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Run a database backup",
	Long:  "Run a database backup using CLI flags or a YAML config.\n\nExamples:\n  sentinel backup --config sentinel.yaml\n  sentinel backup --type postgres --host db --port 5432 --user backup --database app",
	Run: func(cmd *cobra.Command, args []string) {
		configPath, _ = cmd.Flags().GetString("config")
		if configPath != "" {
			if err := runBackupFromConfig(cmd, configPath); err != nil {
				cmd.PrintErrln(err)
				os.Exit(1)
			}
			return
		}

		dbType, _ = cmd.Flags().GetString("type")
		if dbType == "" {
			cmd.PrintErrln("database type is required when --config is not provided")
			return
		}

		// validate the database type
		if err = backup.ValidateDbType(dbType); err != nil {
			cmd.PrintErrln(err)
			return
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
			AWSBucket:            awsBucket,
			AWSRegion:            awsRegion,
			AWSBucketEndpoint:    awsBucketEndpoint,
			AWSAccessKeyID:       awsAccessKeyID,
			AWSSecretAccessKey:   awsSecretAccessKey,
		}

		if err = storage.ValidateStorage(params); err != nil {
			cmd.PrintErrln(err)
			return
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

			err = pg_dump.Backup(pda)
			if err != nil {
				cmd.PrintErrln(err)
				os.Exit(1)
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

			err = mysql_dump.Backup(mda)
			if err != nil {
				cmd.PrintErrln(err)
				os.Exit(1)
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

			err = mariadb_dump.Backup(mda)
			if err != nil {
				cmd.PrintErrln(err)
				os.Exit(1)
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

			err = mongo_dump.Backup(da)
			if err != nil {
				cmd.PrintErrln(err)
				os.Exit(1)
			}
		}
	},
}

func init() {
	BackupCmd.Flags().StringVarP(&dbType, "type", "t", "", "Database type (mysql, postgres, mariadb, mongodb)")
	BackupCmd.Flags().StringVar(&configPath, "config", "", "Path to YAML configuration file (preferred)")

	BackupCmd.Flags().StringVarP(&host, "host", "H", "127.0.0.1", "Database host")
	BackupCmd.Flags().StringVarP(&port, "port", "P", "", "Database port")
	BackupCmd.Flags().StringVarP(&user, "user", "u", "root", "Database user")
	BackupCmd.Flags().StringVarP(&password, "password", "p", "", "Database password")
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
	BackupCmd.Flags().StringVarP(&storageType, "storage", "s", "local", "storage type (local, s3, google-drive)")
	BackupCmd.Flags().StringVarP(&localPath, "local-path", "", "", "Local path to store the backup")
	BackupCmd.Flags().StringVarP(&output, "output", "o", "", "Output name")
	//google drive
	BackupCmd.Flags().StringVarP(&gDriveFolderId, "gdrive-folder-id", "", "", "Google Drive folder ID")
	BackupCmd.Flags().StringVarP(&gDriveSaFile, "gdrive-sa-file", "", "", "Google Drive service account file")
	//aws s3 storage
	BackupCmd.Flags().StringVarP(&awsBucket, "aws-bucket", "", "", "AWS S3 bucket name")
	BackupCmd.Flags().StringVarP(&awsRegion, "aws-region", "", "us-east-1", "AWS region")
	BackupCmd.Flags().StringVarP(&awsBucketEndpoint, "aws-bucket-endpoint", "", "", "AWS S3 bucket endpoint")
	BackupCmd.Flags().StringVarP(&awsAccessKeyID, "aws-access-key-id", "", "", "AWS access key ID")
	BackupCmd.Flags().StringVarP(&awsSecretAccessKey, "aws-secret", "", "", "AWS secret")

	// required args are enforced at runtime when --config is not provided
}

func runBackupFromConfig(cmd *cobra.Command, path string) error {
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return err
	}
	if err := config.ValidateConfig(cfg); err != nil {
		return err
	}

	for _, job := range cfg.Databases {
		if job.Enabled != nil && !*job.Enabled {
			continue
		}
		if err := executeBackupJob(cmd, cfg, job); err != nil {
			return err
		}
	}

	return nil
}

func executeBackupJob(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob) error {
	if err := applyCLIOverrides(cmd, &job); err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	if job.Database == "*" {
		return executeAutoDiscovery(cmd, cfg, job)
	}

	return executeSingleBackupJob(cmd, cfg, job)
}

func executeSingleBackupJob(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob) error {
	start := time.Now()
	storageParams := config.BuildStorageParams(job)
	if err := applyStorageOverrides(cmd, storageParams, &job); err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	additionalArgs := config.BuildAdditionalArgs(job)
	if cmd.Flags().Changed("args") {
		additionalArgs, _ = cmd.Flags().GetString("args")
	}

	password, err := resolvePassword(cmd, job)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	var backupErr error
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
		backupErr = pg_dump.Backup(pgArgs)
	case "mysql":
		mysqlArgs, err := config.BuildMySQLDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		backupErr = mysql_dump.Backup(mysqlArgs)
	case "mariadb":
		mariaArgs, err := config.BuildMariaDBDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		backupErr = mariadb_dump.Backup(mariaArgs)
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
		backupErr = mongo_dump.Backup(mongoArgs)
	default:
		return fmt.Errorf("backup '%s': unsupported database type '%s'", job.Name, job.Type)
	}

	end := time.Now()
	var security *backupSecurityResult
	if backupErr == nil {
		security, err = applyBackupSecurity(cfg, job, storageParams)
		if err != nil {
			backupErr = fmt.Errorf("backup '%s': security processing failed: %w", job.Name, err)
		}
	}
	if notifyErr := notifyBackupResult(cmd, cfg, job, storageParams, start, end, backupErr, security); notifyErr != nil {
		cmd.PrintErrln("notification error:", notifyErr)
	}

	return backupErr
}

func executeAutoDiscovery(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob) error {
	strategy := job.Strategy
	if strategy == "" {
		strategy = "individual"
	}

	if strategy == "single" {
		return executeAutoDiscoverySingle(cmd, cfg, job)
	}

	return executeAutoDiscoveryIndividual(cmd, cfg, job)
}

func executeAutoDiscoveryIndividual(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob) error {
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
		if err := executeSingleBackupJob(cmd, cfg, childJob); err != nil {
			return err
		}
	}

	return nil
}

func executeAutoDiscoverySingle(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob) error {
	start := time.Now()
	storageParams := config.BuildStorageParams(job)
	if err := applyStorageOverrides(cmd, storageParams, &job); err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	additionalArgs := config.BuildAdditionalArgs(job)
	if cmd.Flags().Changed("args") {
		additionalArgs, _ = cmd.Flags().GetString("args")
	}

	password, err := resolvePassword(cmd, job)
	if err != nil {
		return fmt.Errorf("backup '%s': %w", job.Name, err)
	}

	var backupErr error
	switch job.Type {
	case "postgres":
		pgArgs, err := config.BuildPgDumpArgs(job, password, additionalArgs, storageParams)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		backupErr = pg_dump.BackupAll(&pg_dump.PgDumpAllArgs{
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
		backupErr = mysql_dump.BackupAll(&mysql_dump.MySqlDumpAllArgs{
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
		backupErr = mariadb_dump.BackupAll(&mariadb_dump.MariaDBDumpAllArgs{
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
		backupErr = mongo_dump.Backup(mongoArgs)
	default:
		return fmt.Errorf("backup '%s': unsupported database type '%s'", job.Name, job.Type)
	}

	end := time.Now()
	var security *backupSecurityResult
	if backupErr == nil {
		security, err = applyBackupSecurity(cfg, job, storageParams)
		if err != nil {
			backupErr = fmt.Errorf("backup '%s': security processing failed: %w", job.Name, err)
		}
	}
	if notifyErr := notifyBackupResult(cmd, cfg, job, storageParams, start, end, backupErr, security); notifyErr != nil {
		cmd.PrintErrln("notification error:", notifyErr)
	}

	return backupErr
}

func notifyBackupResult(cmd *cobra.Command, cfg *config.Configuration, job config.BackupJob, storageParams *storage.Params, start, end time.Time, backupErr error, security *backupSecurityResult) error {
	if err := recordBackupExecution(cfg, job, storageParams, start, end, backupErr, security); err != nil {
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

func recordBackupExecution(cfg *config.Configuration, job config.BackupJob, storageParams *storage.Params, start, end time.Time, backupErr error, security *backupSecurityResult) error {
	if cfg == nil || cfg.HistoryDBPath == "" {
		return nil
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return err
	}
	defer mon.Close()

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

	exec := &monitor.Execution{
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
	if cmd.Flags().Changed("password") {
		return cmd.Flags().GetString("password")
	}
	if job.Type == "mongodb" {
		return "", nil
	}
	return config.PasswordFromEnv(job.PasswordEnv)
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
}

// applyBackupSecurity computes a SHA-256 hash of the local backup file and optionally
// encrypts it in-place using AES-256-GCM (T023, T033). A BackupManifest is written
// alongside the file. Returns nil silently for non-local or missing files.
func applyBackupSecurity(cfg *config.Configuration, job config.BackupJob, storageParams *storage.Params) (*backupSecurityResult, error) {
	filePath, fileSize := localBackupInfo(storageParams)
	if filePath == "" {
		return nil, nil
	}
	if _, err := os.Stat(filePath); err != nil {
		return nil, nil
	}

	hashValue, err := computeFileHash(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to compute backup hash: %w", err)
	}
	plaintextHash := hashValue

	result := &backupSecurityResult{
		hashAlgo:  "sha256",
		hashValue: hashValue,
	}

	// Encryption is opt-in: only attempt encryption when an explicit key source is configured.
	var encInfo *manifest.EncryptionInfo
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

	// Write manifest alongside the backup file
	manifestPath := filePath + ".manifest.json"
	m := &manifest.BackupManifest{
		BackupID:     job.Name,
		Database:     job.Database,
		DatabaseType: job.Type,
		CreatedAt:    time.Now().UTC(),
		SizeBytes:    fileSize,
		Hash: manifest.HashInfo{
			Algorithm:      "sha256",
			Value:          result.hashValue,
			PlaintextValue: plaintextHash,
		},
		Encryption: encInfo,
	}
	if writeErr := manifest.WriteManifest(manifestPath, m); writeErr != nil {
		fmt.Printf("Warning: failed to write manifest for '%s': %v\n", job.Name, writeErr)
	} else {
		result.manifestPath = manifestPath
	}

	return result, nil
}

// computeFileHash returns the hex-encoded SHA-256 digest of the file at path.
func computeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file for hashing: %w", err)
	}
	defer f.Close()
	hw := crypto.NewHashingWriter(io.Discard)
	if _, err := io.Copy(hw, f); err != nil {
		return "", fmt.Errorf("failed to read file for hashing: %w", err)
	}
	return hw.Sum(), nil
}

// encryptBackupFile encrypts filePath in-place using AES-256-GCM via ChunkEncryptWriter.
// Returns (encrypted, encInfo, hashOfEncryptedFile, err).
func encryptBackupFile(cfg *config.Configuration, filePath, backupID string) (bool, *manifest.EncryptionInfo, string, error) {
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

	encInfo := &manifest.EncryptionInfo{
		Algorithm:     "AES-256-GCM",
		KeyDerivation: "PBKDF2-HMAC-SHA256",
		Iterations:    100_000,
		Salt:          base64.StdEncoding.EncodeToString(salt),
		IV:            hex.EncodeToString(nonce),
		AuthTag:       hex.EncodeToString(authTag),
	}

	return true, encInfo, encHash, nil
}
