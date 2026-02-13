package notifier_test

import (
	"context"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/notifier"
)

func TestEmailNotifier_TypeAndEnabled(t *testing.T) {
	config := &notifier.EmailNotificationConfig{
		SMTPHost:    "smtp.example.com",
		SMTPPort:    587,
		FromAddress: "sender@example.com",
		ToAddresses: []string{"receiver@example.com"},
		Events:      []string{"success"},
		Enabled:     true,
	}

	n := notifier.NewEmailNotifier(config)
	if n.Type() != "email" {
		t.Fatalf("expected type 'email', got '%s'", n.Type())
	}
	if !n.IsEnabled() {
		t.Fatal("expected IsEnabled() to return true")
	}
}

func TestEmailNotifier_Send_Disabled(t *testing.T) {
	config := &notifier.EmailNotificationConfig{
		SMTPHost:    "smtp.example.com",
		SMTPPort:    587,
		FromAddress: "sender@example.com",
		ToAddresses: []string{"receiver@example.com"},
		Events:      []string{"success"},
		Enabled:     false,
	}

	n := notifier.NewEmailNotifier(config)
	backup := &notifier.BackupContext{
		BackupName:   "nightly",
		DatabaseType: "postgres",
		DatabaseName: "db",
		Status:       notifier.StatusSuccess,
		StartTime:    time.Now().Add(-2 * time.Minute),
		EndTime:      time.Now(),
		FilePath:     "/backups/db.sql",
		FileSize:     1024,
	}

	err := n.Send(context.Background(), backup)
	if err != nil {
		t.Fatalf("expected no error when disabled, got %v", err)
	}
}

func TestEmailNotifier_Send_IgnoresUnsubscribedEvents(t *testing.T) {
	config := &notifier.EmailNotificationConfig{
		SMTPHost:    "smtp.example.com",
		SMTPPort:    587,
		FromAddress: "sender@example.com",
		ToAddresses: []string{"receiver@example.com"},
		Events:      []string{"failure"}, // only subscribe to failures
		Enabled:     true,
	}

	n := notifier.NewEmailNotifier(config)
	backup := &notifier.BackupContext{
		BackupName:   "nightly",
		DatabaseType: "postgres",
		DatabaseName: "db",
		Status:       notifier.StatusSuccess, // send success when only failures subscribed
		StartTime:    time.Now().Add(-2 * time.Minute),
		EndTime:      time.Now(),
		FilePath:     "/backups/db.sql",
		FileSize:     1024,
	}

	err := n.Send(context.Background(), backup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
