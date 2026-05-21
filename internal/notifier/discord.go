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

// DiscordNotifier sends notifications to Discord via webhook
type DiscordNotifier struct {
	config *WebhookNotificationConfig
	client *http.Client
}

// NewDiscordNotifier creates a new Discord notifier
func NewDiscordNotifier(config *WebhookNotificationConfig) *DiscordNotifier {
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = 10
	}
	return &DiscordNotifier{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

// SendBackup sends a backup notification to Discord
func (d *DiscordNotifier) SendBackup(ctx context.Context, backup *BackupContext) error {
	if !d.config.Enabled || !ShouldNotify(d.config.Events, backup.Status) {
		return nil
	}

	msg := FormatMessage(backup)
	payload := d.buildPayload(msg)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord payload - %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create Discord request - %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Discord notification - %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			return fmt.Errorf("discord webhook returned status %d (Retry-After: %s): %s: %w",
				resp.StatusCode, ra, string(respBody), ErrNon2xxResponse)
		}
		return fmt.Errorf("discord webhook returned status %d: %s: %w",
			resp.StatusCode, string(respBody), ErrNon2xxResponse)
	}

	return nil
}

// SendRestore sends a restore notification to Discord
func (d *DiscordNotifier) SendRestore(ctx context.Context, restore *RestoreContext) error {
	if !d.config.Enabled || !ShouldNotify(d.config.Events, restore.Status) {
		return nil
	}

	msg := FormatRestoreMessage(restore)
	payload := d.buildPayload(msg)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord payload - %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create Discord request - %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Discord notification - %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			return fmt.Errorf("discord webhook returned status %d (Retry-After: %s): %s: %w",
				resp.StatusCode, ra, string(respBody), ErrNon2xxResponse)
		}
		return fmt.Errorf("discord webhook returned status %d: %s: %w",
			resp.StatusCode, string(respBody), ErrNon2xxResponse)
	}

	return nil
}

// Send is deprecated, use SendBackup instead
func (d *DiscordNotifier) Send(ctx context.Context, backup *BackupContext) error {
	return d.SendBackup(ctx, backup)
}

// Type returns the notifier type
func (d *DiscordNotifier) Type() string {
	return "discord"
}

// IsEnabled returns whether the notifier is enabled
func (d *DiscordNotifier) IsEnabled() bool {
	return d.config.Enabled
}

// buildPayload builds a Discord webhook payload (embeds format)
func (d *DiscordNotifier) buildPayload(msg *FormattedMessage) map[string]any {
	color := GetStatusColor(msg.Status)

	fields := []map[string]any{
		{
			"name":   "Database",
			"value":  msg.Details["Database"],
			"inline": true,
		},
		{
			"name":   "Type",
			"value":  msg.Details["Database Type"],
			"inline": true,
		},
		{
			"name":   "Duration",
			"value":  msg.Details["Duration"],
			"inline": true,
		},
		{
			"name":   "Size",
			"value":  msg.Details["File Size"],
			"inline": true,
		},
		{
			"name":   "Status",
			"value":  fmt.Sprintf("%s %s", StatusEmoji(msg.Status), msg.Details["Status"]),
			"inline": true,
		},
	}

	if msg.Details["Error"] != "" {
		fields = append(fields, map[string]any{
			"name":   "Error",
			"value":  msg.Details["Error"],
			"inline": false,
		})
	}

	embed := map[string]any{
		"title":       msg.Title,
		"description": msg.MessageText,
		"fields":      fields,
		"color":       color.DiscordInt,
		"timestamp":   msg.Timestamp.Format(time.RFC3339),
	}

	return map[string]any{
		"embeds": []map[string]any{embed},
	}
}
