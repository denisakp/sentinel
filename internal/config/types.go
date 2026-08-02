package config

import (
	"time"
)

// TLSConfig holds TLS/SSL settings for a database connection.
type TLSConfig struct {
	// Enabled controls whether TLS is used (default: true when tls: section present)
	Enabled bool `yaml:"enabled"`

	// Mode is one of: require, verify-ca, verify-full, prefer (default: prefer)
	Mode string `yaml:"mode"`

	// CACertPath is the path to the CA certificate file
	CACertPath string `yaml:"ca_cert,omitempty"`

	// ClientCert is the path to the client certificate file (mutual TLS)
	ClientCert string `yaml:"client_cert,omitempty"`

	// ClientKey is the path to the client private key file (mutual TLS)
	ClientKey string `yaml:"client_key,omitempty"`

	// ClientKeyPasswordEnv names an environment variable that holds the passphrase
	// for an encrypted ClientKey. The passphrase value itself is never stored in
	// the config file or passed on the command line.
	ClientKeyPasswordEnv string `yaml:"client_key_password_env,omitempty"`
}

// SchedulerConfig holds global scheduler and concurrency settings.
type SchedulerConfig struct {
	// MaxConcurrentBackups is the global concurrency limit for backup jobs (default: 3)
	MaxConcurrentBackups int `yaml:"max_concurrent_backups"`

	// MaxConcurrentRestores is the global concurrency limit for restore jobs (default: 1)
	MaxConcurrentRestores int `yaml:"max_concurrent_restores"`

	// JobTimeoutMinutes is the per-job timeout in minutes (default: 180)
	JobTimeoutMinutes int `yaml:"job_timeout_minutes"`

	// StaleLockThreshold is the age in minutes after which a lock with a dead PID is considered stale (default: 60)
	StaleLockThreshold int `yaml:"stale_lock_threshold"`

	// LockDir is the directory for job lock files (default: /var/run/sentinel)
	LockDir string `yaml:"lock_dir"`
}

// RestoreRuntimeConfig holds shared runtime settings for restore execution.
type RestoreRuntimeConfig struct {
	// StagingDir is the base directory used for staged restore artifacts.
	StagingDir string `yaml:"staging_dir,omitempty"`

	// KeepFile retains staged artifacts after execution for debugging.
	KeepFile bool `yaml:"keep_file,omitempty"`
}

// IncrementalBackupConfig holds chain policy and engine pre-check settings.
type IncrementalBackupConfig struct {
	Enabled bool `yaml:"enabled"`

	// MaxChainDepth controls when a chain is reset by producing a new full backup.
	// Default is 6 when omitted.
	MaxChainDepth int `yaml:"max_chain_depth,omitempty"`

	// WalSummaryCheck verifies PostgreSQL wal_summary=on before incremental backup.
	WalSummaryCheck bool `yaml:"wal_summary_check,omitempty"`

	// BinlogCheck verifies MySQL/MariaDB log_bin=ON before incremental backup.
	BinlogCheck bool `yaml:"binlog_check,omitempty"`

	// OplogWindowWarnHours emits a warning when MongoDB oplog window is below threshold.
	OplogWindowWarnHours int `yaml:"oplog_window_warn_hours,omitempty"`
}

// MySQLConfig holds MySQL/MariaDB-specific options.
type MySQLConfig struct {
	// BinlogPath is a local or mounted path readable by Sentinel.
	BinlogPath string `yaml:"binlog_path,omitempty"`
}

// AzureAuthConfig specifies authentication method for Azure Blob Storage.
type AzureAuthConfig struct {
	// Type is one of: managed_identity, connection_string, sas_token
	Type string `yaml:"type"`

	// ConnectionString is the Azure Storage connection string (connection_string auth)
	ConnectionString string `yaml:"connection_string,omitempty"`

	// ConnectionStringEnv is the env var name for the connection string
	ConnectionStringEnv string `yaml:"connection_string_env,omitempty"`

	// SASToken is the SAS token query string (sas_token auth)
	SASToken string `yaml:"sas_token,omitempty"`

	// SASTokenEnv is the env var name for the SAS token
	SASTokenEnv string `yaml:"sas_token_env,omitempty"`
}

// AzureConfig holds Azure Blob Storage backend settings.
type AzureConfig struct {
	// AccountName is the Azure storage account name
	AccountName string `yaml:"account_name"`

	// Container is the blob container name
	Container string `yaml:"container"`

	// Tier is the storage access tier: Hot, Cool, Archive (default: Hot)
	Tier string `yaml:"tier,omitempty"`

	// Auth holds authentication configuration
	Auth AzureAuthConfig `yaml:"auth"`
}

// Configuration represents the top-level YAML configuration file
type Configuration struct {
	// Version of the configuration schema (e.g., "1.0")
	Version string `yaml:"version"`

	// Defaults applied to all backups unless overridden
	Defaults GlobalDefaults `yaml:"defaults"`

	// Backup job definitions keyed by job name
	Databases map[string]BackupJob `yaml:"databases"`

	// Restore job definitions keyed by job name
	Restores map[string]RestoreJob `yaml:"restores,omitempty"`

	// Restore holds shared runtime settings for restore execution.
	Restore RestoreRuntimeConfig `yaml:"restore,omitempty"`

	// Named storage configurations (reusable references)
	Storages map[string]StorageConfig `yaml:"storages,omitempty"`

	// Global concurrency limit (default: 3) — kept for backward compatibility
	MaxConcurrentBackups int `yaml:"max_concurrent_backups"`

	// Global concurrency limit for `restore run --all` (default: 1 — serial;
	// parallelism is opt-in). Spec 045 / PRD 31.
	MaxConcurrentRestores int `yaml:"max_concurrent_restores"`

	// Scheduler holds advanced concurrency and timeout settings
	Scheduler SchedulerConfig `yaml:"scheduler,omitempty"`

	// Log format: "json" or "text" (default: "json")
	LogFormat string `yaml:"log_format"`

	// Path to SQLite backup history database (default: ~/.sentinel/history.db)
	HistoryDBPath string `yaml:"history_db_path"`

	// EncryptionKeyEnv is an optional env var name for the master encryption key.
	// Encryption is enabled only when this or EncryptionKeyFile is explicitly configured.
	EncryptionKeyEnv string `yaml:"encryption_key_env,omitempty"`

	// EncryptionKeyFile is an optional path to a file containing the base64-encoded master key.
	// Encryption is enabled only when this or EncryptionKeyEnv is explicitly configured.
	EncryptionKeyFile string `yaml:"encryption_key_file,omitempty"`
}

// GlobalDefaults contains default values applied to all backup jobs
type GlobalDefaults struct {
	// Default cron schedule for backup jobs without an explicit schedule
	Schedule string `yaml:"schedule,omitempty"`

	// Default storage configuration
	Storage StorageConfig `yaml:"storage"`

	// Default retention policy
	Retention RetentionPolicy `yaml:"retention"`

	// Default notification channels
	Notifications []NotificationChannel `yaml:"notifications"`
}

// BackupJob represents a single backup definition in the configuration
// This is a discriminated union that maps to one of: PostgresJob, MySQLJob, MariaDBJob, MongoDBJob
type BackupJob struct {
	// Unique identifier (derived from YAML map key)
	Name string `yaml:"-"`

	// Database type: "postgres", "mysql", "mariadb", "mongodb"
	Type string `yaml:"type"`

	// Whether this job is enabled (default: true)
	Enabled *bool `yaml:"enabled"`

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

	// Database selection: single name or "*" for auto-discovery
	Database string `yaml:"database"`

	// Databases to exclude (only valid with database: "*")
	Exclude []string `yaml:"exclude,omitempty"`

	// Auto-discovery strategy: "individual" or "single" (only valid with database: "*")
	Strategy string `yaml:"strategy,omitempty"`

	// Output filename (defaults to SENTINEL_2006-01-02T15-04-05.ext)
	Output string `yaml:"output,omitempty"`

	// Storage configuration for this backup
	Storage StorageConfig `yaml:"storage,omitempty"`

	// Database-specific options (validated by type)
	// PostgreSQL: pg_out_format, compress
	// MySQL/MariaDB: single_transaction, routines, triggers, events
	// MongoDB: gzip, oplog, archive
	DatabaseOptions map[string]interface{} `yaml:"database_options,omitempty"`

	// PITREnabled enables capture of PITR-related metadata for this backup job.
	PITREnabled bool `yaml:"pitr_enabled,omitempty"`

	// WALArchivePrefix describes where archived WAL segments can be retrieved.
	WALArchivePrefix string `yaml:"wal_archive_prefix,omitempty"`

	// IncrementalMetadataEnabled enables lineage metadata capture for future incremental restores.
	IncrementalMetadataEnabled bool `yaml:"incremental_metadata_enabled,omitempty"`

	// IncrementalBackup enables chain-based incremental backup behavior.
	IncrementalBackup *IncrementalBackupConfig `yaml:"incremental_backup,omitempty"`

	// MySQL holds MySQL/MariaDB engine-specific options.
	MySQL MySQLConfig `yaml:"mysql,omitempty"`

	// Cron expression for scheduling (5-field format)
	Schedule string `yaml:"schedule,omitempty"`

	// Retention policy for this backup
	Retention RetentionPolicy `yaml:"retention,omitempty"`

	// Notification channels for this backup
	Notifications []NotificationChannel `yaml:"notifications,omitempty"`

	// TLS holds TLS/SSL settings for the database connection
	TLS *TLSConfig `yaml:"tls,omitempty"`
}

// StorageConfig defines a storage backend for backups
// This is a discriminated union that maps to one of: LocalStorage, S3Storage, GCSStorage, GoogleDriveStorage, AzureStorage
type StorageConfig struct {
	// Storage type: "local", "s3", "gcs", "google-drive", "azure"
	Type string `yaml:"type"`

	// Optional named storage reference from top-level storages map
	Name string `yaml:"name,omitempty"`

	// Local filesystem storage
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
}

// RetentionPolicy defines how long to keep backups
type RetentionPolicy struct {
	// Keep the N most recent backups
	KeepLast int `yaml:"keep_last,omitempty"`

	// Keep backups from the last N days
	KeepDays int `yaml:"keep_days,omitempty"`

	// Dry-run mode: preview deletions without executing
	DryRun bool `yaml:"dry_run,omitempty"`
}

// NotificationChannel defines how to send backup alerts (discriminated union)
// This is a placeholder; actual type is determined by the Type field and parsed into:
// - WebhookNotification (type: slack, discord, webhook)
// - EmailNotification (type: email)
type NotificationChannel struct {
	// Notification type: "slack", "discord", "webhook", "email"
	Type string `yaml:"type"`

	// Webhook notifications (slack, discord, webhook)
	WebhookURLEnv  string `yaml:"webhook_url_env,omitempty"` // Env var for webhook URL
	TimeoutSeconds int    `yaml:"timeout_seconds,omitempty"` // HTTP timeout (default: 10)

	// Email notifications
	SMTPHost        string   `yaml:"smtp_host,omitempty"`
	SMTPPort        int      `yaml:"smtp_port,omitempty"`
	SMTPUsernameEnv string   `yaml:"smtp_username_env,omitempty"`
	SMTPPasswordEnv string   `yaml:"smtp_password_env,omitempty"`
	FromAddressEnv  string   `yaml:"from_address_env,omitempty"`
	ToAddresses     []string `yaml:"to_addresses,omitempty"`
	UseTLS          *bool    `yaml:"use_tls"`

	// Events that trigger notifications
	// Valid values: "success", "failure", "warning"
	Events []string `yaml:"events,omitempty"`

	// Enable/disable this channel (default: true)
	Enabled *bool `yaml:"enabled"`
}

// BackupExecution represents a single backup execution record
type BackupExecution struct {
	// Unique identifier
	ID string

	// Backup job name
	BackupName string

	// Execution timestamp
	Timestamp time.Time

	// Backup status: "success", "failure", "in-progress"
	Status string

	// Execution duration
	Duration time.Duration

	// Output file/path in storage
	FilePath string

	// Backup file size in bytes
	FileSize int64

	// Error message if failed
	ErrorMessage string

	// SHA256 checksum of backup (if integrity enabled)
	SHA256Checksum string
}

// ScheduledJob represents a scheduled backup with cron timing information
type ScheduledJob struct {
	BackupJob BackupJob
	Schedule  string
	NextRun   time.Time
	LastRun   time.Time
	Enabled   bool
}
