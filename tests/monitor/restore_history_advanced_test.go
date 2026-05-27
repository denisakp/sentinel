package monitor_test

import (
	"context"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestRestoreHistoryIncludesAdvancedFields(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	now := time.Now().UTC().Truncate(time.Second)
	target := now.Add(-30 * time.Minute)
	rec := &ports.RestoreExecution{
		RestoreName:          "restore-pitr",
		DatabaseType:         "postgres",
		DatabaseName:         "app",
		RestoreMode:          "pitr",
		PlanningStatus:       "ready",
		RequestedPITRTimeUTC: &target,
		BaselineBackupID:     "base-001",
		FallbackDecision:     "none",
		RecoveryTimelineID:   "1",
		SourceType:           "local",
		ConflictStrategy:     "error",
		Timestamp:            now,
		DurationMs:           1000,
		Status:               ports.StatusSuccess,
		SourceBackupPath:     "backup.sql",
		VerificationPassed:   true,
		CreatedAt:            now,
		FinishedAt:           &now,
	}

	if err := mon.RecordRestoreExecution(context.Background(), rec); err != nil {
		t.Fatalf("RecordRestoreExecution() error = %v", err)
	}

	items, err := mon.ListRestoreExecutions(context.Background(), &ports.RestoreFilter{RestoreName: "restore-pitr"}, 10, 0)
	if err != nil {
		t.Fatalf("ListRestoreExecutions() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 restore execution, got %d", len(items))
	}
	got := items[0]
	if got.RestoreMode != "pitr" {
		t.Fatalf("RestoreMode = %q, want pitr", got.RestoreMode)
	}
	if got.PlanningStatus != "ready" {
		t.Fatalf("PlanningStatus = %q, want ready", got.PlanningStatus)
	}
	if got.BaselineBackupID != "base-001" {
		t.Fatalf("BaselineBackupID = %q, want base-001", got.BaselineBackupID)
	}
	if got.FallbackDecision != "none" {
		t.Fatalf("FallbackDecision = %q, want none", got.FallbackDecision)
	}
	if got.RecoveryTimelineID != "1" {
		t.Fatalf("RecoveryTimelineID = %q, want 1", got.RecoveryTimelineID)
	}
	if got.RequestedPITRTimeUTC == nil {
		t.Fatal("RequestedPITRTimeUTC is nil")
	}
}
