package runtime

import (
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	domainrestore "github.com/denisakp/sentinel/internal/domain/restore"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestPlanAdvancedRestore(t *testing.T) {
	now := time.Date(2026, 3, 20, 22, 0, 0, 0, time.UTC)
	start := time.Date(2026, 3, 20, 20, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 20, 23, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		job        config.RestoreJob
		request    *config.AdvancedRestoreRequest
		manifest   *ports.BackupManifest
		wantStatus domainrestore.PlanStatus
		wantReason string
		wantMode   domainrestore.AdvancedRestoreMode
	}{
		{
			name: "full mode ready",
			job:  config.RestoreJob{Type: "postgres"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode: "full",
			},
			wantStatus: domainrestore.PlanStatusReady,
			wantReason: ReasonCodeReady,
			wantMode:   domainrestore.AdvancedRestoreModeFull,
		},
		{
			name: "pitr outside postgres rejected",
			job:  config.RestoreJob{Type: "mysql"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode:      "pitr",
				PITRTimestampUTC: &now,
			},
			wantStatus: domainrestore.PlanStatusRejected,
			wantReason: ReasonCodeUnsupportedDatabaseType,
			wantMode:   domainrestore.AdvancedRestoreModePITR,
		},
		{
			name: "pitr within window ready",
			job:  config.RestoreJob{Type: "postgres"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode:      "pitr",
				PITRTimestampUTC: &now,
			},
			manifest: &ports.BackupManifest{
				BackupID: "base-001",
				AdvancedRestore: &ports.AdvancedRestoreMetadata{
					Capabilities:                  []string{"full", "pitr"},
					RecoverableWindowStartUTC:     &start,
					RecoverableWindowEndUTC:       &end,
					RequiresIntegrityVerification: true,
				},
			},
			wantStatus: domainrestore.PlanStatusReady,
			wantReason: ReasonCodeReady,
			wantMode:   domainrestore.AdvancedRestoreModePITR,
		},
		{
			name: "incremental fallback confirmation required",
			job:  config.RestoreJob{Type: "postgres"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode:           "incremental",
				IncrementalFromBackup: "base-001",
			},
			manifest: &ports.BackupManifest{
				AdvancedRestore: &ports.AdvancedRestoreMetadata{
					Capabilities: []string{"incremental"},
					IncrementalLineage: &ports.IncrementalLineageMetadata{
						BaselineBackupID:   "base-001",
						ExecutionSupported: false,
					},
				},
			},
			wantStatus: domainrestore.PlanStatusConfirmationRequired,
			wantReason: ReasonCodeFullFallbackConfirmationRequired,
			wantMode:   domainrestore.AdvancedRestoreModeIncremental,
		},
		{
			name: "incremental fallback approved with confirmation",
			job:  config.RestoreJob{Type: "postgres"},
			request: &config.AdvancedRestoreRequest{
				RestoreMode:           "incremental",
				IncrementalFromBackup: "base-001",
				ConfirmFullFallback:   true,
			},
			manifest: &ports.BackupManifest{
				AdvancedRestore: &ports.AdvancedRestoreMetadata{
					Capabilities: []string{"incremental"},
					IncrementalLineage: &ports.IncrementalLineageMetadata{
						BaselineBackupID:   "base-001",
						ExecutionSupported: false,
					},
				},
			},
			wantStatus: domainrestore.PlanStatusReady,
			wantReason: ReasonCodeFullFallbackApproved,
			wantMode:   domainrestore.AdvancedRestoreModeFull,
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

			if tt.name == "incremental fallback confirmation required" {
				if plan.Fallback != domainrestore.FallbackCandidateFullRestore {
					t.Fatalf("Fallback = %q, want %q", plan.Fallback, domainrestore.FallbackCandidateFullRestore)
				}
				if plan.FallbackReason != ReasonCodeIncrementalCapabilityUnavailable {
					t.Fatalf("FallbackReason = %q, want %q", plan.FallbackReason, ReasonCodeIncrementalCapabilityUnavailable)
				}
				if plan.FallbackBackupID != "base-001" {
					t.Fatalf("FallbackBackupID = %q, want base-001", plan.FallbackBackupID)
				}
			}

			if tt.name == "incremental fallback approved with confirmation" {
				if plan.Fallback != domainrestore.FallbackCandidateFullRestore {
					t.Fatalf("Fallback = %q, want %q", plan.Fallback, domainrestore.FallbackCandidateFullRestore)
				}
				if plan.FallbackReason != ReasonCodeIncrementalCapabilityUnavailable {
					t.Fatalf("FallbackReason = %q, want %q", plan.FallbackReason, ReasonCodeIncrementalCapabilityUnavailable)
				}
				if plan.FallbackBackupID != "base-001" {
					t.Fatalf("FallbackBackupID = %q, want base-001", plan.FallbackBackupID)
				}
			}
		})
	}
}
