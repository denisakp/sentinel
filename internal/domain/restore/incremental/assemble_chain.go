package incremental

import (
	"fmt"
	"time"
)

func AssembleChain(lineage []ChainArtifact, cutoff *time.Time) (*ResolvedChain, error) {
	resolved, err := ResolveOrderedChain(lineage, "")
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(lineage))
	for _, a := range lineage {
		if _, dup := seen[a.BackupID]; dup {
			return nil, fmt.Errorf("duplicate_artifact: %s", a.BackupID)
		}
		seen[a.BackupID] = struct{}{}
	}

	allTimestamped := true
	for _, a := range lineage {
		if a.EndTimestamp.IsZero() {
			allTimestamped = false
			break
		}
	}
	if allTimestamped {
		for i := 1; i < len(lineage); i++ {
			prev, curr := lineage[i-1], lineage[i]
			if curr.EndTimestamp.Before(prev.EndTimestamp) {
				return nil, fmt.Errorf("out_of_order: %s@%s > %s@%s",
					prev.BackupID, prev.EndTimestamp.Format(time.RFC3339),
					curr.BackupID, curr.EndTimestamp.Format(time.RFC3339))
			}
		}
	}

	if cutoff != nil && allTimestamped {
		trimmed := make([]string, 0, len(lineage))
		for _, a := range lineage {
			if !a.EndTimestamp.After(*cutoff) {
				trimmed = append(trimmed, a.BackupID)
			}
		}
		if len(trimmed) == 0 {
			return nil, fmt.Errorf("cutoff_before_baseline: cutoff=%s baseline_end=%s",
				cutoff.Format(time.RFC3339),
				lineage[0].EndTimestamp.Format(time.RFC3339))
		}
		return &ResolvedChain{
			BaselineBackupID: lineage[0].BackupID,
			ArtifactIDs:      trimmed,
			Depth:            len(trimmed),
			TargetBackupID:   trimmed[len(trimmed)-1],
		}, nil
	}

	return resolved, nil
}
