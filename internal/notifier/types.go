package notifier

import (
	"context"
	"time"
)

// BackupStatus represents the status of a backup execution.
type BackupStatus string

const (
	StatusSuccess BackupStatus = "success"
	StatusFailure BackupStatus = "failure"
	StatusWarning BackupStatus = "warning"
)

// BackupContext contains information about a backup execution.
type BackupContext struct {
	BackupName   string
	DatabaseType string
	DatabaseName string
	Status       BackupStatus
	StartTime    time.Time
	EndTime      time.Time
	Error        string
	FilePath     string
	FileSize     int64
}

// Duration returns the time taken for backup.
func (bc *BackupContext) Duration() time.Duration {
	if bc.StartTime.IsZero() || bc.EndTime.IsZero() {
		return 0
	}
	return bc.EndTime.Sub(bc.StartTime)
}

// RestoreContext contains information about a restore execution.
type RestoreContext struct {
	RestoreName        string
	DatabaseType       string
	DatabaseName       string
	Status             BackupStatus
	StartTime          time.Time
	EndTime            time.Time
	Error              string
	BytesRestored      int64
	SourceBackupPath   string
	VerificationPassed bool
}

// Duration returns the time taken for restore.
func (rc *RestoreContext) Duration() time.Duration {
	if rc.StartTime.IsZero() || rc.EndTime.IsZero() {
		return 0
	}
	return rc.EndTime.Sub(rc.StartTime)
}

// NotificationContext is a union type for backup and restore contexts.
type NotificationContext interface {
	// GetStatus returns the status of the operation.
	GetStatus() BackupStatus
	// GetStartTime returns the operation start time.
	GetStartTime() time.Time
	// GetEndTime returns the operation end time.
	GetEndTime() time.Time
	// GetError returns the error message if any.
	GetError() string
	// IsRestore returns true if this is a restore context.
	IsRestore() bool
}

// GetStatus implements NotificationContext for BackupContext.
func (bc *BackupContext) GetStatus() BackupStatus {
	return bc.Status
}

func (bc *BackupContext) GetStartTime() time.Time {
	return bc.StartTime
}

func (bc *BackupContext) GetEndTime() time.Time {
	return bc.EndTime
}

func (bc *BackupContext) GetError() string {
	return bc.Error
}

func (bc *BackupContext) IsRestore() bool {
	return false
}

// GetStatus implements NotificationContext for RestoreContext.
func (rc *RestoreContext) GetStatus() BackupStatus {
	return rc.Status
}

func (rc *RestoreContext) GetStartTime() time.Time {
	return rc.StartTime
}

func (rc *RestoreContext) GetEndTime() time.Time {
	return rc.EndTime
}

func (rc *RestoreContext) GetError() string {
	return rc.Error
}

func (rc *RestoreContext) IsRestore() bool {
	return true
}

// Notifier interface defines the contract for notification channels.
type Notifier interface {
	// Send sends a notification for the given backup or restore context.
	SendBackup(ctx context.Context, backup *BackupContext) error
	// SendRestore sends a restore notification.
	SendRestore(ctx context.Context, restore *RestoreContext) error
	// Type returns the notifier type name (slack, discord, webhook, email).
	Type() string
	// IsEnabled returns whether the notifier is enabled.
	IsEnabled() bool
}

// WebhookNotificationConfig represents webhook-based notifications (Slack, Discord, Generic).
type WebhookNotificationConfig struct {
	Type           string   `yaml:"type"` // "slack", "discord", or "webhook"
	WebhookURLEnv  string   `yaml:"webhook_url_env"`
	WebhookURL     string   `yaml:"-"` // Populated from env var
	Events         []string `yaml:"events"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
	Enabled        bool     `yaml:"enabled"`
}

// EmailNotificationConfig represents email-based notifications.
type EmailNotificationConfig struct {
	Type            string   `yaml:"type"` // "email"
	SMTPHost        string   `yaml:"smtp_host"`
	SMTPPort        int      `yaml:"smtp_port"`
	SMTPUsernameEnv string   `yaml:"smtp_username_env"`
	SMTPUsername    string   `yaml:"-"` // Populated from env var
	SMTPPasswordEnv string   `yaml:"smtp_password_env"`
	SMTPPassword    string   `yaml:"-"` // Populated from env var
	FromAddressEnv  string   `yaml:"from_address_env"`
	FromAddress     string   `yaml:"-"` // Populated from env var
	ToAddresses     []string `yaml:"to_addresses"`
	UseTLS          bool     `yaml:"use_tls"`
	Events          []string `yaml:"events"`
	Enabled         bool     `yaml:"enabled"`
}

// ShouldNotify checks if notification should be sent for given status.
func ShouldNotify(events []string, status BackupStatus) bool {
	for _, event := range events {
		if event == string(status) {
			return true
		}
	}
	return false
}
