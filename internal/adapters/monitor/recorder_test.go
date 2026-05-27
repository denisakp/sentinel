package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestRecordExecution_PersistsIncrementalObservabilityFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	exec := &ports.Execution{
		BackupName:          "backup-incremental",
		DatabaseType:        "postgres",
		Timestamp:           now,
		DurationMs:          1250,
		Status:              ports.StatusCompleted,
		StorageBackend:      "local",
		FilePath:            "backup.sql",
		FileSizeBytes:       2048,
		BackupType:          "incremental",
		ChainID:             "chain-001",
		ChainIndex:          2,
		DeltaSizeBytes:      1024,
		FullBackupSizeBytes: 8192,
		CreatedAt:           now,
	}
	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}

	stored, err := mon.GetExecution(context.Background(), exec.ID)
	if err != nil {
		t.Fatalf("GetExecution() error = %v", err)
	}
	if stored.BackupType != "incremental" {
		t.Fatalf("BackupType = %q, want incremental", stored.BackupType)
	}
	if stored.ChainID != "chain-001" {
		t.Fatalf("ChainID = %q, want chain-001", stored.ChainID)
	}
	if stored.ChainIndex != 2 {
		t.Fatalf("ChainIndex = %d, want 2", stored.ChainIndex)
	}
	if stored.DeltaSizeBytes != 1024 {
		t.Fatalf("DeltaSizeBytes = %d, want 1024", stored.DeltaSizeBytes)
	}
	if stored.FullBackupSizeBytes != 8192 {
		t.Fatalf("FullBackupSizeBytes = %d, want 8192", stored.FullBackupSizeBytes)
	}
}

func TestRecordRestoreExecution_PersistsIncrementalObservabilityFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	rec := &ports.RestoreExecution{
		RestoreName:        "restore-incremental",
		DatabaseType:       "postgres",
		DatabaseName:       "app",
		RestoreMode:        "incremental",
		PlanningStatus:     "ready",
		BaselineBackupID:   "base-001",
		FallbackDecision:   "full_restore",
		FallbackReason:     "incremental_capability_unavailable",
		FallbackBackupID:   "base-001",
		ChainDepth:         3,
		ChainID:            "chain-001",
		AssemblyDurationMs: 275,
		SourceType:         "local",
		ConflictStrategy:   "error",
		Timestamp:          now,
		Status:             ports.StatusCompleted,
		SourceBackupPath:   "backup.sql",
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
	if rows[0].FallbackDecision != "full_restore" {
		t.Fatalf("FallbackDecision = %q, want full_restore", rows[0].FallbackDecision)
	}
	if rows[0].FallbackReason != "incremental_capability_unavailable" {
		t.Fatalf("FallbackReason = %q, want incremental_capability_unavailable", rows[0].FallbackReason)
	}
	if rows[0].ChainDepth != 3 {
		t.Fatalf("ChainDepth = %d, want 3", rows[0].ChainDepth)
	}
	if rows[0].AssemblyDurationMs != 275 {
		t.Fatalf("AssemblyDurationMs = %d, want 275", rows[0].AssemblyDurationMs)
	}
}
