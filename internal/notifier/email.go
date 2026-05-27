package notifier

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

// EmailNotifier sends notifications via SMTP email
type EmailNotifier struct {
	config *ports.EmailNotificationConfig
	send   func(addr, from string, to []string, msg []byte, useTLS bool, host, user, pass string) error
}

// NewEmailNotifier creates a new email notifier
func NewEmailNotifier(config *ports.EmailNotificationConfig) *EmailNotifier {
	if config.SMTPPort == 0 {
		config.SMTPPort = 587
	}
	e := &EmailNotifier{config: config}
	e.send = e.smtpDialSend
	return e
}

// SendBackup sends a backup notification via email
func (e *EmailNotifier) SendBackup(ctx context.Context, backup *ports.BackupContext) error {
	if !e.config.Enabled || !ShouldNotify(e.config.Events, backup.Status) {
		return nil
	}

	msg := FormatMessage(backup)
	emailBody := e.buildEmailBodyFromBackup(backup, msg)

	to := e.config.ToAddresses
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n",
		e.config.FromAddress,
		strings.Join(to, ", "),
		msg.Title,
		time.Now().Format(time.RFC1123Z),
	)

	fullMessage := []byte(headers + emailBody)
	addr := fmt.Sprintf("%s:%d", e.config.SMTPHost, e.config.SMTPPort)
	return e.send(addr, e.config.FromAddress, to, fullMessage, e.config.UseTLS, e.config.SMTPHost, e.config.SMTPUsername, e.config.SMTPPassword)
}

// SendRestore sends a restore notification via email
func (e *EmailNotifier) SendRestore(ctx context.Context, restore *ports.RestoreContext) error {
	if !e.config.Enabled || !ShouldNotify(e.config.Events, restore.Status) {
		return nil
	}

	msg := FormatRestoreMessage(restore)
	emailBody := e.buildEmailBodyFromRestore(restore, msg)

	to := e.config.ToAddresses
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n",
		e.config.FromAddress,
		strings.Join(to, ", "),
		msg.Title,
		time.Now().Format(time.RFC1123Z),
	)

	fullMessage := []byte(headers + emailBody)
	addr := fmt.Sprintf("%s:%d", e.config.SMTPHost, e.config.SMTPPort)
	return e.send(addr, e.config.FromAddress, to, fullMessage, e.config.UseTLS, e.config.SMTPHost, e.config.SMTPUsername, e.config.SMTPPassword)
}

// Send is deprecated, use SendBackup instead
func (e *EmailNotifier) Send(ctx context.Context, backup *ports.BackupContext) error {
	return e.SendBackup(ctx, backup)
}

// smtpDialSend sends a fully-formed message via SMTP. Default implementation of EmailNotifier.send.
func (e *EmailNotifier) smtpDialSend(addr, from string, to []string, msg []byte, useTLS bool, host, user, pass string) error {
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}

	conn, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server %s - %w", addr, err)
	}
	defer conn.Close()

	if useTLS {
		tlsConfig := &tls.Config{ServerName: host}
		if err = conn.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to upgrade SMTP connection to TLS - %w", err)
		}
	}

	if auth != nil {
		if err = conn.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate with SMTP server - %w", err)
		}
	}

	if err = conn.Mail(from); err != nil {
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

	if _, err = wc.Write(msg); err != nil {
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
func (e *EmailNotifier) buildEmailBodyFromBackup(backup *ports.BackupContext, msg *FormattedMessage) string {
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
func (e *EmailNotifier) buildEmailBodyFromRestore(restore *ports.RestoreContext, msg *FormattedMessage) string {
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
