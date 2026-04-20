package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/pkg/backup/mariadb_dump"
	"github.com/denisakp/sentinel/pkg/backup/mongo_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysqlbinlog"
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
	"github.com/denisakp/sentinel/pkg/restore/mariadb_restore"
	"github.com/denisakp/sentinel/pkg/restore/mongo_restore"
	"github.com/denisakp/sentinel/pkg/restore/mysql_restore"
	"github.com/denisakp/sentinel/pkg/restore/pg_restore"
)

const defaultIncrementalMaxChainDepth = 6

// NormalizeIncrementalBackupConfig applies feature defaults for incremental backup policy.
func NormalizeIncrementalBackupConfig(job BackupJob) BackupJob {
	if job.IncrementalBackup == nil {
		return job
	}

	normalized := *job.IncrementalBackup
	if normalized.MaxChainDepth == 0 {
		normalized.MaxChainDepth = defaultIncrementalMaxChainDepth
	}
	if normalized.OplogWindowWarnHours == 0 {
		normalized.OplogWindowWarnHours = 24
	}

	job.IncrementalBackup = &normalized
	return job
}

// BuildStorageParams converts a job's storage configuration into storage.Params.
func BuildStorageParams(job BackupJob) *storage.Params {
	return &storage.Params{
		OutName:              job.Output,
		StorageType:          job.Storage.Type,
		LocalPath:            job.Storage.LocalPath,
		GoogleDriveFolderId:  job.Storage.GDriveFolderID,
		GoogleServiceAccount: job.Storage.GDriveSAFile,
		GCSBucket:            job.Storage.GCSBucket,
		GCSProjectID:         job.Storage.GCSProjectID,
		GCSCredentialsFile:   job.Storage.GCSCredentialsFile,
		AWSBucket:            job.Storage.S3Bucket,
		AWSRegion:            job.Storage.S3Region,
		AWSBucketEndpoint:    job.Storage.S3BucketEndpoint,
		AWSAccessKeyID:       job.Storage.S3AccessKeyID,
		AWSSecretAccessKey:   job.Storage.S3SecretAccessKey,
	}
}

// BuildPgDumpArgs maps a job into pg_dump arguments.
func BuildPgDumpArgs(job BackupJob, password string, additionalArgs string, storageParams *storage.Params) (*pg_dump.PgDumpArgs, error) {
	pgArgs := &pg_dump.PgDumpArgs{
		Host:             job.Host,
		Port:             portToString(job.Port),
		Username:         job.Username,
		Password:         password,
		Database:         job.Database,
		AdditionalArgs:   additionalArgs,
		Storage:          storageParams,
		CompressionLevel: 1,
	}

	if value, ok := optionString(job.DatabaseOptions, "pg_out_format"); ok {
		pgArgs.PgOutFormat = value
	}
	if value, ok := optionInt(job.DatabaseOptions, "compress"); ok {
		if value > 0 {
			pgArgs.Compress = true
			pgArgs.CompressionLevel = value
		}
	}
	if value, ok := optionString(job.DatabaseOptions, "pg_compression_algo"); ok {
		pgArgs.CompressionAlgorithm = value
		pgArgs.Compress = true
	}
	if value, ok := optionInt(job.DatabaseOptions, "pg_compression_level"); ok {
		pgArgs.Compress = true
		pgArgs.CompressionLevel = value
	}

	return pgArgs, nil
}

// BuildMySQLDumpArgs maps a job into mysqldump arguments.
func BuildMySQLDumpArgs(job BackupJob, password string, additionalArgs string, storageParams *storage.Params) (*mysql_dump.MySqlDumpArgs, error) {
	return &mysql_dump.MySqlDumpArgs{
		Host:           job.Host,
		Port:           portToString(job.Port),
		Username:       job.Username,
		Password:       password,
		Database:       job.Database,
		AdditionalArgs: additionalArgs,
		Storage:        storageParams,
	}, nil
}

// BuildMariaDBDumpArgs maps a job into mariadb-dump arguments.
func BuildMariaDBDumpArgs(job BackupJob, password string, additionalArgs string, storageParams *storage.Params) (*mariadb_dump.MariaDBDumpArgs, error) {
	return &mariadb_dump.MariaDBDumpArgs{
		Host:           job.Host,
		Port:           portToString(job.Port),
		Username:       job.Username,
		Password:       password,
		Database:       job.Database,
		AdditionalArgs: additionalArgs,
		Storage:        storageParams,
	}, nil
}

// BuildMongoDumpArgs maps a job into mongodump arguments.
func BuildMongoDumpArgs(job BackupJob, additionalArgs string, storageParams *storage.Params) (*mongo_dump.DumpMongoArgs, error) {
	return &mongo_dump.DumpMongoArgs{
		Uri:            job.URI,
		Database:       job.Database,
		Compress:       optionBool(job.DatabaseOptions, "gzip"),
		AdditionalArgs: additionalArgs,
		Storage:        storageParams,
	}, nil
}

// PasswordFromEnv resolves the password from the environment variable.
func PasswordFromEnv(envName string) (string, error) {
	if envName == "" {
		return "", fmt.Errorf("password_env is required")
	}
	value := os.Getenv(envName)
	if value == "" {
		return "", fmt.Errorf("environment variable '%s' is not set", envName)
	}
	return value, nil
}

func portToString(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
}

func optionInt(options map[string]interface{}, key string) (int, bool) {
	if options == nil {
		return 0, false
	}
	value, ok := options[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case uint64:
		return int(v), true
	default:
		return 0, false
	}
}

func optionString(options map[string]interface{}, key string) (string, bool) {
	if options == nil {
		return "", false
	}
	value, ok := options[key]
	if !ok {
		return "", false
	}
	str, ok := value.(string)
	return str, ok
}

func optionBool(options map[string]interface{}, key string) bool {
	if options == nil {
		return false
	}
	value, ok := options[key]
	if !ok {
		return false
	}
	flag, ok := value.(bool)
	if !ok {
		return false
	}
	return flag
}

// BuildAdditionalArgs builds a space-separated args string from database options.
func BuildAdditionalArgs(job BackupJob) string {
	var args []string
	for key, value := range job.DatabaseOptions {
		switch job.Type {
		case "mysql", "mariadb":
			if flagValue, ok := value.(bool); ok && flagValue {
				switch key {
				case "single_transaction":
					args = append(args, "--single-transaction")
				case "routines":
					args = append(args, "--routines")
				case "triggers":
					args = append(args, "--triggers")
				case "events":
					args = append(args, "--events")
				}
			}
		case "mongodb":
			if flagValue, ok := value.(bool); ok && flagValue {
				switch key {
				case "oplog":
					args = append(args, "--oplog")
				case "archive":
					args = append(args, "--archive")
				}
			}
		}
	}
	return strings.Join(args, " ")
}

// BuildAdvancedRestoreRequest normalizes restore-mode-specific fields for planner input.
func BuildAdvancedRestoreRequest(job RestoreJob) (*AdvancedRestoreRequest, error) {
	mode := job.RestoreMode
	if mode == "" {
		mode = "full"
	}

	request := &AdvancedRestoreRequest{
		RestoreMode:           mode,
		PITRInputValue:        job.PITRTimestamp,
		PITRTargetTimeline:    job.PITRTargetTimeline,
		IncrementalFromBackup: job.IncrementalFromBackup,
		ConfirmFullFallback:   job.ConfirmFullFallback,
		BinlogTargetTime:      job.MySQL.BinlogTargetTime,
		BinlogTargetPosition:  job.MySQL.BinlogTargetPosition,
	}

	if mode == "pitr" && job.PITRTimestamp != "" {
		parsed, err := time.Parse(time.RFC3339, job.PITRTimestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse pitr_timestamp: %w", err)
		}
		utc := parsed.UTC()
		request.PITRTimestampUTC = &utc
	}

	return request, nil
}

// BuildPgRestoreArgs maps a restore job into pg_restore arguments.
func BuildPgRestoreArgs(job RestoreJob, password, stagedPath string) (*pg_restore.RestoreArgs, error) {
	return &pg_restore.RestoreArgs{
		Host:           job.Host,
		Port:           job.Port,
		Username:       job.Username,
		Password:       password,
		Database:       job.Database,
		BackupPath:     stagedPath,
		OnConflict:     effectiveRestoreConflict(job),
		AllowCascade:   job.AllowCascade,
		AdditionalArgs: BuildRestoreAdditionalArgs(job),
	}, nil
}

// BuildMySQLRestoreArgs maps a restore job into mysql restore arguments.
func BuildMySQLRestoreArgs(job RestoreJob, password, stagedPath string) (*mysql_restore.RestoreArgs, error) {
	return &mysql_restore.RestoreArgs{
		Host:           job.Host,
		Port:           job.Port,
		Username:       job.Username,
		Password:       password,
		Database:       job.Database,
		BackupPath:     stagedPath,
		OnConflict:     effectiveRestoreConflict(job),
		AdditionalArgs: BuildRestoreAdditionalArgs(job),
	}, nil
}

// BuildMariaDBRestoreArgs maps a restore job into mariadb restore arguments.
func BuildMariaDBRestoreArgs(job RestoreJob, password, stagedPath string) (*mariadb_restore.RestoreArgs, error) {
	return &mariadb_restore.RestoreArgs{
		Host:           job.Host,
		Port:           job.Port,
		Username:       job.Username,
		Password:       password,
		Database:       job.Database,
		BackupPath:     stagedPath,
		OnConflict:     effectiveRestoreConflict(job),
		AdditionalArgs: BuildRestoreAdditionalArgs(job),
	}, nil
}

// BuildMySQLBinlogReplayArgs maps a restore job into mysqlbinlog replay arguments.
func BuildMySQLBinlogReplayArgs(job RestoreJob, password string, binlogSources []string) (*mysqlbinlog.ReplayArgs, error) {
	if job.Type != "mysql" && job.Type != "mariadb" {
		return nil, fmt.Errorf("mysql binlog replay is only valid for mysql or mariadb jobs")
	}

	args := &mysqlbinlog.ReplayArgs{
		Engine:        job.Type,
		Host:          job.Host,
		Port:          job.Port,
		Username:      job.Username,
		Password:      password,
		Database:      job.Database,
		BinlogSources: append([]string{}, binlogSources...),
		TargetTime:    job.MySQL.BinlogTargetTime,
	}

	if job.MySQL.BinlogTargetPosition != nil {
		args.TargetPosition = &mysqlbinlog.BinlogPosition{
			File: job.MySQL.BinlogTargetPosition.File,
			Pos:  job.MySQL.BinlogTargetPosition.Pos,
		}
	}

	return args, nil
}

// BuildMongoRestoreArgs maps a restore job into mongorestore arguments.
func BuildMongoRestoreArgs(job RestoreJob, stagedPath string) (*mongo_restore.RestoreArgs, error) {
	return &mongo_restore.RestoreArgs{
		URI:            job.URI,
		Database:       job.Database,
		BackupPath:     stagedPath,
		OnConflict:     effectiveRestoreConflict(job),
		Gzip:           optionBool(job.RestoreOptions, "gzip"),
		Archive:        optionBool(job.RestoreOptions, "archive"),
		AdditionalArgs: BuildRestoreAdditionalArgs(job),
	}, nil
}

// RestorePasswordFromEnv resolves a restore password from the configured env var.
func RestorePasswordFromEnv(envName string) (string, error) {
	if envName == "" {
		return "", nil
	}
	value := os.Getenv(envName)
	if value == "" {
		return "", fmt.Errorf("environment variable '%s' is not set", envName)
	}
	return value, nil
}

// BuildMongoOplogReplayArgs maps a restore job and an oplog archive path into
// a mongo_restore.OplogReplayArgs for incremental restore replay.
func BuildMongoOplogReplayArgs(job RestoreJob, archivePath string) (*mongo_restore.OplogReplayArgs, error) {
	if job.Type != "mongodb" {
		return nil, fmt.Errorf("mongodb oplog replay is only valid for mongodb jobs")
	}
	if strings.TrimSpace(archivePath) == "" {
		return nil, fmt.Errorf("oplog archive path is required")
	}
	uri := job.URI
	if strings.TrimSpace(uri) == "" {
		return nil, fmt.Errorf("uri is required for mongodb oplog replay")
	}
	return &mongo_restore.OplogReplayArgs{
		URI:         uri,
		ArchivePath: archivePath,
	}, nil
}

// BuildRestoreAdditionalArgs builds a space-separated args string from restore options.
func BuildRestoreAdditionalArgs(job RestoreJob) string {
	if len(job.RestoreOptions) == 0 {
		return ""
	}
	var args []string
	for key, value := range job.RestoreOptions {
		if flag, ok := value.(bool); ok && flag {
			switch key {
			case "clean":
				args = append(args, "--clean")
			case "if_exists":
				args = append(args, "--if-exists")
			case "no_owner":
				args = append(args, "--no-owner")
			case "no_privileges":
				args = append(args, "--no-privileges")
			case "gzip":
				if job.Type == "mongodb" {
					args = append(args, "--gzip")
				}
			}
		}
	}
	return strings.Join(args, " ")
}

func effectiveRestoreConflict(job RestoreJob) string {
	if job.ConflictStrategy == "" {
		return "error"
	}
	return job.ConflictStrategy
}
