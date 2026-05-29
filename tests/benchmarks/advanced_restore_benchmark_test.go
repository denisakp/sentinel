package benchmarks

import (
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/adapters/notifier"
	"github.com/denisakp/sentinel/internal/restore"
)

func BenchmarkAdvancedRestorePlannerOverhead(b *testing.B) {
	windowStart := time.Date(2026, time.March, 20, 22, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, time.March, 21, 0, 0, 0, 0, time.UTC)
	targetTime := time.Date(2026, time.March, 20, 23, 59, 0, 0, time.UTC)

	job := config.RestoreJob{Type: "postgres"}
	pitrRequest := &config.AdvancedRestoreRequest{
		RestoreMode:      "pitr",
		PITRInputValue:   targetTime.Format(time.RFC3339),
		PITRTimestampUTC: &targetTime,
	}
	pitrManifest := &ports.BackupManifest{
		BackupID: "postgres-base-2026-03-20T22-00-00Z",
		AdvancedRestore: &ports.AdvancedRestoreMetadata{
			Capabilities:                  []string{"pitr"},
			RecoverableWindowStartUTC:     &windowStart,
			RecoverableWindowEndUTC:       &windowEnd,
			RequiresIntegrityVerification: true,
		},
	}

	incrementalRequest := &config.AdvancedRestoreRequest{
		RestoreMode:           "incremental",
		IncrementalFromBackup: "postgres-base-2026-03-19",
	}
	incrementalManifest := &ports.BackupManifest{
		BackupID: "postgres-delta-2026-03-20",
		AdvancedRestore: &ports.AdvancedRestoreMetadata{
			Capabilities: []string{"incremental"},
			IncrementalLineage: &ports.IncrementalLineageMetadata{
				BaselineBackupID:   "postgres-base-2026-03-19",
				ExecutionSupported: false,
				RequiredBackupIDs: []string{
					"postgres-base-2026-03-19",
					"postgres-delta-2026-03-20",
				},
			},
		},
	}

	b.ReportAllocs()
	b.Run("pitr_ready", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			plan, err := restore.PlanAdvancedRestore(job, pitrRequest, pitrManifest)
			if err != nil {
				b.Fatalf("plan pitr restore: %v", err)
			}
			if plan.Status != restore.PlanStatusReady {
				b.Fatalf("unexpected plan status: %s", plan.Status)
			}
		}
	})

	b.Run("incremental_confirmation_required", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			plan, err := restore.PlanAdvancedRestore(job, incrementalRequest, incrementalManifest)
			if err != nil {
				b.Fatalf("plan incremental restore: %v", err)
			}
			if plan.Status != restore.PlanStatusConfirmationRequired {
				b.Fatalf("unexpected plan status: %s", plan.Status)
			}
		}
	})
}

func BenchmarkAdvancedRestoreTransferMetricReporting(b *testing.B) {
	restoreContext := &ports.RestoreContext{
		RestoreName:        "postgres-incident-recovery",
		DatabaseType:       "postgres",
		DatabaseName:       "appdb",
		Status:             ports.NotifyStatusSuccess,
		StartTime:          time.Date(2026, time.March, 20, 23, 55, 0, 0, time.UTC),
		EndTime:            time.Date(2026, time.March, 21, 0, 9, 32, 0, time.UTC),
		BytesRestored:      25 * 1024 * 1024 * 1024,
		SourceBackupPath:   "/tmp/appdb-base.dump",
		VerificationPassed: true,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatted := notifier.FormatRestoreMessage(restoreContext)
		if formatted == nil {
			b.Fatal("expected formatted restore message")
		}
		if formatted.Details["Bytes Restored"] == "" {
			b.Fatal("missing bytes restored detail")
		}
		if !strings.Contains(formatted.MessageText, "Bytes Restored") {
			b.Fatal("missing bytes restored message text")
		}
	}
}
