package restore

// Relocated from internal/restore/plan_types.go by spec 037. Pure: stdlib only.

import "time"

// AdvancedRestoreMode identifies the requested or resolved advanced restore mode.
type AdvancedRestoreMode string

const (
	AdvancedRestoreModeFull        AdvancedRestoreMode = "full"
	AdvancedRestoreModePITR        AdvancedRestoreMode = "pitr"
	AdvancedRestoreModeIncremental AdvancedRestoreMode = "incremental"
)

// PlanStatus represents the planner outcome before restore execution proceeds.
type PlanStatus string

const (
	PlanStatusReady                PlanStatus = "ready"
	PlanStatusRejected             PlanStatus = "rejected"
	PlanStatusConfirmationRequired PlanStatus = "confirmation_required"
)

// FallbackCandidate describes whether planner can switch to another restore mode.
type FallbackCandidate string

const (
	FallbackCandidateNone        FallbackCandidate = "none"
	FallbackCandidateFullRestore FallbackCandidate = "full_restore"
)

// AdvancedRestorePlan captures the planning decision consumed by executor and monitor paths.
type AdvancedRestorePlan struct {
	Mode                        AdvancedRestoreMode
	Status                      PlanStatus
	ReasonCode                  string
	ResolvedBackupIDs           []string
	ChainDepth                  int
	ResolvedTargetTimeUTC       *time.Time
	RequestedInputPITRTimestamp string
	RequestedTimeline           string
	BaselineBackupID            string
	BaselineCompatible          bool
	RequiresPhysicalRecovery    bool
	RequiresIntegrityCheck      bool
	Fallback                    FallbackCandidate
	FallbackReason              string
	FallbackBackupID            string
	AssemblyDurationMs          int64
}
