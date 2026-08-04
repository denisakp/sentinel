package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	defaultLogFormat             = "json"
	defaultMaxConcurrentJobs     = 3
	defaultHistoryDBPath         = "~/.sentinel/history.db"
	defaultRestoreStagingDir     = "/tmp/sentinel"
	defaultAutoDiscoveryMode     = "individual"
	defaultTLSMode               = "prefer"
	defaultJobTimeoutMinutes     = 180
	defaultStaleLockThreshold    = 60
	defaultMaxConcurrentRestores = 1
	defaultLockDir               = "/var/run/sentinel"
)

// LoadConfig reads, parses, and normalizes a YAML configuration file.
func LoadConfig(path string) (*Configuration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Configuration
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if len(cfg.Databases) == 0 {
		return nil, fmt.Errorf("configuration must define at least one backup job")
	}

	applyDefaults(&cfg)

	if err := resolveNamedStorages(&cfg); err != nil {
		return nil, err
	}
	if err := applyEnvOverrides(&cfg); err != nil {
		return nil, err
	}
	if err := interpolateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(cfg *Configuration) {
	if cfg.MaxConcurrentBackups == 0 {
		cfg.MaxConcurrentBackups = defaultMaxConcurrentJobs
	}
	if cfg.MaxConcurrentRestores == 0 {
		cfg.MaxConcurrentRestores = defaultMaxConcurrentRestores
	}
	if cfg.LogFormat == "" {
		cfg.LogFormat = defaultLogFormat
	}
	if cfg.HistoryDBPath == "" {
		cfg.HistoryDBPath = defaultHistoryDBPath
	}
	if cfg.Restore.StagingDir == "" {
		cfg.Restore.StagingDir = defaultRestoreStagingDir
	}

	// Apply scheduler defaults
	if cfg.Scheduler.MaxConcurrentBackups == 0 {
		cfg.Scheduler.MaxConcurrentBackups = cfg.MaxConcurrentBackups
	}
	if cfg.Scheduler.MaxConcurrentRestores == 0 {
		cfg.Scheduler.MaxConcurrentRestores = defaultMaxConcurrentRestores
	}
	if cfg.Scheduler.JobTimeoutMinutes == 0 {
		cfg.Scheduler.JobTimeoutMinutes = defaultJobTimeoutMinutes
	}
	if cfg.Scheduler.StaleLockThreshold == 0 {
		cfg.Scheduler.StaleLockThreshold = defaultStaleLockThreshold
	}
	if cfg.Scheduler.LockDir == "" {
		cfg.Scheduler.LockDir = defaultLockDir
	}

	for name, job := range cfg.Databases {
		job.Name = name
		if job.Enabled == nil {
			job.Enabled = boolPtr(true)
		}
		if job.Schedule == "" && cfg.Defaults.Schedule != "" {
			job.Schedule = cfg.Defaults.Schedule
		}
		// Only apply storage defaults if no named storage reference is defined
		if job.Storage.Name == "" && job.Storage.Type == "" && cfg.Defaults.Storage.Type != "" {
			job.Storage = cfg.Defaults.Storage
		}
		if !hasRetention(job.Retention) && hasRetention(cfg.Defaults.Retention) {
			job.Retention = cfg.Defaults.Retention
		}
		if job.Compression == nil && cfg.Defaults.Compression != nil {
			inherited := *cfg.Defaults.Compression
			job.Compression = &inherited
		}
		applyCompressionDefaults(job.Compression)
		if job.VerifyAfterUpload == nil {
			job.VerifyAfterUpload = boolPtr(cfg.Integrity.VerifyAfterUpload)
		}
		if job.Notifications == nil && len(cfg.Defaults.Notifications) > 0 {
			job.Notifications = cfg.Defaults.Notifications
		}
		if job.Database == "*" && job.Strategy == "" {
			job.Strategy = defaultAutoDiscoveryMode
		}
		applyTLSDefaults(job.TLS)
		applyNotificationDefaults(job.Notifications)
		cfg.Databases[name] = job
	}

	for name, job := range cfg.Restores {
		job.Name = name
		if job.Enabled == nil {
			job.Enabled = boolPtr(false)
		}
		if job.StagingDir == "" {
			job.StagingDir = cfg.Restore.StagingDir
		}
		if !job.KeepFile && cfg.Restore.KeepFile {
			job.KeepFile = true
		}
		cfg.Restores[name] = job
	}

	applyNotificationDefaults(cfg.Defaults.Notifications)
}

// applyTLSDefaults sets the default TLS mode when a tls: section is present.
func applyTLSDefaults(tls *TLSConfig) {
	if tls == nil {
		return
	}
	if tls.Mode == "" {
		tls.Mode = defaultTLSMode
	}
}

func applyNotificationDefaults(channels []NotificationChannel) {
	for i := range channels {
		if channels[i].Enabled == nil {
			channels[i].Enabled = boolPtr(true)
		}
		if channels[i].Type == "email" && channels[i].UseTLS == nil {
			channels[i].UseTLS = boolPtr(true)
		}
		if (channels[i].Type == "slack" || channels[i].Type == "discord" || channels[i].Type == "webhook") && channels[i].TimeoutSeconds == 0 {
			channels[i].TimeoutSeconds = 10
		}
	}
}

func hasRetention(policy RetentionPolicy) bool {
	return policy.KeepLast > 0 || policy.KeepDays > 0 || policy.DryRun || gfsConfigured(policy.GFS)
}

// applyCompressionDefaults normalizes an enabled compression block in place:
// an omitted algorithm becomes zstd (the recommended default), and an omitted
// level (0) becomes the per-algorithm default. Disabled or nil blocks are left
// untouched.
func applyCompressionDefaults(c *CompressionConfig) {
	if c == nil || !c.Enabled {
		return
	}
	if c.Algorithm == "" {
		c.Algorithm = "zstd"
	}
	if c.Level == 0 {
		switch c.Algorithm {
		case "gzip":
			c.Level = 6
		case "zstd":
			c.Level = 3
		}
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func applyEnvOverrides(cfg *Configuration) error {
	for name, st := range cfg.Storages {
		if err := resolveEnvOverride(&st.S3AccessKeyID, st.S3AccessKeyIDEnv); err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&st.S3SecretAccessKey, st.S3SecretAccessKeyEnv); err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&st.AzureStorageAccount, st.AzureStorageAccountEnv); err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&st.AzureStorageKey, st.AzureStorageKeyEnv); err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		cfg.Storages[name] = st
	}

	for name, job := range cfg.Databases {
		if err := resolveEnvOverride(&job.Host, job.HostEnv); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.Username, job.UsernameEnv); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.URI, job.URIEnv); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.Storage.S3AccessKeyID, job.Storage.S3AccessKeyIDEnv); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.Storage.S3SecretAccessKey, job.Storage.S3SecretAccessKeyEnv); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.Storage.AzureStorageAccount, job.Storage.AzureStorageAccountEnv); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.Storage.AzureStorageKey, job.Storage.AzureStorageKeyEnv); err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}

		if job.DefaultsFile != "" && (job.Type == "mysql" || job.Type == "mariadb") {
			if err := applyDefaultsFile(&job); err != nil {
				return fmt.Errorf("backup '%s': %w", name, err)
			}
		}

		for i := range job.Notifications {
			channel := &job.Notifications[i]
			enabled := channel.Enabled == nil || *channel.Enabled
			if !enabled {
				continue
			}

			switch channel.Type {
			case "slack", "discord", "webhook":
				if err := requireEnvValue(channel.WebhookURLEnv); err != nil {
					return fmt.Errorf("backup '%s': %w", name, err)
				}
			case "email":
				if err := requireEnvValue(channel.FromAddressEnv); err != nil {
					return fmt.Errorf("backup '%s': %w", name, err)
				}
				if err := requireEnvValue(channel.SMTPPasswordEnv); err != nil {
					return fmt.Errorf("backup '%s': %w", name, err)
				}
			}
		}

		if job.Type != "mongodb" && job.myCnfPassword == "" {
			if err := requireEnvValue(job.PasswordEnv); err != nil {
				return fmt.Errorf("backup '%s': %w", name, err)
			}
		}

		cfg.Databases[name] = job
	}

	for name, job := range cfg.Restores {
		if err := resolveEnvOverride(&job.Host, job.HostEnv); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.Username, job.UsernameEnv); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.URI, job.URIEnv); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.BackupSource.S3AccessKeyID, job.BackupSource.S3AccessKeyIDEnv); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.BackupSource.S3SecretAccessKey, job.BackupSource.S3SecretAccessKeyEnv); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.BackupSource.AzureStorageAccount, job.BackupSource.AzureStorageAccountEnv); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		if err := resolveEnvOverride(&job.BackupSource.AzureStorageKey, job.BackupSource.AzureStorageKeyEnv); err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}

		cfg.Restores[name] = job
	}

	return nil
}

func interpolateConfig(cfg *Configuration) error {
	if cfg.HistoryDBPath != "" {
		value, err := interpolateEnvVars(cfg.HistoryDBPath)
		if err != nil {
			return err
		}
		cfg.HistoryDBPath = value
	}

	if cfg.Restore.StagingDir != "" {
		value, err := interpolateEnvVars(cfg.Restore.StagingDir)
		if err != nil {
			return err
		}
		cfg.Restore.StagingDir = value
	}

	if cfg.EncryptionKeyFile != "" {
		value, err := interpolateEnvVars(cfg.EncryptionKeyFile)
		if err != nil {
			return err
		}
		cfg.EncryptionKeyFile = value
	}

	for name, st := range cfg.Storages {
		var err error
		st.LocalPath, err = interpolateEnvVars(st.LocalPath)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.S3Bucket, err = interpolateEnvVars(st.S3Bucket)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.S3BucketEndpoint, err = interpolateEnvVars(st.S3BucketEndpoint)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.S3Region, err = interpolateEnvVars(st.S3Region)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.S3AccessKeyID, err = interpolateEnvVars(st.S3AccessKeyID)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.S3SecretAccessKey, err = interpolateEnvVars(st.S3SecretAccessKey)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.GCSBucket, err = interpolateEnvVars(st.GCSBucket)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.GCSProjectID, err = interpolateEnvVars(st.GCSProjectID)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.GCSCredentialsFile, err = interpolateEnvVars(st.GCSCredentialsFile)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.GDriveFolderID, err = interpolateEnvVars(st.GDriveFolderID)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.GDriveSAFile, err = interpolateEnvVars(st.GDriveSAFile)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.AzureStorageAccount, err = interpolateEnvVars(st.AzureStorageAccount)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.AzureStorageKey, err = interpolateEnvVars(st.AzureStorageKey)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		st.AzureContainer, err = interpolateEnvVars(st.AzureContainer)
		if err != nil {
			return fmt.Errorf("storage '%s': %w", name, err)
		}
		cfg.Storages[name] = st
	}

	for name, job := range cfg.Databases {
		var err error

		job.Host, err = interpolateEnvVars(job.Host)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Username, err = interpolateEnvVars(job.Username)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.URI, err = interpolateEnvVars(job.URI)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Database, err = interpolateEnvVars(job.Database)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Output, err = interpolateEnvVars(job.Output)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}

		job.Storage.LocalPath, err = interpolateEnvVars(job.Storage.LocalPath)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.S3Bucket, err = interpolateEnvVars(job.Storage.S3Bucket)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.S3BucketEndpoint, err = interpolateEnvVars(job.Storage.S3BucketEndpoint)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.S3Region, err = interpolateEnvVars(job.Storage.S3Region)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.S3AccessKeyID, err = interpolateEnvVars(job.Storage.S3AccessKeyID)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.S3SecretAccessKey, err = interpolateEnvVars(job.Storage.S3SecretAccessKey)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.GCSBucket, err = interpolateEnvVars(job.Storage.GCSBucket)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.GCSProjectID, err = interpolateEnvVars(job.Storage.GCSProjectID)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.GCSCredentialsFile, err = interpolateEnvVars(job.Storage.GCSCredentialsFile)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.GDriveFolderID, err = interpolateEnvVars(job.Storage.GDriveFolderID)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.GDriveSAFile, err = interpolateEnvVars(job.Storage.GDriveSAFile)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.AzureStorageAccount, err = interpolateEnvVars(job.Storage.AzureStorageAccount)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.AzureStorageKey, err = interpolateEnvVars(job.Storage.AzureStorageKey)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}
		job.Storage.AzureContainer, err = interpolateEnvVars(job.Storage.AzureContainer)
		if err != nil {
			return fmt.Errorf("backup '%s': %w", name, err)
		}

		for i := range job.Notifications {
			channel := &job.Notifications[i]
			channel.SMTPHost, err = interpolateEnvVars(channel.SMTPHost)
			if err != nil {
				return fmt.Errorf("backup '%s': %w", name, err)
			}
			for j := range channel.ToAddresses {
				channel.ToAddresses[j], err = interpolateEnvVars(channel.ToAddresses[j])
				if err != nil {
					return fmt.Errorf("backup '%s': %w", name, err)
				}
			}
		}

		cfg.Databases[name] = job
	}

	for name, job := range cfg.Restores {
		var err error

		job.Host, err = interpolateEnvVars(job.Host)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.Username, err = interpolateEnvVars(job.Username)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.URI, err = interpolateEnvVars(job.URI)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.Database, err = interpolateEnvVars(job.Database)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.Type, err = interpolateEnvVars(job.BackupSource.Type)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.LocalPath, err = interpolateEnvVars(job.BackupSource.LocalPath)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.S3Bucket, err = interpolateEnvVars(job.BackupSource.S3Bucket)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.S3BucketEndpoint, err = interpolateEnvVars(job.BackupSource.S3BucketEndpoint)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.S3Region, err = interpolateEnvVars(job.BackupSource.S3Region)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.S3AccessKeyID, err = interpolateEnvVars(job.BackupSource.S3AccessKeyID)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.S3SecretAccessKey, err = interpolateEnvVars(job.BackupSource.S3SecretAccessKey)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.GCSBucket, err = interpolateEnvVars(job.BackupSource.GCSBucket)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.GCSProjectID, err = interpolateEnvVars(job.BackupSource.GCSProjectID)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.GCSCredentialsFile, err = interpolateEnvVars(job.BackupSource.GCSCredentialsFile)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.GDriveFolderID, err = interpolateEnvVars(job.BackupSource.GDriveFolderID)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.GDriveSAFile, err = interpolateEnvVars(job.BackupSource.GDriveSAFile)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.AzureStorageAccount, err = interpolateEnvVars(job.BackupSource.AzureStorageAccount)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.AzureStorageKey, err = interpolateEnvVars(job.BackupSource.AzureStorageKey)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.AzureContainer, err = interpolateEnvVars(job.BackupSource.AzureContainer)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}
		job.BackupSource.BackupPath, err = interpolateEnvVars(job.BackupSource.BackupPath)
		if err != nil {
			return fmt.Errorf("restore '%s': %w", name, err)
		}

		cfg.Restores[name] = job
	}

	return nil
}

func resolveNamedStorages(cfg *Configuration) error {
	for name, job := range cfg.Databases {
		if job.Storage.Name == "" {
			continue
		}

		named, ok := cfg.Storages[job.Storage.Name]
		if !ok {
			return fmt.Errorf("backup '%s': unknown storage reference '%s'", name, job.Storage.Name)
		}

		resolved := named
		overlayStorage(&resolved, job.Storage)
		job.Storage = resolved
		cfg.Databases[name] = job
	}

	return nil
}

func overlayStorage(base *StorageConfig, override StorageConfig) {
	if override.Type != "" {
		base.Type = override.Type
	}
	if override.Name != "" {
		base.Name = override.Name
	}
	if override.LocalPath != "" {
		base.LocalPath = override.LocalPath
	}
	if override.S3Bucket != "" {
		base.S3Bucket = override.S3Bucket
	}
	if override.S3BucketEndpoint != "" {
		base.S3BucketEndpoint = override.S3BucketEndpoint
	}
	if override.S3Region != "" {
		base.S3Region = override.S3Region
	}
	if override.S3AccessKeyID != "" {
		base.S3AccessKeyID = override.S3AccessKeyID
	}
	if override.S3AccessKeyIDEnv != "" {
		base.S3AccessKeyIDEnv = override.S3AccessKeyIDEnv
	}
	if override.S3SecretAccessKey != "" {
		base.S3SecretAccessKey = override.S3SecretAccessKey
	}
	if override.S3SecretAccessKeyEnv != "" {
		base.S3SecretAccessKeyEnv = override.S3SecretAccessKeyEnv
	}
	if override.GCSBucket != "" {
		base.GCSBucket = override.GCSBucket
	}
	if override.GCSProjectID != "" {
		base.GCSProjectID = override.GCSProjectID
	}
	if override.GCSCredentialsFile != "" {
		base.GCSCredentialsFile = override.GCSCredentialsFile
	}
	if override.GDriveFolderID != "" {
		base.GDriveFolderID = override.GDriveFolderID
	}
	if override.GDriveSAFile != "" {
		base.GDriveSAFile = override.GDriveSAFile
	}
	if override.AzureStorageAccount != "" {
		base.AzureStorageAccount = override.AzureStorageAccount
	}
	if override.AzureStorageAccountEnv != "" {
		base.AzureStorageAccountEnv = override.AzureStorageAccountEnv
	}
	if override.AzureStorageKey != "" {
		base.AzureStorageKey = override.AzureStorageKey
	}
	if override.AzureStorageKeyEnv != "" {
		base.AzureStorageKeyEnv = override.AzureStorageKeyEnv
	}
	if override.AzureContainer != "" {
		base.AzureContainer = override.AzureContainer
	}
}
