package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNewMonitor_RepairsRestoreStatusConstraint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}

	now := time.Now().UTC()
	for _, status := range []string{StatusTimeout, StatusSkipped} {
		rec := &RestoreExecution{
			RestoreName:      "restore-job",
			DatabaseType:     "postgres",
			DatabaseName:     "db",
			SourceType:       "local",
			ConflictStrategy: "error",
			Timestamp:        now,
			Status:           status,
			SourceBackupPath: "backup.sql",
			CreatedAt:        now,
		}
		if err := mon.RecordRestoreExecution(context.Background(), rec); err != nil {
			_ = mon.Close()
			t.Fatalf("RecordRestoreExecution() with status %q error = %v", status, err)
		}
	}

	if err := mon.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
