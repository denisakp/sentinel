package incremental

// FallbackDecision captures whether execution should proceed or require explicit confirmation.
type FallbackDecision struct {
	Proceed          bool
	PlanStatus       string
	FallbackBackupID string
	Reason           string
}

// EvaluateFallback enforces explicit confirmation for degraded full restore execution.
func EvaluateFallback(confirmFullFallback bool, fallbackBackupID, fallbackReason string) FallbackDecision {
	if confirmFullFallback {
		return FallbackDecision{
			Proceed:          true,
			PlanStatus:       "ready",
			FallbackBackupID: fallbackBackupID,
			Reason:           fallbackReason,
		}
	}

	return FallbackDecision{
		Proceed:          false,
		PlanStatus:       "confirmation_required",
		FallbackBackupID: fallbackBackupID,
		Reason:           fallbackReason,
	}
}
