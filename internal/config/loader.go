package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	defaultLogFormat         = "json"
	defaultMaxConcurrentJobs = 3
	defaultHistoryDBPath     = "~/.sentinel/history.db"
	defaultAutoDiscoveryMode = "individual"
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
	if cfg.LogFormat == "" {
		cfg.LogFormat = defaultLogFormat
	}
	if cfg.HistoryDBPath == "" {
		cfg.HistoryDBPath = defaultHistoryDBPath
	}

	for name, job := range cfg.Databases {
		job.Name = name
		if job.Enabled == nil {
			job.Enabled = boolPtr(true)
		}
		if job.Storage.Type == "" && cfg.Defaults.Storage.Type != "" {
			job.Storage = cfg.Defaults.Storage
		}
		if !hasRetention(job.Retention) && hasRetention(cfg.Defaults.Retention) {
			job.Retention = cfg.Defaults.Retention
		}
		if job.Notifications == nil && len(cfg.Defaults.Notifications) > 0 {
			job.Notifications = cfg.Defaults.Notifications
		}
		if job.Database == "*" && job.Strategy == "" {
			job.Strategy = defaultAutoDiscoveryMode
		}
		applyNotificationDefaults(job.Notifications)
		cfg.Databases[name] = job
	}

	applyNotificationDefaults(cfg.Defaults.Notifications)
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
	return policy.KeepLast > 0 || policy.KeepDays > 0 || policy.DryRun
}

func boolPtr(v bool) *bool {
	return &v
}

func applyEnvOverrides(cfg *Configuration) error {
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

		if job.Type != "mongodb" {
			if err := requireEnvValue(job.PasswordEnv); err != nil {
				return fmt.Errorf("backup '%s': %w", name, err)
			}
		}

		cfg.Databases[name] = job
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

	return nil
}
