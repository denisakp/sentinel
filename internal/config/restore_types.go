package config

import (
	"fmt"
)

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

	// Retention policy for backup files used in restore
	Retention RestoreRetentionPolicy `yaml:"retention,omitempty"`

	// KeepFile prevents the staged restore artifact from being deleted after the
	// restore attempt.  Useful for debugging restore failures.
	KeepFile bool `yaml:"keep_file,omitempty"`
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
	// Source type: "local", "s3", "google-drive", "azure", "gcs"
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

	// Validate schedule
	if job.Schedule == "" {
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
	case "google-drive":
		if job.BackupSource.GDriveFolderID == "" {
			return fmt.Errorf("backup_source.gdrive_folder_id is required for google-drive type")
		}
	case "azure":
		if job.BackupSource.AzureContainer == "" {
			return fmt.Errorf("backup_source.azure_container is required for azure type")
		}
		if job.BackupSource.AzureStorageAccount == "" && job.BackupSource.AzureStorageAccountEnv == "" {
			return fmt.Errorf("backup_source.azure_storage_account or backup_source.azure_storage_account_env is required for azure type")
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
