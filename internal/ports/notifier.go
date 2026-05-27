package ports

import (
	"context"
	"errors"
	"time"
)

// Dispatcher abstracts *internal/notifier.Dispatcher (current concrete implementation).
//
// Implementations fan out backup/restore notifications across configured
// per-channel Notifier instances (slack, discord, email, webhook). Sync
// methods block until every channel responds (or times out); Async variants
// return a channel that the caller drains.
type Dispatcher interface {
	Notify(backup *BackupContext) error
	NotifyAsync(backup *BackupContext) <-chan error
	NotifyRestore(restore *RestoreContext) error
	NotifyRestoreAsync(restore *RestoreContext) <-chan error
	AddWebhookNotifier(config *WebhookNotificationConfig) error
	AddEmailNotifier(config *EmailNotificationConfig) error
	Clear()
	Count() int
	SetTimeout(timeout time.Duration)
}

// Notifier abstracts the per-channel notifier contract (slack, discord,
// email, webhook). Current concrete implementations live in
// internal/notifier/{slack,discord,email,webhook}.go.
type Notifier interface {
	SendBackup(ctx context.Context, backup *BackupContext) error
	SendRestore(ctx context.Context, restore *RestoreContext) error
	Type() string
	IsEnabled() bool
}

// NotificationContext is the unifying interface implemented by both
// *BackupContext and *RestoreContext. Channel implementations switch on
// IsRestore to render the right payload.
type NotificationContext interface {
	GetStatus() BackupStatus
	GetStartTime() time.Time
	GetEndTime() time.Time
	GetError() string
	IsRestore() bool
}

// BackupStatus represents the status of a backup or restore execution.
//
// Relocated from internal/notifier/types.go (single source of truth per
// spec 028 FR-003a).
type BackupStatus string

const (
	NotifyStatusSuccess BackupStatus = "success"
	NotifyStatusFailure BackupStatus = "failure"
	NotifyStatusWarning BackupStatus = "warning"
)

// BackupContext contains information about a backup execution.
//
// Relocated from internal/notifier/types.go.
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

// GetStatus implements NotificationContext for BackupContext.
func (bc *BackupContext) GetStatus() BackupStatus { return bc.Status }

// GetStartTime implements NotificationContext.
func (bc *BackupContext) GetStartTime() time.Time { return bc.StartTime }

// GetEndTime implements NotificationContext.
func (bc *BackupContext) GetEndTime() time.Time { return bc.EndTime }

// GetError implements NotificationContext.
func (bc *BackupContext) GetError() string { return bc.Error }

// IsRestore implements NotificationContext.
func (bc *BackupContext) IsRestore() bool { return false }

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

// GetStatus implements NotificationContext for RestoreContext.
func (rc *RestoreContext) GetStatus() BackupStatus { return rc.Status }

// GetStartTime implements NotificationContext.
func (rc *RestoreContext) GetStartTime() time.Time { return rc.StartTime }

// GetEndTime implements NotificationContext.
func (rc *RestoreContext) GetEndTime() time.Time { return rc.EndTime }

// GetError implements NotificationContext.
func (rc *RestoreContext) GetError() string { return rc.Error }

// IsRestore implements NotificationContext.
func (rc *RestoreContext) IsRestore() bool { return true }

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

// ErrNon2xxResponse is returned when a webhook target replies with a status
// outside the 2xx range. Relocated from internal/notifier/errors.go.
var ErrNon2xxResponse = errors.New("non-2xx response from notifier remote")
