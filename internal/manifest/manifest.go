package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/denisakp/sentinel/internal/crypto"
)

// ErrNoManifest is returned when a manifest file does not exist.
// Callers should treat this as a pre-v1.1 backup and proceed with a WARN.
var ErrNoManifest = errors.New("manifest not found")

// WriteManifest serialises m to a JSON file at path.
func WriteManifest(path string, m *BackupManifest) error {
	if path == "" {
		return fmt.Errorf("manifest path is empty")
	}
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write manifest to %q: %w", path, err)
	}

	return nil
}

// ReadManifest deserialises a manifest from a JSON file at path.
// Returns ErrNoManifest if the file does not exist.
func ReadManifest(path string) (*BackupManifest, error) {
	if path == "" {
		return nil, fmt.Errorf("manifest path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoManifest
		}
		return nil, fmt.Errorf("failed to read manifest from %q: %w", path, err)
	}

	var m BackupManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest at %q: %w", path, err)
	}

	if m.BackupID == "" {
		return nil, fmt.Errorf("invalid manifest at %q: backup_id is empty", path)
	}
	if m.Hash.Algorithm == "" {
		return nil, fmt.Errorf("invalid manifest at %q: hash.algorithm is empty", path)
	}
	if m.Hash.Value == "" {
		return nil, fmt.Errorf("invalid manifest at %q: hash.value is empty", path)
	}

	return &m, nil
}

// LoadRestoreManifest loads restore manifest metadata and preserves ErrNoManifest semantics.
func LoadRestoreManifest(path string) (*BackupManifest, error) {
	if path == "" {
		return nil, ErrNoManifest
	}
	m, err := ReadManifest(path)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ValidateIncrementalLineageContract verifies required lineage fields when incremental metadata is present.
func ValidateIncrementalLineageContract(m *BackupManifest) error {
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

// VerifyBackupHash verifies a backup file against the expected hash value.
// Only SHA-256 is supported.
func VerifyBackupHash(path, algorithm, expected string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("backup path is empty")
	}
	if strings.TrimSpace(expected) == "" {
		return fmt.Errorf("expected hash is empty")
	}

	algo := strings.ToLower(strings.TrimSpace(algorithm))
	if algo == "" {
		algo = "sha256"
	}
	if algo != "sha256" {
		return fmt.Errorf("unsupported hash algorithm %q", algorithm)
	}

	computed, err := computeSHA256(path)
	if err != nil {
		return fmt.Errorf("failed to compute hash for %q: %w", path, err)
	}

	if !strings.EqualFold(computed, strings.TrimSpace(expected)) {
		return fmt.Errorf("hash mismatch: expected=%s computed=%s", expected, computed)
	}

	return nil
}

func computeSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hw := crypto.NewHashingWriter(io.Discard)
	if _, err := io.Copy(hw, f); err != nil {
		return "", err
	}

	return hw.Sum(), nil
}
