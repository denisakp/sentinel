package notifier

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// NewDispatcherFromConfig creates a dispatcher and adds notifiers from config.
func NewDispatcherFromConfig(notifications []config.NotificationChannel) (*Dispatcher, error) {
	dispatcher := NewDispatcher(nil)

	for i, notifCfg := range notifications {
		enabled := true
		if notifCfg.Enabled != nil {
			enabled = *notifCfg.Enabled
		}

		switch notifCfg.Type {
		case "slack", "discord", "webhook":
			webhookURL, err := resolveEnvVar(notifCfg.WebhookURLEnv)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve webhook_url_env for channel %d - %w", i, err)
			}
			webhookConfig := &ports.WebhookNotificationConfig{
				Type:           notifCfg.Type,
				WebhookURLEnv:  notifCfg.WebhookURLEnv,
				WebhookURL:     webhookURL,
				Events:         notifCfg.Events,
				TimeoutSeconds: notifCfg.TimeoutSeconds,
				Enabled:        enabled,
			}
			if err := dispatcher.AddWebhookNotifier(webhookConfig); err != nil {
				return nil, fmt.Errorf("failed to add webhook notifier for channel %d - %w", i, err)
			}
		case "email":
			smtpPassword, err := resolveEnvVar(notifCfg.SMTPPasswordEnv)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve smtp_password_env for channel %d - %w", i, err)
			}
			fromAddress, err := resolveEnvVar(notifCfg.FromAddressEnv)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve from_address_env for channel %d - %w", i, err)
			}

			smtpUsername := ""
			if notifCfg.SMTPUsernameEnv != "" {
				smtpUsername, err = resolveEnvVar(notifCfg.SMTPUsernameEnv)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve smtp_username_env for channel %d - %w", i, err)
				}
			}

			smtpPort := notifCfg.SMTPPort
			if smtpPort == 0 {
				smtpPort = 587
			}

			useTLS := true
			if notifCfg.UseTLS != nil {
				useTLS = *notifCfg.UseTLS
			}

			emailConfig := &ports.EmailNotificationConfig{
				Type:            "email",
				SMTPHost:        notifCfg.SMTPHost,
				SMTPPort:        smtpPort,
				SMTPUsername:    smtpUsername,
				SMTPUsernameEnv: notifCfg.SMTPUsernameEnv,
				SMTPPassword:    smtpPassword,
				SMTPPasswordEnv: notifCfg.SMTPPasswordEnv,
				FromAddress:     fromAddress,
				FromAddressEnv:  notifCfg.FromAddressEnv,
				ToAddresses:     notifCfg.ToAddresses,
				UseTLS:          useTLS,
				Events:          notifCfg.Events,
				Enabled:         enabled,
			}
			if err := dispatcher.AddEmailNotifier(emailConfig); err != nil {
				return nil, fmt.Errorf("failed to add email notifier for channel %d - %w", i, err)
			}
		default:
			return nil, fmt.Errorf("unsupported notification type: %s", notifCfg.Type)
		}
	}

	return dispatcher, nil
}

// NewDispatcherFromRestoreConfig creates a dispatcher for restore notifications.
// It uses the same notification channels as backups.
func NewDispatcherFromRestoreConfig(notifications []config.NotificationChannel) (*Dispatcher, error) {
	return NewDispatcherFromConfig(notifications)
}

// NotificationStatusFromRestoreStatus maps restore execution status values to
// notifier event categories used by channel event filters.
func NotificationStatusFromRestoreStatus(status string) ports.BackupStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "completed":
		return ports.NotifyStatusSuccess
	case "skipped":
		return ports.NotifyStatusWarning
	case "failed", "failure", "timeout", "interrupted":
		return ports.NotifyStatusFailure
	default:
		return ports.NotifyStatusWarning
	}
}

// resolveEnvVar resolves an environment variable name to its value.
// Validates that the env var name follows the pattern: ^[A-Z_][A-Z0-9_]*$
func resolveEnvVar(envVarName string) (string, error) {
	if envVarName == "" {
		return "", nil
	}

	if !isValidEnvVarName(envVarName) {
		return "", fmt.Errorf("invalid environment variable name: %s (must match ^[A-Z_][A-Z0-9_]*$)", envVarName)
	}

	value, exists := os.LookupEnv(envVarName)
	if !exists {
		return "", fmt.Errorf("environment variable not found: %s", envVarName)
	}

	return value, nil
}

// isValidEnvVarName validates that an environment variable name follows Go conventions.
func isValidEnvVarName(name string) bool {
	if name == "" {
		return false
	}
	pattern := regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	return pattern.MatchString(name)
}
