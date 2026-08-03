package notifier_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/notifier"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestDispatcher_Notify_Success(t *testing.T) {
	callCount := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	dispatcher := notifier.NewDispatcher(nil)

	config1 := &ports.WebhookNotificationConfig{
		Type:       "slack",
		WebhookURL: server.URL,
		Events:     []string{"success"},
		Enabled:    true,
	}

	config2 := &ports.WebhookNotificationConfig{
		Type:       "discord",
		WebhookURL: server.URL,
		Events:     []string{"success"},
		Enabled:    true,
	}

	dispatcher.AddWebhookNotifier(config1)
	dispatcher.AddWebhookNotifier(config2)

	backup := &ports.BackupContext{
		BackupName:   "test",
		DatabaseType: "postgres",
		DatabaseName: "db",
		Status:       ports.NotifyStatusSuccess,
		EndTime:      time.Now(),
	}

	err := dispatcher.Notify(backup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if callCount.Load() != 2 {
		t.Fatalf("expected 2 webhook calls, got %d", callCount.Load())
	}
}

func TestDispatcher_Notify_Empty(t *testing.T) {
	dispatcher := notifier.NewDispatcher(nil)

	backup := &ports.BackupContext{
		BackupName:   "test",
		DatabaseType: "postgres",
		DatabaseName: "db",
		Status:       ports.NotifyStatusSuccess,
		EndTime:      time.Now(),
	}

	err := dispatcher.Notify(backup)
	if err != nil {
		t.Fatalf("expected no error for empty dispatcher, got %v", err)
	}
}

func TestDispatcher_Count(t *testing.T) {
	dispatcher := notifier.NewDispatcher(nil)

	if dispatcher.Count() != 0 {
		t.Fatalf("expected 0 notifiers, got %d", dispatcher.Count())
	}

	config := &ports.WebhookNotificationConfig{
		Type:       "slack",
		WebhookURL: "http://example.com/webhook",
		Enabled:    true,
	}

	dispatcher.AddWebhookNotifier(config)
	if dispatcher.Count() != 1 {
		t.Fatalf("expected 1 notifier, got %d", dispatcher.Count())
	}

	dispatcher.Clear()
	if dispatcher.Count() != 0 {
		t.Fatalf("expected 0 notifiers after clear, got %d", dispatcher.Count())
	}
}

func TestDispatcher_Notify_IgnoresDisabledChannels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("disabled notifier should not be called")
	}))
	defer server.Close()

	dispatcher := notifier.NewDispatcher(nil)

	config := &ports.WebhookNotificationConfig{
		Type:       "slack",
		WebhookURL: server.URL,
		Events:     []string{"success"},
		Enabled:    false,
	}

	dispatcher.AddWebhookNotifier(config)

	backup := &ports.BackupContext{
		BackupName:   "test",
		DatabaseType: "postgres",
		DatabaseName: "db",
		Status:       ports.NotifyStatusSuccess,
		EndTime:      time.Now(),
	}

	err := dispatcher.Notify(backup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDispatcher_NotifyAsync(t *testing.T) {
	callCount := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := notifier.NewDispatcher(nil)

	config := &ports.WebhookNotificationConfig{
		Type:       "slack",
		WebhookURL: server.URL,
		Events:     []string{"success"},
		Enabled:    true,
	}

	dispatcher.AddWebhookNotifier(config)

	backup := &ports.BackupContext{
		BackupName:   "test",
		DatabaseType: "postgres",
		DatabaseName: "db",
		Status:       ports.NotifyStatusSuccess,
		EndTime:      time.Now(),
	}

	resultChan := dispatcher.NotifyAsync(backup)

	errorCount := 0
	for range resultChan {
		errorCount++
	}

	if callCount.Load() != 1 {
		t.Fatalf("expected 1 webhook call, got %d", callCount.Load())
	}
	if errorCount != 0 {
		t.Fatalf("expected 0 errors, got %d", errorCount)
	}
}

func TestDispatcher_SetTimeout(t *testing.T) {
	dispatcher := notifier.NewDispatcher(nil)

	timeout := 5 * time.Second
	dispatcher.SetTimeout(timeout)
}

func TestDispatcher_AddWebhookNotifier_InvalidURL(t *testing.T) {
	dispatcher := notifier.NewDispatcher(nil)

	config := &ports.WebhookNotificationConfig{
		Type:       "slack",
		WebhookURL: "",
		Enabled:    true,
	}

	err := dispatcher.AddWebhookNotifier(config)
	if err == nil {
		t.Fatal("expected error for empty webhook URL")
	}
}

func TestDispatcher_AddWebhookNotifier_NilConfig(t *testing.T) {
	dispatcher := notifier.NewDispatcher(nil)

	err := dispatcher.AddWebhookNotifier(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}
