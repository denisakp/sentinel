package incremental

// BackupDecision describes whether the next backup run should be full or incremental.
type BackupDecision struct {
	Type             string
	Reason           string
	ChainID          string
	ChainIndex       int
	BaselineBackupID string
}

const (
	ReasonNoBaseline  = "no_baseline"
	ReasonForceFull   = "force_full"
	ReasonMaxDepth    = "max_chain_depth_reached"
	ReasonIncremental = "incremental_eligible"
)

// Decide determines the next backup mode using chain state and operator override.
func Decide(previous ChainState, forceFull bool) (BackupDecision, error) {
	backupType, next, err := NextChainState(previous, forceFull)
	if err != nil {
		return BackupDecision{}, err
	}

	effectiveMaxDepth := previous.MaxDepth
	if effectiveMaxDepth <= 0 {
		effectiveMaxDepth = DefaultMaxChainDepth
	}

	reason := ReasonIncremental
	if forceFull {
		reason = ReasonForceFull
	} else if previous.BaselineBackupID == "" || previous.ChainID == "" {
		reason = ReasonNoBaseline
	} else if previous.CurrentIndex >= effectiveMaxDepth {
		reason = ReasonMaxDepth
	}

	return BackupDecision{
		Type:             backupType,
		Reason:           reason,
		ChainID:          next.ChainID,
		ChainIndex:       next.CurrentIndex,
		BaselineBackupID: next.BaselineBackupID,
	}, nil
}
