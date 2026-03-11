package notifier

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

// EmailNotifier sends notifications via SMTP email
type EmailNotifier struct {
	config *EmailNotificationConfig
}

// NewEmailNotifier creates a new email notifier
func NewEmailNotifier(config *EmailNotificationConfig) *EmailNotifier {
	if config.SMTPPort == 0 {
		config.SMTPPort = 587
	}
	return &EmailNotifier{
		config: config,
	}
}

// SendBackup sends a backup notification via email
func (e *EmailNotifier) SendBackup(ctx context.Context, backup *BackupContext) error {
	if !e.config.Enabled || !ShouldNotify(e.config.Events, backup.Status) {
		return nil
	}

	msg := FormatMessage(backup)
	emailBody := e.buildEmailBodyFromBackup(backup, msg)

	// Build email headers
	to := e.config.ToAddresses
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n",
		e.config.FromAddress,
		strings.Join(to, ", "),
		msg.Title,
		time.Now().Format(time.RFC1123Z),
	)

	fullMessage := headers + emailBody
	return e.sendEmailMessage(fullMessage)
}

// SendRestore sends a restore notification via email
func (e *EmailNotifier) SendRestore(ctx context.Context, restore *RestoreContext) error {
	if !e.config.Enabled || !ShouldNotify(e.config.Events, restore.Status) {
		return nil
	}

	msg := FormatRestoreMessage(restore)
	emailBody := e.buildEmailBodyFromRestore(restore, msg)

	// Build email headers
	to := e.config.ToAddresses
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n",
		e.config.FromAddress,
		strings.Join(to, ", "),
		msg.Title,
		time.Now().Format(time.RFC1123Z),
	)

	fullMessage := headers + emailBody
	return e.sendEmailMessage(fullMessage)
}

// Send is deprecated, use SendBackup instead
func (e *EmailNotifier) Send(ctx context.Context, backup *BackupContext) error {
	return e.SendBackup(ctx, backup)
}

// sendEmailMessage sends an email message via SMTP
func (e *EmailNotifier) sendEmailMessage(fullMessage string) error {
	// Connect to SMTP server
	addr := fmt.Sprintf("%s:%d", e.config.SMTPHost, e.config.SMTPPort)

	var auth smtp.Auth
	if e.config.SMTPUsername != "" {
		auth = smtp.PlainAuth("", e.config.SMTPUsername, e.config.SMTPPassword, e.config.SMTPHost)
	}

	// Establish connection
	var conn *smtp.Client
	var err error

	if e.config.UseTLS {
		// Use StartTLS (port 587 typical)
		conn, err = smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("failed to connect to SMTP server %s - %w", addr, err)
		}
		defer conn.Close()

		// Upgrade connection to TLS
		tlsConfig := &tls.Config{ServerName: e.config.SMTPHost}
		if err = conn.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to upgrade SMTP connection to TLS - %w", err)
		}

		// Authenticate
		if auth != nil {
			if err = conn.Auth(auth); err != nil {
				return fmt.Errorf("failed to authenticate with SMTP server - %w", err)
			}
		}
	} else {
		// Direct SSL/TLS connection (port 465 typical)
		conn, err = smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("failed to connect to SMTP server %s - %w", addr, err)
		}
		defer conn.Close()

		if auth != nil {
			if err = conn.Auth(auth); err != nil {
				return fmt.Errorf("failed to authenticate with SMTP server - %w", err)
			}
		}
	}

	// Send email
	to := e.config.ToAddresses
	if err = conn.Mail(e.config.FromAddress); err != nil {
		return fmt.Errorf("failed to set sender address - %w", err)
	}

	for _, recipient := range to {
		if err = conn.Rcpt(recipient); err != nil {
			return fmt.Errorf("failed to set recipient %s - %w", recipient, err)
		}
	}

	wc, err := conn.Data()
	if err != nil {
		return fmt.Errorf("failed to send data - %w", err)
	}
	defer wc.Close()

	if _, err = wc.Write([]byte(fullMessage)); err != nil {
		return fmt.Errorf("failed to write email body - %w", err)
	}

	if err = conn.Quit(); err != nil {
		return fmt.Errorf("failed to close SMTP connection - %w", err)
	}

	return nil
}

// Type returns the notifier type
func (e *EmailNotifier) Type() string {
	return "email"
}

// IsEnabled returns whether the notifier is enabled
func (e *EmailNotifier) IsEnabled() bool {
	return e.config.Enabled
}

// buildEmailBodyFromBackup builds the email body text for backups
func (e *EmailNotifier) buildEmailBodyFromBackup(backup *BackupContext, msg *FormattedMessage) string {
	body := fmt.Sprintf(`Backup Execution Report

Status: %s %s
Timestamp: %s

--- Details ---
Database: %s
Database Type: %s
Backup Name: %s
Duration: %s
File Size: %s
File Path: %s
`,
		StatusEmoji(backup.Status),
		backup.Status,
		msg.Timestamp.Format(time.RFC3339),
		backup.DatabaseName,
		backup.DatabaseType,
		backup.BackupName,
		msg.Details["Duration"],
		msg.Details["File Size"],
		backup.FilePath,
	)

	if backup.Error != "" {
		body += fmt.Sprintf("\n--- Error ---\n%s\n", backup.Error)
	}

	body += "\n---\nSent by Sentinel Backup Tool"

	return body
}

// buildEmailBodyFromRestore builds the email body text for restores
func (e *EmailNotifier) buildEmailBodyFromRestore(restore *RestoreContext, msg *FormattedMessage) string {
	body := fmt.Sprintf(`Restore Execution Report

Status: %s %s
Timestamp: %s

--- Details ---
Database: %s
Database Type: %s
Restore Name: %s
Duration: %s
Bytes Restored: %s
Source Backup: %s
Verification: %v
`,
		StatusEmoji(restore.Status),
		restore.Status,
		msg.Timestamp.Format(time.RFC3339),
		restore.DatabaseName,
		restore.DatabaseType,
		restore.RestoreName,
		msg.Details["Duration"],
		msg.Details["Bytes Restored"],
		restore.SourceBackupPath,
		restore.VerificationPassed,
	)

	if restore.Error != "" {
		body += fmt.Sprintf("\n--- Error ---\n%s\n", restore.Error)
	}

	body += "\n---\nSent by Sentinel Restore Tool"

	return body
}
