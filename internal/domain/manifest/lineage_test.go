package manifest_test

import (
	"testing"

	"github.com/denisakp/sentinel/internal/domain/manifest"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestValidateIncrementalLineageContract(t *testing.T) {
	m := &ports.BackupManifest{
		BackupID: "inc-001",
		Hash:     ports.HashInfo{Algorithm: "sha256", Value: "abc"},
		AdvancedRestore: &ports.AdvancedRestoreMetadata{
			IncrementalLineage: &ports.IncrementalLineageMetadata{
				ChainID:       "chain-1",
				ChainIndex:    1,
				MaxChainDepth: 6,
			},
		},
	}

	if err := manifest.ValidateIncrementalLineageContract(m); err != nil {
		t.Fatalf("ValidateIncrementalLineageContract() unexpected error = %v", err)
	}

	m.AdvancedRestore.IncrementalLineage.ChainID = ""
	if err := manifest.ValidateIncrementalLineageContract(m); err == nil {
		t.Fatal("expected validation error for empty chain_id")
	}
}
