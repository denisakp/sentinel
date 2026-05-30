// Package manifest_store is the driving adapter for ports.ManifestStore.
// It owns the file-system I/O for backup manifest sidecars
// (<artifact>.manifest.json) and the streaming SHA-256 verification path.
//
// Pure validation (incremental lineage contract checks) lives in
// internal/domain/manifest. Relocated from internal/manifest/ by spec 037.
package manifest_store

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/denisakp/sentinel/internal/domain/manifest"
	"github.com/denisakp/sentinel/internal/ports"
)

// WriteManifest serialises m to a JSON file at path.
func WriteManifest(path string, m *ports.BackupManifest) error {
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
// Returns ports.ErrNoManifest if the file does not exist.
func ReadManifest(path string) (*ports.BackupManifest, error) {
	if path == "" {
		return nil, fmt.Errorf("manifest path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ports.ErrNoManifest
		}
		return nil, fmt.Errorf("failed to read manifest from %q: %w", path, err)
	}

	var m ports.BackupManifest
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

// LoadRestoreManifest loads restore manifest metadata and preserves
// ports.ErrNoManifest semantics.
func LoadRestoreManifest(path string) (*ports.BackupManifest, error) {
	if path == "" {
		return nil, ports.ErrNoManifest
	}
	return ReadManifest(path)
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

// Adapter implements ports.ManifestStore. Stateless.
type Adapter struct{}

func (Adapter) Write(path string, m *ports.BackupManifest) error {
	return WriteManifest(path, m)
}

func (Adapter) Read(path string) (*ports.BackupManifest, error) {
	return ReadManifest(path)
}

func (Adapter) LoadForRestore(path string) (*ports.BackupManifest, error) {
	return LoadRestoreManifest(path)
}

func (Adapter) VerifyHash(path, algorithm, expected string) error {
	return VerifyBackupHash(path, algorithm, expected)
}

// ValidateIncrementalLineage delegates to the pure domain validator.
func (Adapter) ValidateIncrementalLineage(m *ports.BackupManifest) error {
	return manifest.ValidateIncrementalLineageContract(m)
}
