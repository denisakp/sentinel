package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestListExecutions_ReturnsIncrementalObservabilityFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	exec := &ports.Execution{
		BackupName:          "backup-incremental",
		DatabaseType:        "mysql",
		Timestamp:           now,
		DurationMs:          1100,
		Status:              ports.StatusCompleted,
		StorageBackend:      "local",
		FilePath:            "backup.sql",
		FileSizeBytes:       4096,
		BackupType:          "incremental",
		ChainID:             "chain-002",
		ChainIndex:          4,
		DeltaSizeBytes:      2048,
		FullBackupSizeBytes: 16384,
		CreatedAt:           now,
	}
	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}

	rows, err := mon.ListExecutions(context.Background(), &ports.Filter{BackupName: "backup-incremental"}, 10, 0)
	if err != nil {
		t.Fatalf("ListExecutions() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].ChainID != "chain-002" || rows[0].ChainIndex != 4 {
		t.Fatalf("unexpected chain fields: %#v", rows[0])
	}
	if rows[0].DeltaSizeBytes != 2048 || rows[0].FullBackupSizeBytes != 16384 {
		t.Fatalf("unexpected size fields: %#v", rows[0])
	}
}

func TestListRestoreExecutions_ReturnsIncrementalObservabilityFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	rec := &ports.RestoreExecution{
		RestoreName:        "restore-incremental",
		DatabaseType:       "mongodb",
		DatabaseName:       "app",
		RestoreMode:        "incremental",
		PlanningStatus:     "ready",
		BaselineBackupID:   "base-002",
		FallbackDecision:   "none",
		ChainDepth:         5,
		ChainID:            "chain-002",
		AssemblyDurationMs: 900,
		SourceType:         "local",
		ConflictStrategy:   "error",
		Timestamp:          now,
		Status:             ports.StatusCompleted,
		SourceBackupPath:   "backup.archive",
		CreatedAt:          now,
	}
	if err := mon.RecordRestoreExecution(context.Background(), rec); err != nil {
		t.Fatalf("RecordRestoreExecution() error = %v", err)
	}

	rows, err := mon.ListRestoreExecutions(context.Background(), &ports.RestoreFilter{RestoreName: "restore-incremental"}, 10, 0)
	if err != nil {
		t.Fatalf("ListRestoreExecutions() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].BaselineBackupID != "base-002" {
		t.Fatalf("BaselineBackupID = %q, want base-002", rows[0].BaselineBackupID)
	}
	if rows[0].FallbackDecision != "none" {
		t.Fatalf("FallbackDecision = %q, want none", rows[0].FallbackDecision)
	}
	if rows[0].ChainDepth != 5 || rows[0].ChainID != "chain-002" {
		t.Fatalf("unexpected chain fields: %#v", rows[0])
	}
	if rows[0].AssemblyDurationMs != 900 {
		t.Fatalf("AssemblyDurationMs = %d, want 900", rows[0].AssemblyDurationMs)
	}
}
