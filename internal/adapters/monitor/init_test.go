package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestNewMonitor_RepairsRestoreStatusConstraint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}

	now := time.Now().UTC()
	for _, status := range []string{ports.StatusTimeout, ports.StatusSkipped} {
		rec := &ports.RestoreExecution{
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

func TestNewMonitor_AcceptsAdvancedRestoreFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	target := now.Add(-time.Minute)
	rec := &ports.RestoreExecution{
		RestoreName:          "restore-advanced",
		DatabaseType:         "postgres",
		DatabaseName:         "db",
		RestoreMode:          "pitr",
		PlanningStatus:       "ready",
		RequestedPITRTimeUTC: &target,
		BaselineBackupID:     "base-001",
		FallbackDecision:     "none",
		FallbackReason:       "",
		FallbackBackupID:     "",
		RecoveryTimelineID:   "1",
		SourceType:           "local",
		ConflictStrategy:     "error",
		Timestamp:            now,
		Status:               ports.StatusCompleted,
		SourceBackupPath:     "backup.sql",
		CreatedAt:            now,
	}
	if err := mon.RecordRestoreExecution(context.Background(), rec); err != nil {
		t.Fatalf("RecordRestoreExecution() error = %v", err)
	}

	rows, err := mon.ListRestoreExecutions(context.Background(), &ports.RestoreFilter{RestoreName: "restore-advanced"}, 10, 0)
	if err != nil {
		t.Fatalf("ListRestoreExecutions() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
}

func TestNewMonitor_PersistsFallbackReasonFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	rec := &ports.RestoreExecution{
		RestoreName:      "restore-fallback",
		DatabaseType:     "postgres",
		DatabaseName:     "db",
		RestoreMode:      "full",
		PlanningStatus:   "ready",
		FallbackDecision: "full_restore",
		FallbackReason:   "incremental_capability_unavailable",
		FallbackBackupID: "base-001",
		SourceType:       "local",
		ConflictStrategy: "error",
		Timestamp:        now,
		Status:           ports.StatusSuccess,
		SourceBackupPath: "backup.sql",
		CreatedAt:        now,
	}
	if err := mon.RecordRestoreExecution(context.Background(), rec); err != nil {
		t.Fatalf("RecordRestoreExecution() error = %v", err)
	}

	rows, err := mon.ListRestoreExecutions(context.Background(), &ports.RestoreFilter{RestoreName: "restore-fallback"}, 10, 0)
	if err != nil {
		t.Fatalf("ListRestoreExecutions() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].FallbackReason != "incremental_capability_unavailable" {
		t.Fatalf("FallbackReason = %q", rows[0].FallbackReason)
	}
	if rows[0].FallbackBackupID != "base-001" {
		t.Fatalf("FallbackBackupID = %q", rows[0].FallbackBackupID)
	}
}

func TestNewMonitor_AcceptsIncrementalObservabilityFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	backupRec := &ports.Execution{
		BackupName:          "backup-incremental",
		DatabaseType:        "postgres",
		Timestamp:           now,
		Status:              ports.StatusCompleted,
		StorageBackend:      "local",
		FilePath:            "backup.sql",
		BackupType:          "incremental",
		ChainID:             "chain-001",
		ChainIndex:          2,
		DeltaSizeBytes:      1024,
		FullBackupSizeBytes: 8192,
		CreatedAt:           now,
	}
	if err := mon.RecordExecution(context.Background(), backupRec); err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}

	restoreRec := &ports.RestoreExecution{
		RestoreName:        "restore-incremental",
		DatabaseType:       "postgres",
		DatabaseName:       "db",
		RestoreMode:        "incremental",
		PlanningStatus:     "ready",
		BaselineBackupID:   "base-001",
		FallbackDecision:   "none",
		ChainDepth:         3,
		ChainID:            "chain-001",
		AssemblyDurationMs: 250,
		SourceType:         "local",
		ConflictStrategy:   "error",
		Timestamp:          now,
		Status:             ports.StatusCompleted,
		SourceBackupPath:   "backup.sql",
		CreatedAt:          now,
	}
	if err := mon.RecordRestoreExecution(context.Background(), restoreRec); err != nil {
		t.Fatalf("RecordRestoreExecution() error = %v", err)
	}
}
