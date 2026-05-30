// TODO(spec-038): remove
//
// Spec 037 Sub-PR E introduced this bridge file. Pure plan types relocated
// to internal/domain/restore/plan_types.go. Legacy callers (CLI, scheduler,
// many integration tests) still import internal/restore; a future spec
// swaps those imports and deletes the rest of internal/restore/ per FR-009.
package restore

import (
	domainrestore "github.com/denisakp/sentinel/internal/domain/restore"
)

// Re-exports of the pure domain types + constants. NO function bodies in this
// file — see specs/037-domain-extraction/research.md R4.

type (
	AdvancedRestoreMode = domainrestore.AdvancedRestoreMode
	PlanStatus          = domainrestore.PlanStatus
	FallbackCandidate   = domainrestore.FallbackCandidate
	AdvancedRestorePlan = domainrestore.AdvancedRestorePlan
)

const (
	AdvancedRestoreModeFull        = domainrestore.AdvancedRestoreModeFull
	AdvancedRestoreModePITR        = domainrestore.AdvancedRestoreModePITR
	AdvancedRestoreModeIncremental = domainrestore.AdvancedRestoreModeIncremental

	PlanStatusReady                = domainrestore.PlanStatusReady
	PlanStatusRejected             = domainrestore.PlanStatusRejected
	PlanStatusConfirmationRequired = domainrestore.PlanStatusConfirmationRequired

	FallbackCandidateNone        = domainrestore.FallbackCandidateNone
	FallbackCandidateFullRestore = domainrestore.FallbackCandidateFullRestore
)
