package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/restore/incremental/mysqlbinlog"
	"github.com/denisakp/sentinel/internal/adapters/storage"
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
// ports.DumpJobSpec consumed by ports.DumpArgsFactory. It
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

// BuildRestoreJobSpec translates a YAML RestoreJob into the pure, engine-agnostic
// ports.RestoreJobSpec consumed by ports.RestoreArgsFactory.
// It owns conflict/gzip/archive/additional-args resolution; the per-engine restore
// adapters copy the resolved fields into their *RestoreArgs / *OplogReplayArgs.
// stagedPath feeds the primary restore; archivePath feeds the mongo oplog replay.
func BuildRestoreJobSpec(job RestoreJob, password, stagedPath, archivePath string) ports.RestoreJobSpec {
	return ports.RestoreJobSpec{
		Engine:         job.Type,
		Host:           job.Host,
		Port:           job.Port,
		Username:       job.Username,
		Password:       password,
		Database:       job.Database,
		URI:            job.URI,
		BackupPath:     stagedPath,
		ArchivePath:    archivePath,
		OnConflict:     effectiveRestoreConflict(job),
		AllowCascade:   job.AllowCascade,
		Gzip:           optionBool(job.RestoreOptions, "gzip"),
		Archive:        optionBool(job.RestoreOptions, "archive"),
		AdditionalArgs: BuildRestoreAdditionalArgs(job),
	}
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

// BuildRestoreAdditionalArgs builds a space-separated args string from restore
// options: the recognised boolean flags first, then the operator's own
// restore_options.additional_args string.
//
// That string used to be dropped. It was parsed and validated at configuration
// load and then never read here, so engine-specific restore flags were accepted,
// reported as valid, and silently ignored. Nothing in the output said so (#172).
//
// The boolean keys are emitted in a fixed order. Ranging over the map produced a
// different argument order on every run, since Go randomises map iteration, which
// made the command non-reproducible and any assertion on it flaky.
func BuildRestoreAdditionalArgs(job RestoreJob) string {
	if len(job.RestoreOptions) == 0 {
		return ""
	}

	// Declared order, not map order.
	flagOrder := []struct {
		key, flag string
		mongoOnly bool
	}{
		{key: "clean", flag: "--clean"},
		{key: "if_exists", flag: "--if-exists"},
		{key: "no_owner", flag: "--no-owner"},
		{key: "no_privileges", flag: "--no-privileges"},
		{key: "gzip", flag: "--gzip", mongoOnly: true},
	}

	var args []string
	for _, f := range flagOrder {
		if f.mongoOnly && job.Type != "mongodb" {
			continue
		}
		if flag, ok := job.RestoreOptions[f.key].(bool); ok && flag {
			args = append(args, f.flag)
		}
	}

	// The operator's own arguments go last, so they sit nearest the engine
	// invocation and can override a flag derived above where the tool honours
	// the later occurrence.
	if raw, ok := job.RestoreOptions["additional_args"]; ok {
		if extra, isString := raw.(string); isString && strings.TrimSpace(extra) != "" {
			args = append(args, strings.TrimSpace(extra))
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
