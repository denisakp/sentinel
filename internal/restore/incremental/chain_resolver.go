package incremental

import "fmt"

// ChainArtifact describes a single artifact in a resolved chain.
type ChainArtifact struct {
	BackupID         string
	BaselineBackupID string
	ChainIndex       int
	HashAlgorithm    string
	HashValue        string
	TimelineID       string
	ManifestPresent  bool
	HashVerified     bool
}

// ResolvedChain contains validated artifacts in execution order.
type ResolvedChain struct {
	BaselineBackupID string
	ArtifactIDs      []string
	Depth            int
	TargetBackupID   string
}

// ResolveOrderedChain validates continuity and returns ordered backup IDs.
func ResolveOrderedChain(artifacts []ChainArtifact, targetBackupID string) (*ResolvedChain, error) {
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("broken_lineage_chain: rule_1_baseline_missing")
	}
	if artifacts[0].ChainIndex != 0 || artifacts[0].BackupID == "" {
		return nil, fmt.Errorf("broken_lineage_chain: rule_1_baseline_missing")
	}
	if !artifacts[0].ManifestPresent {
		return nil, fmt.Errorf("broken_lineage_chain: rule_1_baseline_manifest_missing")
	}
	if !artifacts[0].HashVerified {
		return nil, fmt.Errorf("broken_lineage_chain: rule_7_hash_verification_failed")
	}

	baselineID := artifacts[0].BackupID
	timelineID := artifacts[0].TimelineID

	for i, artifact := range artifacts {
		if artifact.BackupID == "" || !artifact.ManifestPresent {
			return nil, fmt.Errorf("broken_lineage_chain: rule_2_intermediate_missing")
		}
		if artifact.ChainIndex != i {
			return nil, fmt.Errorf("broken_lineage_chain: rule_4_non_contiguous_chain_index")
		}
		if i > 0 && artifact.BaselineBackupID != baselineID {
			return nil, fmt.Errorf("broken_lineage_chain: rule_3_baseline_mismatch")
		}
		if timelineID != "" && artifact.TimelineID != "" && artifact.TimelineID != timelineID {
			return nil, fmt.Errorf("timeline_mismatch: rule_5_timeline_divergence")
		}
		if !artifact.HashVerified {
			return nil, fmt.Errorf("broken_lineage_chain: rule_7_hash_verification_failed")
		}
	}

	target := targetBackupID
	if target == "" {
		target = artifacts[len(artifacts)-1].BackupID
	} else {
		found := false
		for _, artifact := range artifacts {
			if artifact.BackupID == target {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("broken_lineage_chain: rule_6_target_not_in_chain")
		}
	}

	ids := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		ids = append(ids, artifact.BackupID)
	}

	return &ResolvedChain{
		BaselineBackupID: baselineID,
		ArtifactIDs:      ids,
		Depth:            len(ids),
		TargetBackupID:   target,
	}, nil
}
