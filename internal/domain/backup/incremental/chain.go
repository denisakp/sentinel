package incremental

import (
	"fmt"
	"strings"
	"time"
)

const DefaultMaxChainDepth = 6

// ChainState represents the current lineage state for a backup job.
type ChainState struct {
	ChainID          string
	BaselineBackupID string
	CurrentIndex     int
	MaxDepth         int
}

// NextChainState computes the next state for a backup execution.
// It returns the planned backup type (full or incremental) and the resulting chain state.
func NextChainState(previous ChainState, forceFull bool) (backupType string, next ChainState, err error) {
	maxDepth := previous.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxChainDepth
	}

	if forceFull || previous.ChainID == "" || previous.BaselineBackupID == "" || previous.CurrentIndex >= maxDepth {
		chainID := newChainID()
		return "full", ChainState{
			ChainID:          chainID,
			BaselineBackupID: "",
			CurrentIndex:     0,
			MaxDepth:         maxDepth,
		}, nil
	}

	if previous.CurrentIndex < 0 {
		return "", ChainState{}, fmt.Errorf("current chain index cannot be negative")
	}
	if strings.TrimSpace(previous.BaselineBackupID) == "" {
		return "", ChainState{}, fmt.Errorf("baseline backup id is required for incremental planning")
	}

	return "incremental", ChainState{
		ChainID:          previous.ChainID,
		BaselineBackupID: previous.BaselineBackupID,
		CurrentIndex:     previous.CurrentIndex + 1,
		MaxDepth:         maxDepth,
	}, nil
}

func newChainID() string {
	return fmt.Sprintf("chain-%d", nowUTCUnix())
}

var nowUTCUnix = func() int64 {
	return time.Now().UTC().Unix()
}
