package manifest

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// ValidateIncrementalLineageContract verifies required lineage fields when
// incremental metadata is present. Pure: no I/O.
//
// Relocated from internal/manifest/manifest.go; the I/O-bound
// Read/Write/VerifyHash helpers stay adapter-side under
// internal/adapters/manifest_store/.
func ValidateIncrementalLineageContract(m *ports.BackupManifest) error {
	if m == nil || m.AdvancedRestore == nil || m.AdvancedRestore.IncrementalLineage == nil {
		return nil
	}

	lineage := m.AdvancedRestore.IncrementalLineage
	if lineage.ChainID == "" {
		return fmt.Errorf("invalid manifest: incremental_lineage.chain_id is empty")
	}
	if lineage.ChainIndex < 0 {
		return fmt.Errorf("invalid manifest: incremental_lineage.chain_index must be >= 0")
	}
	if lineage.MaxChainDepth < 0 {
		return fmt.Errorf("invalid manifest: incremental_lineage.max_chain_depth must be >= 0")
	}

	return nil
}
