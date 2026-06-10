package config

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	backup "github.com/denisakp/sentinel/internal/domain/backup"
	backupincremental "github.com/denisakp/sentinel/internal/domain/backup/incremental"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/adapters/storage"
)

var allowedPostgresOptions = map[string]bool{
	"pg_out_format":        true,
	"compress":             true,
	"pg_compression_algo":  true,
	"pg_compression_level": true,
}

var allowedMySQLOptions = map[string]bool{
	"single_transaction": true,
	"routines":           true,
	"triggers":           true,
	"events":             true,
}

var allowedMongoOptions = map[string]bool{
	"gzip":    true,
	"oplog":   true,
	"archive": true,
}

var envVarNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ValidateConfig validates all configuration rules.
func ValidateConfig(cfg *Configuration) error {
	if cfg.Version != "1.0" {
		return fmt.Errorf("invalid config version '%s': expected '1.0'", cfg.Version)
	}
	if len(cfg.Databases) == 0 {
		return fmt.Errorf("no backup jobs defined in configuration")
	}
	if cfg.MaxConcurrentBackups < 1 || cfg.MaxConcurrentBackups > 100 {
		return fmt.Errorf("max_concurrent_backups must be between 1 and 100")
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	for name, job := range cfg.Databases {
		if err := backup.ValidateDbType(job.Type); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if job.Database == "" {
			return fmt.Errorf("backup '%s': database field is required", name)
		}
		if job.Database == "*" {
			if job.Strategy != "" && job.Strategy != "individual" && job.Strategy != "single" {
				return fmt.Errorf("backup '%s': strategy must be 'individual' or 'single'", name)
			}
		}

		if job.Schedule != "" {
			if strings.TrimSpace(job.Schedule) == "" {
				return fmt.Errorf("backup '%s': invalid cron expression '%s': schedule cannot be whitespace-only", name, job.Schedule)
			}
			if _, err := parser.Parse(job.Schedule); err != nil {
				return fmt.Errorf("backup '%s': invalid cron expression '%s': %w", name, job.Schedule, err)
			}
		}

		if err := validateConnection(job); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := validateEnvNames(job); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := validateStorage(job); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := validateRetention(job); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := validateDatabaseOptions(job); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := validateNotifications(job); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := validateIncrementalBackupPolicy(job); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}

		// T017: validate TLS configuration when present; warn when absent.
		if job.TLS != nil {
			tlsCfg := &ports.Config{
				Enabled:              job.TLS.Enabled,
				Mode:                 job.TLS.Mode,
				CACertPath:           job.TLS.CACertPath,
				ClientCert:           job.TLS.ClientCert,
				ClientKey:            job.TLS.ClientKey,
				ClientKeyPasswordEnv: job.TLS.ClientKeyPasswordEnv,
			}
			if err := tlsCfg.Validate(); err != nil {
				return fmt.Errorf("backup '%s': tls: %w", name, err)
			}
		} else {
			slog.Warn("TLS not configured for database",
				"event", "tls_not_configured",
				"database", name)
		}

		// T041: warn about plaintext credentials in storage configuration.
		warnPlaintextCredentials(name, job)
	}

	for name, job := range cfg.Restores {
		job.Name = name
		if err := ValidateRestoreJob(&job); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		if strings.TrimSpace(job.Schedule) != "" {
			if _, err := parser.Parse(job.Schedule); err != nil {
				return fmt.Errorf("restore '%s': invalid cron expression '%s': %w", name, job.Schedule, err)
			}
		}
		if job.TimeoutSeconds < 0 {
			return fmt.Errorf("restore '%s': timeout_seconds must be non-negative", name)
		}
		if err := validateAdvancedRestoreOptions(job); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		if err := validateRestoreEnvNames(job); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
	}

	return nil
}

// warnPlaintextCredentials emits structured log warnings for any storage
// credential fields that contain inline values instead of env-var references.
func warnPlaintextCredentials(name string, job BackupJob) {
	if job.Storage.S3AccessKeyID != "" {
		slog.Warn("plaintext credential detected",
			"event", "plaintext_password_detected",
			"field", fmt.Sprintf("databases.%s.storage.s3_access_key_id", name))
	}
	if job.Storage.S3SecretAccessKey != "" {
		slog.Warn("plaintext credential detected",
			"event", "plaintext_password_detected",
			"field", fmt.Sprintf("databases.%s.storage.s3_secret_access_key", name))
	}
	if job.Storage.AzureStorageKey != "" {
		slog.Warn("plaintext credential detected",
			"event", "plaintext_password_detected",
			"field", fmt.Sprintf("databases.%s.storage.azure_storage_key", name))
	}
}

func validateConnection(job BackupJob) error {
	if job.Type == "mongodb" {
		if job.URI == "" && job.URIEnv == "" {
			return fmt.Errorf("uri_env is required for mongodb backups")
		}
		return nil
	}

	if job.Host == "" && job.HostEnv == "" {
		return fmt.Errorf("host or host_env is required")
	}
	if job.Username == "" && job.UsernameEnv == "" {
		return fmt.Errorf("username or username_env is required")
	}
	if job.PasswordEnv == "" {
		return fmt.Errorf("password_env is required")
	}
	return nil
}

func validateEnvNames(job BackupJob) error {
	fields := map[string]string{
		"host_env":                  job.HostEnv,
		"username_env":              job.UsernameEnv,
		"password_env":              job.PasswordEnv,
		"uri_env":                   job.URIEnv,
		"s3_access_key_id_env":      job.Storage.S3AccessKeyIDEnv,
		"s3_secret_access_key_env":  job.Storage.S3SecretAccessKeyEnv,
		"azure_storage_account_env": job.Storage.AzureStorageAccountEnv,
		"azure_storage_key_env":     job.Storage.AzureStorageKeyEnv,
	}

	for field, value := range fields {
		if value == "" {
			continue
		}
		if !envVarNamePattern.MatchString(value) {
			return fmt.Errorf("%s must match pattern ^[A-Z_][A-Z0-9_]*$", field)
		}
	}

	for _, channel := range job.Notifications {
		channelFields := map[string]string{
			"webhook_url_env":   channel.WebhookURLEnv,
			"smtp_username_env": channel.SMTPUsernameEnv,
			"smtp_password_env": channel.SMTPPasswordEnv,
			"from_address_env":  channel.FromAddressEnv,
		}
		for field, value := range channelFields {
			if value == "" {
				continue
			}
			if !envVarNamePattern.MatchString(value) {
				return fmt.Errorf("%s must match pattern ^[A-Z_][A-Z0-9_]*$", field)
			}
		}
	}

	return nil
}

func validateRestoreEnvNames(job RestoreJob) error {
	fields := map[string]string{
		"host_env":                  job.HostEnv,
		"username_env":              job.UsernameEnv,
		"password_env":              job.PasswordEnv,
		"uri_env":                   job.URIEnv,
		"s3_access_key_id_env":      job.BackupSource.S3AccessKeyIDEnv,
		"s3_secret_access_key_env":  job.BackupSource.S3SecretAccessKeyEnv,
		"azure_storage_account_env": job.BackupSource.AzureStorageAccountEnv,
		"azure_storage_key_env":     job.BackupSource.AzureStorageKeyEnv,
	}

	for field, value := range fields {
		if value == "" {
			continue
		}
		if !envVarNamePattern.MatchString(value) {
			return fmt.Errorf("%s must match pattern ^[A-Z_][A-Z0-9_]*$", field)
		}
	}

	return nil
}

func validateAdvancedRestoreOptions(job RestoreJob) error {
	mode := job.RestoreMode
	if mode == "" {
		mode = "full"
	}

	if err := validateBinlogReplaySelectors(job); err != nil {
		return err
	}

	if err := validateOplogReplaySelectors(job); err != nil {
		return err
	}

	switch mode {
	case "full":
		if job.ConfirmFullFallback {
			return fmt.Errorf("confirm_full_fallback is only valid when restore_mode is incremental")
		}
		if job.PITRTimestamp != "" || job.PITRTargetTimeline != "" || job.IncrementalFromBackup != "" {
			return fmt.Errorf("advanced restore fields require restore_mode to be pitr or incremental")
		}
		return nil
	case "pitr":
		if job.Type != "postgres" {
			return fmt.Errorf("restore_mode pitr is currently supported only for postgres")
		}
		if job.PITRTimestamp == "" {
			return fmt.Errorf("pitr_timestamp is required when restore_mode is pitr")
		}
		if _, err := time.Parse(time.RFC3339, job.PITRTimestamp); err != nil {
			return fmt.Errorf("pitr_timestamp must be RFC3339 with timezone: %w", err)
		}
		if job.ConfirmFullFallback {
			return fmt.Errorf("confirm_full_fallback is only valid when restore_mode is incremental")
		}
		if job.IncrementalFromBackup != "" {
			return fmt.Errorf("incremental_from_backup is only valid when restore_mode is incremental")
		}
		return nil
	case "incremental":
		if job.IncrementalFromBackup == "" {
			return fmt.Errorf("incremental_from_backup is required when restore_mode is incremental")
		}
		if job.PITRTimestamp != "" || job.PITRTargetTimeline != "" {
			return fmt.Errorf("pitr fields are only valid when restore_mode is pitr")
		}
		switch job.Type {
		case "postgres", "mysql", "mariadb", "mongodb":
			// Supported for advanced incremental planning.
		default:
			return fmt.Errorf("restore_mode incremental is not supported for type '%s'", job.Type)
		}
		return nil
	default:
		return fmt.Errorf("restore_mode must be one of: full, pitr, incremental")
	}
}

func validateIncrementalBackupPolicy(job BackupJob) error {
	if job.IncrementalBackup == nil || !job.IncrementalBackup.Enabled {
		return nil
	}

	if job.IncrementalBackup.MaxChainDepth < 0 {
		return fmt.Errorf("incremental_backup.max_chain_depth must be greater than or equal to 0")
	}

	switch job.Type {
	case "postgres":
		return nil
	case "mysql", "mariadb":
		if result := backupincremental.ValidateMySQLIncrementalPrerequisites(true, job.MySQL.BinlogPath); !result.Passed {
			return fmt.Errorf("%s (type=%s)", result.Detail, job.Type)
		}
		return nil
	case "mongodb":
		if result := backupincremental.ValidateMongoIncrementalPrerequisites(true); !result.Passed {
			return fmt.Errorf("%s (type=mongodb)", result.Detail)
		}
		if job.IncrementalBackup.OplogWindowWarnHours < 0 {
			return fmt.Errorf("incremental_backup.oplog_window_warn_hours must be greater than or equal to 0")
		}
		return nil
	default:
		return fmt.Errorf("incremental backup is not supported for database type '%s'", job.Type)
	}
}

func validateBinlogReplaySelectors(job RestoreJob) error {
	hasTargetTime := strings.TrimSpace(job.MySQL.BinlogTargetTime) != ""
	hasTargetPos := job.MySQL.BinlogTargetPosition != nil

	if hasTargetTime && hasTargetPos {
		return fmt.Errorf("mysql.binlog_target_time and mysql.binlog_target_position are mutually exclusive")
	}

	if !hasTargetTime && !hasTargetPos {
		return nil
	}

	if job.Type != "mysql" && job.Type != "mariadb" {
		return fmt.Errorf("mysql replay target selectors are only valid for mysql or mariadb restore jobs")
	}

	if hasTargetPos {
		if strings.TrimSpace(job.MySQL.BinlogTargetPosition.File) == "" || job.MySQL.BinlogTargetPosition.Pos <= 0 {
			return fmt.Errorf("mysql.binlog_target_position requires both file and pos > 0")
		}
	}

	if hasTargetTime {
		if _, err := time.Parse(time.RFC3339, job.MySQL.BinlogTargetTime); err != nil {
			return fmt.Errorf("mysql.binlog_target_time must be RFC3339 with timezone: %w", err)
		}
	}

	return nil
}

func validateOplogReplaySelectors(job RestoreJob) error {
	ts := strings.TrimSpace(job.MongoDB.OplogTargetTimestamp)
	if ts == "" {
		return nil
	}
	if job.Type != "mongodb" {
		return fmt.Errorf("mongodb.oplog_target_timestamp is only valid for mongodb restore jobs")
	}
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		return fmt.Errorf("mongodb.oplog_target_timestamp must be RFC3339 with timezone: %w", err)
	}
	return nil
}

func validateStorage(job BackupJob) error {
	if job.Storage.Type == "" {
		return fmt.Errorf("storage.type is required")
	}
	if err := storage.ValidateStorageType(job.Storage.Type); err != nil {
		return err
	}
	if job.Storage.Type == "google-drive" {
		if job.Storage.GDriveFolderID == "" {
			return fmt.Errorf("gdrive_folder_id is required for google-drive storage")
		}
		if job.Storage.GDriveSAFile == "" {
			return fmt.Errorf("gdrive_sa_file is required for google-drive storage")
		}
	}
	if job.Storage.Type == "azure" {
		accountName := job.Storage.AzureStorageAccount
		container := job.Storage.AzureContainer
		if err := storage.ValidateAzureConfig(accountName, container, "", ""); err != nil {
			return err
		}
	}
	if job.Storage.Type == "gcs" {
		if err := storage.ValidateGCSConfig(job.Storage.GCSBucket); err != nil {
			return err
		}
	}
	return nil
}

func validateRetention(job BackupJob) error {
	if job.Retention.KeepLast == 0 && job.Retention.KeepDays == 0 {
		if job.Retention.DryRun {
			return fmt.Errorf("keep_last or keep_days is required when retention is enabled")
		}
		return nil
	}
	return nil
}

func validateDatabaseOptions(job BackupJob) error {
	if len(job.DatabaseOptions) == 0 {
		return nil
	}

	switch job.Type {
	case "postgres":
		return validatePostgresOptions(job.DatabaseOptions)
	case "mysql", "mariadb":
		return validateMySQLOptions(job.DatabaseOptions)
	case "mongodb":
		return validateMongoOptions(job.DatabaseOptions)
	default:
		return fmt.Errorf("unsupported database type '%s'", job.Type)
	}
}

func validatePostgresOptions(options map[string]interface{}) error {
	for key, value := range options {
		if !allowedPostgresOptions[key] {
			return fmt.Errorf("unsupported postgres option '%s'", key)
		}
		switch key {
		case "pg_out_format":
			format, ok := value.(string)
			if !ok {
				return fmt.Errorf("pg_out_format must be a string")
			}
			switch format {
			case "p", "c", "t", "d":
				// ok
			default:
				return fmt.Errorf("pg_out_format must be one of: p, c, t, d")
			}
		case "compress":
			level, ok := asInt(value)
			if !ok {
				return fmt.Errorf("compress must be an integer")
			}
			if level < 0 || level > 9 {
				return fmt.Errorf("compress must be between 0 and 9")
			}
		case "pg_compression_algo":
			algo, ok := value.(string)
			if !ok {
				return fmt.Errorf("pg_compression_algo must be a string")
			}
			switch algo {
			case "gzip", "lz4", "zstd", "none":
				// ok
			default:
				return fmt.Errorf("pg_compression_algo must be one of: gzip, lz4, zstd, none")
			}
		case "pg_compression_level":
			level, ok := asInt(value)
			if !ok {
				return fmt.Errorf("pg_compression_level must be an integer")
			}
			if level < 1 || level > 9 {
				return fmt.Errorf("pg_compression_level must be between 1 and 9")
			}
		}
	}
	return nil
}

func validateMySQLOptions(options map[string]interface{}) error {
	for key, value := range options {
		if !allowedMySQLOptions[key] {
			return fmt.Errorf("unsupported mysql option '%s'", key)
		}
		switch key {
		case "single_transaction", "routines", "triggers", "events":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be a boolean", key)
			}
		}
	}
	return nil
}

func validateMongoOptions(options map[string]interface{}) error {
	for key, value := range options {
		if !allowedMongoOptions[key] {
			return fmt.Errorf("unsupported mongodb option '%s'", key)
		}
		switch key {
		case "gzip", "oplog", "archive":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be a boolean", key)
			}
		}
	}
	return nil
}

func validateNotifications(job BackupJob) error {
	for _, channel := range job.Notifications {
		switch channel.Type {
		case "slack", "discord", "email", "webhook":
			// ok
		default:
			return fmt.Errorf("unsupported notification type '%s'", channel.Type)
		}

		if len(channel.Events) == 0 {
			return fmt.Errorf("notification events must include at least one of: success, failure, warning")
		}

		for _, event := range channel.Events {
			switch event {
			case "success", "failure", "warning":
				// ok
			default:
				return fmt.Errorf("unsupported notification event '%s'", event)
			}
		}

		switch channel.Type {
		case "slack", "discord", "webhook":
			if channel.WebhookURLEnv == "" {
				return fmt.Errorf("webhook_url_env is required for %s notifications", channel.Type)
			}
			if channel.TimeoutSeconds != 0 && (channel.TimeoutSeconds < 1 || channel.TimeoutSeconds > 60) {
				return fmt.Errorf("timeout_seconds must be between 1 and 60")
			}
		case "email":
			if channel.SMTPHost == "" {
				return fmt.Errorf("smtp_host is required for email notifications")
			}
			if channel.SMTPPasswordEnv == "" {
				return fmt.Errorf("smtp_password_env is required for email notifications")
			}
			if channel.FromAddressEnv == "" {
				return fmt.Errorf("from_address_env is required for email notifications")
			}
			if len(channel.ToAddresses) == 0 {
				return fmt.Errorf("to_addresses is required for email notifications")
			}
			if channel.SMTPPort != 0 && (channel.SMTPPort < 1 || channel.SMTPPort > 65535) {
				return fmt.Errorf("smtp_port must be between 1 and 65535")
			}
		}
	}
	return nil
}

func asInt(value interface{}) (int, bool) {
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
