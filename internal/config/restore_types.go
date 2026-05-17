package config

import (
	"fmt"
	"time"
)

// BinlogTargetPosition identifies a replay stop point in MySQL/MariaDB binlogs.
type BinlogTargetPosition struct {
	File string `yaml:"file"`
	Pos  int64  `yaml:"pos"`
}

// MySQLRestoreConfig holds MySQL/MariaDB incremental replay selectors.
type MySQLRestoreConfig struct {
	BinlogTargetTime     string                `yaml:"binlog_target_time,omitempty"`
	BinlogTargetPosition *BinlogTargetPosition `yaml:"binlog_target_position,omitempty"`
}

// MongoDBRestoreConfig holds MongoDB-specific restore options for oplog replay.
type MongoDBRestoreConfig struct {
	// OplogTargetTimestamp is an optional RFC3339 timestamp at which oplog replay stops.
	OplogTargetTimestamp string `yaml:"oplog_target_timestamp,omitempty"`
}

// RestoreJob represents a scheduled restore operation in YAML config
type RestoreJob struct {
	// Unique identifier (derived from YAML map key)
	Name string `yaml:"-"`

	// Whether this restore job is enabled (default: false - must be explicit)
	Enabled *bool `yaml:"enabled"`

	// Database type: "postgres", "mysql", "mariadb", "mongodb"
	Type string `yaml:"type"`

	// Host/URI connection parameters
	Host    string `yaml:"host,omitempty"`
	HostEnv string `yaml:"host_env,omitempty"`
	Port    int    `yaml:"port,omitempty"`

	// Username connection parameters
	Username    string `yaml:"username,omitempty"`
	UsernameEnv string `yaml:"username_env,omitempty"`

	// Password connection parameters (env-only, never inline)
	PasswordEnv string `yaml:"password_env,omitempty"`

	// MongoDB URI (env-only variant)
	URI    string `yaml:"uri,omitempty"`
	URIEnv string `yaml:"uri_env,omitempty"`

	// Database to restore into
	Database string `yaml:"database"`

	// Backup source configuration
	BackupSource RestoreBackupSource `yaml:"backup_source"`

	// Cron schedule for restore testing (5-field format)
	Schedule string `yaml:"schedule"`

	// RestoreMode selects restore execution mode: full, pitr, or incremental.
	RestoreMode string `yaml:"restore_mode,omitempty"`

	// PITRTimestamp is the operator-supplied timestamp for PITR requests.
	PITRTimestamp string `yaml:"pitr_timestamp,omitempty"`

	// PITRTargetTimeline optionally selects a recovery timeline for PITR.
	PITRTargetTimeline string `yaml:"pitr_target_timeline,omitempty"`

	// IncrementalFromBackup references the baseline backup for incremental planning.
	IncrementalFromBackup string `yaml:"incremental_from_backup,omitempty"`

	// ConfirmFullFallback authorizes fallback to full restore when required.
	ConfirmFullFallback bool `yaml:"confirm_full_fallback,omitempty"`

	// Restore-specific options
	RestoreOptions map[string]interface{} `yaml:"restore_options,omitempty"`

	// Post-restore verification
	VerifyAfterRestore bool `yaml:"verify_after_restore,omitempty"`

	// Maximum duration for restore operation
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty"`

	// Notification channels for restore results
	Notifications []NotificationChannel `yaml:"notifications,omitempty"`

	// What to do if data exists: "ignore", "replace", "error" (default: "error")
	ConflictStrategy string `yaml:"conflict_strategy,omitempty"`

	// AllowCascade is required for PostgreSQL replace operations that may
	// remove dependent objects through DROP ... CASCADE.
	AllowCascade bool `yaml:"allow_cascade,omitempty"`

	// StagingDir overrides the global restore staging directory for this job.
	StagingDir string `yaml:"staging_dir,omitempty"`

	// Retention policy for backup files used in restore
	Retention RestoreRetentionPolicy `yaml:"retention,omitempty"`

	// KeepFile prevents the staged restore artifact from being deleted after the
	// restore attempt.  Useful for debugging restore failures.
	KeepFile bool `yaml:"keep_file,omitempty"`

	// MySQL holds MySQL/MariaDB restore selectors for binlog-based replay.
	MySQL MySQLRestoreConfig `yaml:"mysql,omitempty"`

	// MongoDB holds MongoDB-specific restore options for oplog replay.
	MongoDB MongoDBRestoreConfig `yaml:"mongodb,omitempty"`
}

// RestoreRetentionPolicy defines how long to keep restore backup files
type RestoreRetentionPolicy struct {
	// Keep last N restore backups (0 = no limit)
	KeepLast int `yaml:"keep_last,omitempty"`

	// Keep restore backups from last N days (0 = no limit)
	KeepDays int `yaml:"keep_days,omitempty"`

	// Enable dry-run mode to preview deletions without removing files
	DryRun bool `yaml:"dry_run,omitempty"`
}

// RestoreBackupSource specifies where to read the backup from
type RestoreBackupSource struct {
	// Source type: "local", "s3", or "gcs"
	Type string `yaml:"type"`

	// Local filesystem path
	LocalPath string `yaml:"local_path,omitempty"`

	// S3-compatible storage
	S3Bucket             string `yaml:"s3_bucket,omitempty"`
	S3BucketEndpoint     string `yaml:"s3_bucket_endpoint,omitempty"`
	S3Region             string `yaml:"s3_region,omitempty"`
	S3AccessKeyID        string `yaml:"s3_access_key_id,omitempty"`
	S3AccessKeyIDEnv     string `yaml:"s3_access_key_id_env,omitempty"`
	S3SecretAccessKey    string `yaml:"s3_secret_access_key,omitempty"`
	S3SecretAccessKeyEnv string `yaml:"s3_secret_access_key_env,omitempty"`

	// Google Cloud Storage
	GCSBucket          string `yaml:"gcs_bucket,omitempty"`
	GCSProjectID       string `yaml:"gcs_project_id,omitempty"`
	GCSCredentialsFile string `yaml:"gcs_credentials_file,omitempty"`

	// Google Drive storage
	GDriveFolderID string `yaml:"gdrive_folder_id,omitempty"`
	GDriveSAFile   string `yaml:"gdrive_sa_file,omitempty"`

	// Azure Blob Storage
	AzureStorageAccount    string `yaml:"azure_storage_account,omitempty"`
	AzureStorageAccountEnv string `yaml:"azure_storage_account_env,omitempty"`
	AzureStorageKey        string `yaml:"azure_storage_key,omitempty"`
	AzureStorageKeyEnv     string `yaml:"azure_storage_key_env,omitempty"`
	AzureContainer         string `yaml:"azure_container,omitempty"`

	// S3-hosted object path or filename
	BackupPath string `yaml:"backup_path"`

	// If backup_path is a pattern (e.g., *.sql), select latest match
	UseLatestMatch bool `yaml:"use_latest_match,omitempty"`
}

// RestoreConfiguration extends Configuration to support restore jobs
type RestoreConfiguration struct {
	// Inherited from base Configuration
	Version              string
	Defaults             GlobalDefaults
	MaxConcurrentBackups int
	LogFormat            string
	HistoryDBPath        string

	// Backup job definitions (existing)
	Databases map[string]BackupJob `yaml:"databases"`

	// Restore job definitions (new)
	Restores map[string]RestoreJob `yaml:"restores,omitempty"`

	// Global restore options
	RestoreDefaults RestoreDefaults `yaml:"restore_defaults,omitempty"`
}

// RestoreDefaults provides default values for all restore jobs
// AdvancedRestoreRequest captures normalized advanced restore input consumed by planner/executor flows.
type AdvancedRestoreRequest struct {
	RestoreMode           string
	PITRTimestampUTC      *time.Time
	PITRInputValue        string
	PITRTargetTimeline    string
	IncrementalFromBackup string
	ConfirmFullFallback   bool
	BinlogTargetTime      string
	BinlogTargetPosition  *BinlogTargetPosition
}

type RestoreDefaults struct {
	// Default verification on/off
	VerifyAfterRestore bool `yaml:"verify_after_restore,omitempty"`

	// Default conflict strategy
	ConflictStrategy string `yaml:"conflict_strategy,omitempty"`

	// Default restore timeout in seconds
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty"`

	// Default notification channels
	Notifications []NotificationChannel `yaml:"notifications,omitempty"`

	// Default backup source
	BackupSource RestoreBackupSource `yaml:"backup_source,omitempty"`

	// Default retention policy for restore backups
	Retention RestoreRetentionPolicy `yaml:"retention,omitempty"`
}

// ValidateRestoreJob validates a restore job configuration
func ValidateRestoreJob(job *RestoreJob) error {
	if job == nil {
		return fmt.Errorf("restore job cannot be nil")
	}

	if job.Name == "" {
		return fmt.Errorf("restore job name is required")
	}

	if job.Type == "" {
		return fmt.Errorf("restore job type is required")
	}
	if job.StagingDir == "" {
		return fmt.Errorf("staging_dir is required")
	}

	// Validate type
	validTypes := map[string]bool{
		"postgres": true,
		"mysql":    true,
		"mariadb":  true,
		"mongodb":  true,
	}
	if !validTypes[job.Type] {
		return fmt.Errorf("unsupported restore job type: %s", job.Type)
	}

	// Validate connection params by type
	switch job.Type {
	case "postgres", "mysql", "mariadb":
		if job.Host == "" && job.HostEnv == "" {
			return fmt.Errorf("host or host_env is required for %s restore", job.Type)
		}
		if job.Username == "" && job.UsernameEnv == "" {
			return fmt.Errorf("username or username_env is required for %s restore", job.Type)
		}
		if job.Database == "" {
			return fmt.Errorf("database is required for %s restore", job.Type)
		}
	case "mongodb":
		if job.URI == "" && job.URIEnv == "" {
			return fmt.Errorf("uri or uri_env is required for MongoDB restore")
		}
	}

	// Validate schedule — only required for enabled jobs
	enabled := job.Enabled == nil || *job.Enabled
	if enabled && job.Schedule == "" {
		return fmt.Errorf("restore schedule (cron) is required")
	}

	// Validate conflict strategy if specified
	if job.ConflictStrategy != "" {
		validStrategies := map[string]bool{
			"ignore":  true,
			"replace": true,
			"error":   true,
		}
		if !validStrategies[job.ConflictStrategy] {
			return fmt.Errorf("invalid conflict_strategy: %s", job.ConflictStrategy)
		}
	}
	if job.AllowCascade && job.Type != "postgres" {
		return fmt.Errorf("allow_cascade is only supported for postgres restores")
	}
	if job.Type == "postgres" && job.ConflictStrategy == "replace" && !job.AllowCascade {
		return fmt.Errorf("conflict_strategy=replace for postgres requires allow_cascade: true (DROP ... CASCADE may remove dependent objects)")
	}

	// Validate retention policy
	if job.Retention.KeepLast == 0 && job.Retention.KeepDays == 0 {
		// Both zero means unlimited retention, which is valid
	} else if job.Retention.KeepLast < 0 || job.Retention.KeepDays < 0 {
		return fmt.Errorf("retention policy values must be non-negative")
	}

	// Validate backup source
	if job.BackupSource.Type == "" {
		return fmt.Errorf("backup_source.type is required")
	}
	if job.BackupSource.BackupPath == "" {
		return fmt.Errorf("backup_source.backup_path is required")
	}

	switch job.BackupSource.Type {
	case "local":
		if job.BackupSource.LocalPath == "" {
			return fmt.Errorf("backup_source.local_path is required for local type")
		}
	case "s3":
		if job.BackupSource.S3Bucket == "" {
			return fmt.Errorf("backup_source.s3_bucket is required for s3 type")
		}
	case "gcs":
		if job.BackupSource.GCSBucket == "" {
			return fmt.Errorf("backup_source.gcs_bucket is required for gcs type")
		}
	default:
		return fmt.Errorf("unsupported backup_source.type: %s", job.BackupSource.Type)
	}

	return nil
}

// ApplyRestoreDefaults applies default values to restore jobs
func ApplyRestoreDefaults(restores map[string]RestoreJob, defaults RestoreDefaults) {
	for name, job := range restores {
		job.Name = name

		if job.Enabled == nil {
			job.Enabled = boolPtr(false) // Restore jobs default to disabled
		}

		if !job.VerifyAfterRestore && defaults.VerifyAfterRestore {
			job.VerifyAfterRestore = defaults.VerifyAfterRestore
		}

		if job.ConflictStrategy == "" && defaults.ConflictStrategy != "" {
			job.ConflictStrategy = defaults.ConflictStrategy
		}

		if job.TimeoutSeconds == 0 && defaults.TimeoutSeconds > 0 {
			job.TimeoutSeconds = defaults.TimeoutSeconds
		}

		if len(job.Notifications) == 0 && len(defaults.Notifications) > 0 {
			job.Notifications = defaults.Notifications
		}

		// Apply retention defaults if not specified
		if job.Retention.KeepLast == 0 && job.Retention.KeepDays == 0 &&
			(defaults.Retention.KeepLast > 0 || defaults.Retention.KeepDays > 0) {
			job.Retention = defaults.Retention
		}

		restores[name] = job
	}
}
