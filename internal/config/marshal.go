package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/adapters/restore/incremental/mysqlbinlog"
	mariadb_restore "github.com/denisakp/sentinel/internal/adapters/restore/mariadb"
	mongo_restore "github.com/denisakp/sentinel/internal/adapters/restore/mongo"
	mysql_restore "github.com/denisakp/sentinel/internal/adapters/restore/mysql"
	pg_restore "github.com/denisakp/sentinel/internal/adapters/restore/pg"
	"github.com/denisakp/sentinel/internal/ports"
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
		AzureStorageAccount:  job.Storage.AzureStorageAccount,
		AzureStorageKey:      job.Storage.AzureStorageKey,
		AzureContainer:       job.Storage.AzureContainer,
	}
}

// BuildDumpJobSpec translates a YAML BackupJob into the pure, engine-agnostic
// ports.DumpJobSpec consumed by ports.DumpArgsFactory (spec 040 / PRD 27). It
// owns all DatabaseOptions resolution; the per-engine adapters copy the resolved
// fields into their *DumpArgs. Storage params are set on the concrete result by
// the command layer, not carried here.
func BuildDumpJobSpec(job BackupJob, password string, additionalArgs string) ports.DumpJobSpec {
	spec := ports.DumpJobSpec{
		Engine:         job.Type,
		Host:           job.Host,
		Port:           portToString(job.Port),
		Username:       job.Username,
		Password:       password,
		Database:       job.Database,
		URI:            job.URI,
		AdditionalArgs: additionalArgs,
	}

	switch job.Type {
	case "postgres":
		spec.CompressionLevel = 1
		if value, ok := optionString(job.DatabaseOptions, "pg_out_format"); ok {
			spec.PgOutFormat = value
		}
		if value, ok := optionInt(job.DatabaseOptions, "compress"); ok {
			if value > 0 {
				spec.Compress = true
				spec.CompressionLevel = value
			}
		}
		if value, ok := optionString(job.DatabaseOptions, "pg_compression_algo"); ok {
			spec.CompressionAlgorithm = value
			spec.Compress = true
		}
		if value, ok := optionInt(job.DatabaseOptions, "pg_compression_level"); ok {
			spec.Compress = true
			spec.CompressionLevel = value
		}
	case "mongodb":
		spec.Compress = optionBool(job.DatabaseOptions, "gzip")
	}

	return spec
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
