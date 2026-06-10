package restore

// Pure advanced-restore planner. Relocated from internal/restore/planner.go
// by spec 038 Sub-PR L: config.RestoreJob collapses to the engine string
// (its only consulted field) and config.AdvancedRestoreRequest to the pure
// PlanRequest mirror. Manifest loading is injected (ports.ManifestStore or
// a driving hook) by PlanFromManifestPath callers.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	domainincr "github.com/denisakp/sentinel/internal/domain/restore/incremental"
	"github.com/denisakp/sentinel/internal/ports"
)

const (
	ReasonCodeReady                            = "ready"
	ReasonCodeInvalidMode                      = "invalid_mode"
	ReasonCodeUnsupportedDatabaseType          = "unsupported_database_type"
	ReasonCodeMissingAdvancedMetadata          = "missing_advanced_metadata"
	ReasonCodeMissingPITRTimestamp             = "missing_pitr_timestamp"
	ReasonCodePITRWindowUnavailable            = "pitr_window_unavailable"
	ReasonCodePITROutsideRecoverableWindow     = "pitr_outside_recoverable_window"
	ReasonCodeMissingIncrementalBaseline       = "missing_incremental_baseline"
	ReasonCodeIncrementalCapabilityUnavailable = "incremental_capability_unavailable"
	ReasonCodeIncompatibleIncrementalBaseline  = "incompatible_incremental_baseline"
	ReasonCodeFullFallbackConfirmationRequired = "full_fallback_confirmation_required"
	ReasonCodeFullFallbackApproved             = "full_fallback_approved"
)

// Plan computes a high-level restore plan before the executor stages
// artifacts. engine is the target database type.
func Plan(engine string, request *PlanRequest, m *ports.BackupManifest) (AdvancedRestorePlan, error) {
	if request == nil {
		return AdvancedRestorePlan{}, fmt.Errorf("advanced restore request is required")
	}

	plan := AdvancedRestorePlan{
		Mode:                        AdvancedRestoreMode(request.RestoreMode),
		Status:                      PlanStatusRejected,
		ReasonCode:                  ReasonCodeInvalidMode,
		RequestedInputPITRTimestamp: request.PITRInputValue,
		RequestedTimeline:           request.PITRTargetTimeline,
		BaselineBackupID:            request.IncrementalFromBackup,
		BaselineCompatible:          false,
		RequiresIntegrityCheck:      false,
		Fallback:                    FallbackCandidateNone,
	}

	switch request.RestoreMode {
	case "", "full":
		plan.Mode = AdvancedRestoreModeFull
		plan.Status = PlanStatusReady
		plan.ReasonCode = ReasonCodeReady
		return plan, nil
	case "pitr":
		return planPITR(engine, request, m, plan), nil
	case "incremental":
		return planIncremental(engine, request, m, plan), nil
	default:
		return plan, nil
	}
}

func planPITR(engine string, request *PlanRequest, m *ports.BackupManifest, plan AdvancedRestorePlan) AdvancedRestorePlan {
	plan.Mode = AdvancedRestoreModePITR
	if engine != "postgres" {
		plan.ReasonCode = ReasonCodeUnsupportedDatabaseType
		return plan
	}
	if request.PITRTimestampUTC == nil {
		plan.ReasonCode = ReasonCodeMissingPITRTimestamp
		return plan
	}
	if m == nil || m.AdvancedRestore == nil {
		plan.ReasonCode = ReasonCodeMissingAdvancedMetadata
		return plan
	}
	if !containsCapability(m.AdvancedRestore.Capabilities, "pitr") {
		plan.ReasonCode = ReasonCodeMissingAdvancedMetadata
		return plan
	}
	if m.AdvancedRestore.RecoverableWindowStartUTC == nil || m.AdvancedRestore.RecoverableWindowEndUTC == nil {
		plan.ReasonCode = ReasonCodePITRWindowUnavailable
		return plan
	}

	target := request.PITRTimestampUTC.UTC()
	start := m.AdvancedRestore.RecoverableWindowStartUTC.UTC()
	end := m.AdvancedRestore.RecoverableWindowEndUTC.UTC()
	if target.Before(start) || target.After(end) {
		plan.ReasonCode = ReasonCodePITROutsideRecoverableWindow
		return plan
	}

	plan.Status = PlanStatusReady
	plan.ReasonCode = ReasonCodeReady
	plan.ResolvedTargetTimeUTC = timePtr(target)
	plan.RequiresPhysicalRecovery = true
	plan.RequiresIntegrityCheck = m.AdvancedRestore.RequiresIntegrityVerification
	if m.BackupID != "" {
		plan.ResolvedBackupIDs = []string{m.BackupID}
	}

	return plan
}

func planIncremental(engine string, request *PlanRequest, m *ports.BackupManifest, plan AdvancedRestorePlan) AdvancedRestorePlan {
	plan.Mode = AdvancedRestoreModeIncremental
	if engine != "postgres" {
		plan.ReasonCode = ReasonCodeUnsupportedDatabaseType
		return plan
	}
	if request.IncrementalFromBackup == "" {
		plan.ReasonCode = ReasonCodeMissingIncrementalBaseline
		return plan
	}
	if m == nil || m.AdvancedRestore == nil || m.AdvancedRestore.IncrementalLineage == nil {
		plan.ReasonCode = ReasonCodeMissingAdvancedMetadata
		return plan
	}
	lineage := m.AdvancedRestore.IncrementalLineage
	if !containsCapability(m.AdvancedRestore.Capabilities, "incremental") {
		plan.ReasonCode = ReasonCodeIncrementalCapabilityUnavailable
		return plan
	}
	if strings.TrimSpace(lineage.BaselineBackupID) == "" {
		plan.ReasonCode = ReasonCodeMissingIncrementalBaseline
		return plan
	}
	if request.IncrementalFromBackup != lineage.BaselineBackupID {
		plan.ReasonCode = ReasonCodeIncompatibleIncrementalBaseline
		return plan
	}

	plan.BaselineCompatible = true
	if !lineage.ExecutionSupported {
		decision := domainincr.EvaluateFallback(request.ConfirmFullFallback, lineage.BaselineBackupID, ReasonCodeIncrementalCapabilityUnavailable)
		plan.Fallback = FallbackCandidateFullRestore
		plan.FallbackReason = decision.Reason
		plan.FallbackBackupID = decision.FallbackBackupID
		if decision.Proceed {
			plan.Status = PlanStatusReady
			plan.Mode = AdvancedRestoreModeFull
			plan.ReasonCode = ReasonCodeFullFallbackApproved
			return plan
		}
		plan.Status = PlanStatusConfirmationRequired
		plan.ReasonCode = ReasonCodeFullFallbackConfirmationRequired
		return plan
	}

	plan.Status = PlanStatusReady
	plan.ReasonCode = ReasonCodeReady
	plan.ResolvedBackupIDs = appendUniqueID(plan.ResolvedBackupIDs, lineage.BaselineBackupID)
	for _, id := range lineage.RequiredBackupIDs {
		plan.ResolvedBackupIDs = appendUniqueID(plan.ResolvedBackupIDs, id)
	}
	if m.BackupID != "" {
		plan.ResolvedBackupIDs = appendUniqueID(plan.ResolvedBackupIDs, m.BackupID)
	}
	plan.ChainDepth = len(plan.ResolvedBackupIDs)

	return plan
}

// PlanFromManifestPath loads manifest metadata through the supplied loader
// (ports.ManifestStore.LoadForRestore or a driving hook) then computes a
// plan. A ports.ErrNoManifest outcome degrades to manifest-less planning.
func PlanFromManifestPath(engine string, request *PlanRequest, manifestPath string, load func(string) (*ports.BackupManifest, error)) (AdvancedRestorePlan, error) {
	if load == nil {
		return AdvancedRestorePlan{}, fmt.Errorf("manifest loader is required")
	}
	m, err := load(manifestPath)
	if err != nil {
		if errors.Is(err, ports.ErrNoManifest) {
			return Plan(engine, request, nil)
		}
		return AdvancedRestorePlan{}, fmt.Errorf("failed to load restore manifest: %w", err)
	}
	return Plan(engine, request, m)
}

func containsCapability(capabilities []string, target string) bool {
	for _, capability := range capabilities {
		if capability == target {
			return true
		}
	}
	return false
}

func timePtr(value time.Time) *time.Time {
	copied := value
	return &copied
}

func appendUniqueID(ids []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ids
	}
	for _, id := range ids {
		if id == candidate {
			return ids
		}
	}
	return append(ids, candidate)
}
