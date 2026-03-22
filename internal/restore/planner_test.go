package restore

import (
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/manifest"
)

func TestPlanAdvancedRestore(t *testing.T) {
	now := time.Date(2026, 3, 20, 22, 0, 0, 0, time.UTC)
	start := time.Date(2026, 3, 20, 20, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 20, 23, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		job        config.RestoreJob
		request    *config.AdvancedRestoreRequest
		manifest   *manifest.BackupManifest
		wantStatus PlanStatus
		wantReason string
		wantMode   AdvancedRestoreMode
	}{
		{
			name: "full mode ready",
			job:  config.RestoreJob{Type: "postgres"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode: "full",
			},
			wantStatus: PlanStatusReady,
			wantReason: ReasonCodeReady,
			wantMode:   AdvancedRestoreModeFull,
		},
		{
			name: "pitr outside postgres rejected",
			job:  config.RestoreJob{Type: "mysql"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode:      "pitr",
				PITRTimestampUTC: &now,
			},
			wantStatus: PlanStatusRejected,
			wantReason: ReasonCodeUnsupportedDatabaseType,
			wantMode:   AdvancedRestoreModePITR,
		},
		{
			name: "pitr within window ready",
			job:  config.RestoreJob{Type: "postgres"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode:      "pitr",
				PITRTimestampUTC: &now,
			},
			manifest: &manifest.BackupManifest{
				BackupID: "base-001",
				AdvancedRestore: &manifest.AdvancedRestoreMetadata{
					Capabilities:                  []string{"full", "pitr"},
					RecoverableWindowStartUTC:     &start,
					RecoverableWindowEndUTC:       &end,
					RequiresIntegrityVerification: true,
				},
			},
			wantStatus: PlanStatusReady,
			wantReason: ReasonCodeReady,
			wantMode:   AdvancedRestoreModePITR,
		},
		{
			name: "incremental fallback confirmation required",
			job:  config.RestoreJob{Type: "postgres"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode:           "incremental",
				IncrementalFromBackup: "base-001",
			},
			manifest: &manifest.BackupManifest{
				AdvancedRestore: &manifest.AdvancedRestoreMetadata{
					Capabilities: []string{"incremental"},
					IncrementalLineage: &manifest.IncrementalLineageMetadata{
						BaselineBackupID:   "base-001",
						ExecutionSupported: false,
					},
				},
			},
			wantStatus: PlanStatusConfirmationRequired,
			wantReason: ReasonCodeFullFallbackConfirmationRequired,
			wantMode:   AdvancedRestoreModeIncremental,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := PlanAdvancedRestore(tt.job, tt.request, tt.manifest)
			if err != nil {
				t.Fatalf("PlanAdvancedRestore() error = %v", err)
			}
			if plan.Status != tt.wantStatus {
				t.Fatalf("Status = %q, want %q", plan.Status, tt.wantStatus)
			}
			if plan.ReasonCode != tt.wantReason {
				t.Fatalf("ReasonCode = %q, want %q", plan.ReasonCode, tt.wantReason)
			}
			if plan.Mode != tt.wantMode {
				t.Fatalf("Mode = %q, want %q", plan.Mode, tt.wantMode)
			}
		})
	}
}
