package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookNotifier sends notifications to a generic webhook endpoint
type WebhookNotifier struct {
	config *WebhookNotificationConfig
	client *http.Client
}

// NewWebhookNotifier creates a new generic webhook notifier
func NewWebhookNotifier(config *WebhookNotificationConfig) *WebhookNotifier {
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = 10
	}
	return &WebhookNotifier{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

// SendBackup sends a backup notification to the webhook endpoint
func (w *WebhookNotifier) SendBackup(ctx context.Context, backup *BackupContext) error {
	if !w.config.Enabled || !ShouldNotify(w.config.Events, backup.Status) {
		return nil
	}

	msg := FormatMessage(backup)
	payload := w.buildBackupPayload(backup, msg)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload - %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request - %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "sentinel-backup/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook notification - %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendRestore sends a restore notification to the webhook endpoint
func (w *WebhookNotifier) SendRestore(ctx context.Context, restore *RestoreContext) error {
	if !w.config.Enabled || !ShouldNotify(w.config.Events, restore.Status) {
		return nil
	}

	msg := FormatRestoreMessage(restore)
	payload := w.buildRestorePayload(restore, msg)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload - %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request - %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "sentinel-restore/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook notification - %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Send is deprecated, use SendBackup instead
func (w *WebhookNotifier) Send(ctx context.Context, backup *BackupContext) error {
	return w.SendBackup(ctx, backup)
}

// Type returns the notifier type
func (w *WebhookNotifier) Type() string {
	return "webhook"
}

// IsEnabled returns whether the notifier is enabled
func (w *WebhookNotifier) IsEnabled() bool {
	return w.config.Enabled
}

// buildBackupPayload builds a generic webhook payload for backups
func (w *WebhookNotifier) buildBackupPayload(backup *BackupContext, msg *FormattedMessage) map[string]interface{} {
	return map[string]interface{}{
		"event":         string(backup.Status),
		"timestamp":     msg.Timestamp.Format(time.RFC3339),
		"backup_name":   backup.BackupName,
		"database":      backup.DatabaseName,
		"database_type": backup.DatabaseType,
		"status":        string(backup.Status),
		"duration_ms":   backup.Duration().Milliseconds(),
		"file_size":     backup.FileSize,
		"file_path":     backup.FilePath,
		"error":         backup.Error,
		"message":       msg.MessageText,
	}
}

// buildRestorePayload builds a generic webhook payload for restores
func (w *WebhookNotifier) buildRestorePayload(restore *RestoreContext, msg *FormattedMessage) map[string]interface{} {
	return map[string]interface{}{
		"event":               string(restore.Status),
		"timestamp":           msg.Timestamp.Format(time.RFC3339),
		"restore_name":        restore.RestoreName,
		"database":            restore.DatabaseName,
		"database_type":       restore.DatabaseType,
		"status":              string(restore.Status),
		"duration_ms":         restore.Duration().Milliseconds(),
		"bytes_restored":      restore.BytesRestored,
		"source_backup":       restore.SourceBackupPath,
		"verification_passed": restore.VerificationPassed,
		"error":               restore.Error,
		"message":             msg.MessageText,
	}
}
