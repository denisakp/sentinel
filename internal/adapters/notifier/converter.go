package notifier

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// NewDispatcherFromConfig creates a dispatcher and adds notifiers from config.
// NewDispatcherFromConfig builds a dispatcher from the configured channels.
//
// Channels are built INDEPENDENTLY. A channel whose secret cannot be resolved,
// or whose type is unsupported, is skipped and its error collected; every other
// channel still notifies. The returned dispatcher is always non-nil and holds
// whatever resolved, and the error, when non-nil, describes only what did not.
//
// This used to abort on the first failure and return no dispatcher at all, so
// one unresolvable secret silenced every channel on the job (#187). A typical
// setup has Slack for immediate notice and email as the durable record; rotating
// the Slack webhook, or deploying where that one variable is unset, took email
// down with it. Backups then failed with nobody told, and the only signal was an
// absence of messages, which looks exactly like everything working. That is the
// failure alerting exists to prevent, applied to alerting itself.
//
// Callers MUST use the returned dispatcher even when the error is non-nil, and
// surface the error as a warning. Discarding it on error reinstates the bug.
func NewDispatcherFromConfig(notifications []config.NotificationChannel) (*Dispatcher, error) {
	dispatcher := NewDispatcher(nil)
	var channelErrs []error

	for i, notifCfg := range notifications {
		enabled := true
		if notifCfg.Enabled != nil {
			enabled = *notifCfg.Enabled
		}

		switch notifCfg.Type {
		case "slack", "discord", "webhook":
			webhookURL, err := resolveEnvVar(notifCfg.WebhookURLEnv)
			if err != nil {
				channelErrs = append(channelErrs,
					fmt.Errorf("channel %d (%s) skipped: failed to resolve webhook_url_env - %w", i, notifCfg.Type, err))
				continue
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
				channelErrs = append(channelErrs,
					fmt.Errorf("channel %d (%s) skipped: %w", i, notifCfg.Type, err))
				continue
			}
		case "email":
			smtpPassword, err := resolveEnvVar(notifCfg.SMTPPasswordEnv)
			if err != nil {
				channelErrs = append(channelErrs,
					fmt.Errorf("channel %d (email) skipped: failed to resolve smtp_password_env - %w", i, err))
				continue
			}
			fromAddress, err := resolveEnvVar(notifCfg.FromAddressEnv)
			if err != nil {
				channelErrs = append(channelErrs,
					fmt.Errorf("channel %d (email) skipped: failed to resolve from_address_env - %w", i, err))
				continue
			}

			smtpUsername := ""
			if notifCfg.SMTPUsernameEnv != "" {
				smtpUsername, err = resolveEnvVar(notifCfg.SMTPUsernameEnv)
				if err != nil {
					channelErrs = append(channelErrs,
						fmt.Errorf("channel %d (email) skipped: failed to resolve smtp_username_env - %w", i, err))
					continue
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
				channelErrs = append(channelErrs,
					fmt.Errorf("channel %d (email) skipped: %w", i, err))
				continue
			}
		default:
			channelErrs = append(channelErrs,
				fmt.Errorf("channel %d skipped: unsupported notification type %q", i, notifCfg.Type))
			continue
		}
	}

	if len(channelErrs) > 0 {
		// Say how much alerting survived. "One channel failed" reads very
		// differently from "every channel failed", and an operator needs to know
		// which of the two they have.
		summary := fmt.Errorf("%d of %d notification channel(s) unavailable, %d still active",
			len(channelErrs), len(notifications), dispatcher.Len())
		return dispatcher, errors.Join(append([]error{summary}, channelErrs...)...)
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
