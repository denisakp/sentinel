package incremental_test

import (
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestIncrementalObservabilityMetricsSnapshot(t *testing.T) {
	monitor.ResetIncrementalMetrics()
	t.Cleanup(monitor.ResetIncrementalMetrics)

	monitor.ObserveIncrementalBackup("backup-job", &ports.Execution{
		BackupType:          "incremental",
		ChainIndex:          3,
		DeltaSizeBytes:      2048,
		FullBackupSizeBytes: 8192,
	})
	monitor.ObserveIncrementalRestore("restore-job", &ports.RestoreExecution{
		RestoreMode:        "incremental",
		ChainDepth:         4,
		AssemblyDurationMs: 650,
		FallbackDecision:   "full_restore",
		FallbackReason:     "incremental_capability_unavailable",
	})

	snapshot := monitor.SnapshotIncrementalMetrics()
	if snapshot.BackupChainDepth["backup-job"] != 3 {
		t.Fatalf("BackupChainDepth = %d, want 3", snapshot.BackupChainDepth["backup-job"])
	}
	if snapshot.BackupDeltaSizeBytes["backup-job"] != 2048 {
		t.Fatalf("BackupDeltaSizeBytes = %d, want 2048", snapshot.BackupDeltaSizeBytes["backup-job"])
	}
	if snapshot.RestoreChainDepth["restore-job"] != 4 {
		t.Fatalf("RestoreChainDepth = %d, want 4", snapshot.RestoreChainDepth["restore-job"])
	}
	if snapshot.RestoreAssemblyDurationMs["restore-job"] != 650 {
		t.Fatalf("RestoreAssemblyDurationMs = %d, want 650", snapshot.RestoreAssemblyDurationMs["restore-job"])
	}
	if snapshot.RestoreFallbackTotal["full_restore|incremental_capability_unavailable"] != 1 {
		t.Fatalf("RestoreFallbackTotal = %d, want 1", snapshot.RestoreFallbackTotal["full_restore|incremental_capability_unavailable"])
	}
	if len(monitor.IncrementalMetricDefinitions) < 4 {
		t.Fatalf("expected incremental metric definitions, got %d", len(monitor.IncrementalMetricDefinitions))
	}
}
