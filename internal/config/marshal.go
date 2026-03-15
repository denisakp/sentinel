package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/pkg/backup/mariadb_dump"
	"github.com/denisakp/sentinel/pkg/backup/mongo_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
)

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
