package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
