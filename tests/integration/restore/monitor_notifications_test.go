package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/notifier"
)

func TestRestoreMonitorAndNotificationFlow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "restore-monitor.db")
	mon, err := monitor.NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	exec := &monitor.RestoreExecution{
		RestoreName:        "nightly-restore",
		DatabaseType:       "postgres",
		DatabaseName:       "app",
		SourceType:         "local",
		ConflictStrategy:   "error",
		Timestamp:          now,
		DurationMs:         1500,
		Status:             "failure",
		ErrorMessage:       "context deadline exceeded",
		ErrorReason:        "timeout",
		Reason:             "timeout",
		SourceBackupPath:   "app.sql",
		BytesRestored:      0,
		VerificationPassed: false,
		TimeoutSeconds:     1,
		CreatedAt:          now,
	}
	if err := mon.RecordRestoreExecution(context.Background(), exec); err != nil {
		t.Fatalf("RecordRestoreExecution() error = %v", err)
	}

	restores, err := mon.ListRestoreExecutions(context.Background(), &monitor.RestoreFilter{RestoreName: "nightly-restore"}, 10, 0)
	if err != nil {
		t.Fatalf("ListRestoreExecutions() error = %v", err)
	}
	if len(restores) != 1 {
		t.Fatalf("expected 1 restore execution, got %d", len(restores))
	}
	if restores[0].Status != "failure" {
		t.Fatalf("recorded status = %q, want failure", restores[0].Status)
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := notifier.NewDispatcher(nil)
	if err := dispatcher.AddWebhookNotifier(&notifier.WebhookNotificationConfig{
		Type:       "webhook",
		WebhookURL: server.URL,
		Events:     []string{"failure", "warning", "success"},
		Enabled:    true,
	}); err != nil {
		t.Fatalf("AddWebhookNotifier() error = %v", err)
	}

	restoreCtx := &notifier.RestoreContext{
		RestoreName:        "nightly-restore",
		DatabaseType:       "postgres",
		DatabaseName:       "app",
		Status:             notifier.NotificationStatusFromRestoreStatus(restores[0].Status),
		StartTime:          now,
		EndTime:            now.Add(1500 * time.Millisecond),
		Error:              restores[0].ErrorMessage,
		BytesRestored:      restores[0].BytesRestored,
		SourceBackupPath:   restores[0].SourceBackupPath,
		VerificationPassed: restores[0].VerificationPassed,
	}
	if err := dispatcher.NotifyRestore(restoreCtx); err != nil {
		t.Fatalf("NotifyRestore() error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 notification call, got %d", calls.Load())
	}
}
