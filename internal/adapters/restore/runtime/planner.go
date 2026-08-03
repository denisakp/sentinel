package runtime

// Config-typed bridge over the pure planner relocated to
// internal/domain/restore/planner.go. External callers
// (CLI validate-chain, integration tests, benchmarks) keep the pre-carve
// signatures; reason-code constants are re-exported.

import (
	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/config"
	domainrestore "github.com/denisakp/sentinel/internal/domain/restore"
	"github.com/denisakp/sentinel/internal/ports"
)

const (
	ReasonCodeReady                            = domainrestore.ReasonCodeReady
	ReasonCodeInvalidMode                      = domainrestore.ReasonCodeInvalidMode
	ReasonCodeUnsupportedDatabaseType          = domainrestore.ReasonCodeUnsupportedDatabaseType
	ReasonCodeMissingAdvancedMetadata          = domainrestore.ReasonCodeMissingAdvancedMetadata
	ReasonCodeMissingPITRTimestamp             = domainrestore.ReasonCodeMissingPITRTimestamp
	ReasonCodePITRWindowUnavailable            = domainrestore.ReasonCodePITRWindowUnavailable
	ReasonCodePITROutsideRecoverableWindow     = domainrestore.ReasonCodePITROutsideRecoverableWindow
	ReasonCodeMissingIncrementalBaseline       = domainrestore.ReasonCodeMissingIncrementalBaseline
	ReasonCodeIncrementalCapabilityUnavailable = domainrestore.ReasonCodeIncrementalCapabilityUnavailable
	ReasonCodeIncompatibleIncrementalBaseline  = domainrestore.ReasonCodeIncompatibleIncrementalBaseline
	ReasonCodeFullFallbackConfirmationRequired = domainrestore.ReasonCodeFullFallbackConfirmationRequired
	ReasonCodeFullFallbackApproved             = domainrestore.ReasonCodeFullFallbackApproved
)

// toPlanRequest mirrors config.AdvancedRestoreRequest into the pure domain
// PlanRequest.
func toPlanRequest(request *config.AdvancedRestoreRequest) *domainrestore.PlanRequest {
	if request == nil {
		return nil
	}
	return &domainrestore.PlanRequest{
		RestoreMode:           request.RestoreMode,
		PITRTimestampUTC:      request.PITRTimestampUTC,
		PITRInputValue:        request.PITRInputValue,
		PITRTargetTimeline:    request.PITRTargetTimeline,
		IncrementalFromBackup: request.IncrementalFromBackup,
		ConfirmFullFallback:   request.ConfirmFullFallback,
	}
}

// PlanAdvancedRestore computes a high-level restore plan before the executor
// stages artifacts.
func PlanAdvancedRestore(job config.RestoreJob, request *config.AdvancedRestoreRequest, m *ports.BackupManifest) (domainrestore.AdvancedRestorePlan, error) {
	return domainrestore.Plan(job.Type, toPlanRequest(request), m)
}

// PlanAdvancedRestoreFromManifestPath loads manifest metadata then computes a plan.
func PlanAdvancedRestoreFromManifestPath(job config.RestoreJob, request *config.AdvancedRestoreRequest, manifestPath string) (domainrestore.AdvancedRestorePlan, error) {
	return domainrestore.PlanFromManifestPath(job.Type, toPlanRequest(request), manifestPath, manifest.LoadRestoreManifest)
}
