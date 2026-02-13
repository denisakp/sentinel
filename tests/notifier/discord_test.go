package notifier_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/notifier"
)

func TestDiscordNotifier_Send_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Fatal("expected non-empty request body")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	config := &notifier.WebhookNotificationConfig{
		Type:           "discord",
		WebhookURL:     server.URL,
		Events:         []string{"success", "failure"},
		TimeoutSeconds: 5,
		Enabled:        true,
	}

	discord := notifier.NewDiscordNotifier(config)

	backup := &notifier.BackupContext{
		BackupName:   "test-backup",
		DatabaseType: "mysql",
		DatabaseName: "proddb",
		Status:       notifier.StatusFailure,
		StartTime:    time.Now().Add(-10 * time.Minute),
		EndTime:      time.Now(),
		Error:        "connection timeout",
		FileSize:     0,
		FilePath:     "",
	}

	err := discord.Send(context.Background(), backup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDiscordNotifier_Type(t *testing.T) {
	config := &notifier.WebhookNotificationConfig{
		Type:       "discord",
		WebhookURL: "http://example.com/webhook",
		Enabled:    true,
	}

	discord := notifier.NewDiscordNotifier(config)
	if discord.Type() != "discord" {
		t.Fatalf("expected type 'discord', got '%s'", discord.Type())
	}
}

func TestDiscordNotifier_IsEnabled(t *testing.T) {
	config := &notifier.WebhookNotificationConfig{
		Type:       "discord",
		WebhookURL: "http://example.com/webhook",
		Enabled:    false,
	}

	discord := notifier.NewDiscordNotifier(config)
	if discord.IsEnabled() {
		t.Fatal("expected IsEnabled() to return false")
	}
}
