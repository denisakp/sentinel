package notifier_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/notifier"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestSlackNotifier_Send_Success(t *testing.T) {
	// Create a test HTTP server to receive the webhook
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected Content-Type: application/json, got %s", ct)
		}

		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Fatal("expected non-empty request body")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	config := &ports.WebhookNotificationConfig{
		Type:           "slack",
		WebhookURL:     server.URL,
		Events:         []string{"success", "failure"},
		TimeoutSeconds: 5,
		Enabled:        true,
	}

	slack := notifier.NewSlackNotifier(config)

	backup := &ports.BackupContext{
		BackupName:   "test-backup",
		DatabaseType: "postgres",
		DatabaseName: "mydb",
		Status:       ports.NotifyStatusSuccess,
		StartTime:    time.Now().Add(-5 * time.Minute),
		EndTime:      time.Now(),
		FileSize:     1024 * 1024 * 100, // 100MB
		FilePath:     "/backups/test.sql",
	}

	err := slack.Send(context.Background(), backup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSlackNotifier_Send_NotEnabled(t *testing.T) {
	config := &ports.WebhookNotificationConfig{
		Type:       "slack",
		WebhookURL: "http://example.com/webhook",
		Events:     []string{"success"},
		Enabled:    false,
	}

	slack := notifier.NewSlackNotifier(config)

	backup := &ports.BackupContext{
		BackupName:   "test-backup",
		DatabaseType: "postgres",
		DatabaseName: "mydb",
		Status:       ports.NotifyStatusSuccess,
		EndTime:      time.Now(),
	}

	err := slack.Send(context.Background(), backup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSlackNotifier_Send_IgnoresUnsubscribedEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("webhook should not be called for unsubscribed event")
	}))
	defer server.Close()

	config := &ports.WebhookNotificationConfig{
		Type:       "slack",
		WebhookURL: server.URL,
		Events:     []string{"failure"}, // only subscribe to failures
		Enabled:    true,
	}

	slack := notifier.NewSlackNotifier(config)

	backup := &ports.BackupContext{
		BackupName:   "test-backup",
		DatabaseType: "postgres",
		DatabaseName: "mydb",
		Status:       ports.NotifyStatusSuccess, // send success (which is not subscribed)
		EndTime:      time.Now(),
	}

	err := slack.Send(context.Background(), backup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSlackNotifier_Type(t *testing.T) {
	config := &ports.WebhookNotificationConfig{
		Type:       "slack",
		WebhookURL: "http://example.com/webhook",
		Events:     []string{"success"},
		Enabled:    true,
	}

	slack := notifier.NewSlackNotifier(config)
	if slack.Type() != "slack" {
		t.Fatalf("expected type 'slack', got '%s'", slack.Type())
	}
}

func TestSlackNotifier_IsEnabled(t *testing.T) {
	tests := []struct {
		enabled bool
		want    bool
	}{
		{true, true},
		{false, false},
	}

	for _, tt := range tests {
		config := &ports.WebhookNotificationConfig{
			Type:       "slack",
			WebhookURL: "http://example.com/webhook",
			Enabled:    tt.enabled,
		}
		slack := notifier.NewSlackNotifier(config)
		if slack.IsEnabled() != tt.want {
			t.Fatalf("expected IsEnabled()=%v, got %v", tt.want, slack.IsEnabled())
		}
	}
}

func TestSlackNotifier_Send_DefaultTimeout(t *testing.T) {
	config := &ports.WebhookNotificationConfig{
		Type:           "slack",
		WebhookURL:     "http://example.com/webhook",
		Events:         []string{"success"},
		TimeoutSeconds: 0, // should default to 10
		Enabled:        true,
	}

	_ = notifier.NewSlackNotifier(config)
	if config.TimeoutSeconds != 10 {
		t.Fatalf("expected timeout to default to 10, got %d", config.TimeoutSeconds)
	}
}
