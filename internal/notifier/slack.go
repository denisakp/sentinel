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

// SlackNotifier sends notifications to Slack via webhook
type SlackNotifier struct {
	config *WebhookNotificationConfig
	client *http.Client
}

// NewSlackNotifier creates a new Slack notifier
func NewSlackNotifier(config *WebhookNotificationConfig) *SlackNotifier {
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = 10
	}
	return &SlackNotifier{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

// SendBackup sends a backup notification to Slack
func (s *SlackNotifier) SendBackup(ctx context.Context, backup *BackupContext) error {
	if !s.config.Enabled || !ShouldNotify(s.config.Events, backup.Status) {
		return nil
	}

	msg := FormatMessage(backup)
	payload := s.buildPayload(msg)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload - %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create Slack request - %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Slack notification - %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendRestore sends a restore notification to Slack
func (s *SlackNotifier) SendRestore(ctx context.Context, restore *RestoreContext) error {
	if !s.config.Enabled || !ShouldNotify(s.config.Events, restore.Status) {
		return nil
	}

	msg := FormatRestoreMessage(restore)
	payload := s.buildPayload(msg)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload - %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create Slack request - %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Slack notification - %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Send is deprecated, use SendBackup instead
func (s *SlackNotifier) Send(ctx context.Context, backup *BackupContext) error {
	return s.SendBackup(ctx, backup)
}

// Type returns the notifier type
func (s *SlackNotifier) Type() string {
	return "slack"
}

// IsEnabled returns whether the notifier is enabled
func (s *SlackNotifier) IsEnabled() bool {
	return s.config.Enabled
}

// buildPayload builds a Slack webhook payload
func (s *SlackNotifier) buildPayload(msg *FormattedMessage) map[string]any {
	color := GetStatusColor(msg.Status)

	fields := []map[string]any{
		{
			"title": "Database",
			"value": msg.Details["Database"],
			"short": true,
		},
		{
			"title": "Type",
			"value": msg.Details["Database Type"],
			"short": true,
		},
		{
			"title": "Duration",
			"value": msg.Details["Duration"],
			"short": true,
		},
		{
			"title": "Size",
			"value": msg.Details["File Size"],
			"short": true,
		},
	}

	if msg.Details["Error"] != "" {
		fields = append(fields, map[string]any{
			"title": "Error",
			"value": msg.Details["Error"],
			"short": false,
		})
	}

	attachment := map[string]any{
		"fallback":  msg.Title,
		"color":     color.SlackHex,
		"title":     msg.Title,
		"text":      SanitizeForSlack(msg.MessageText),
		"fields":    fields,
		"ts":        msg.Timestamp.Unix(),
		"image_url": "",
	}

	return map[string]any{
		"attachments": []map[string]any{attachment},
	}
}
