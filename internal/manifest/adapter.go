package manifest

import "github.com/denisakp/sentinel/internal/ports"

// Adapter is a zero-state wrapper exposing the package's free functions as
// methods so the manifest contract has an explicit conformance target for
// ports.ManifestStore. Introduced by spec 028 (FR-004 clause c).
//
// Each method delegates to the existing free function. No additional logic.
type Adapter struct{}

// Write delegates to WriteManifest.
func (Adapter) Write(path string, m *ports.BackupManifest) error {
	return WriteManifest(path, m)
}

// Read delegates to ReadManifest.
func (Adapter) Read(path string) (*ports.BackupManifest, error) {
	return ReadManifest(path)
}

// LoadForRestore delegates to LoadRestoreManifest.
func (Adapter) LoadForRestore(path string) (*ports.BackupManifest, error) {
	return LoadRestoreManifest(path)
}

// VerifyHash delegates to VerifyBackupHash.
func (Adapter) VerifyHash(path, algorithm, expected string) error {
	return VerifyBackupHash(path, algorithm, expected)
}

// ValidateIncrementalLineage delegates to ValidateIncrementalLineageContract.
func (Adapter) ValidateIncrementalLineage(m *ports.BackupManifest) error {
	return ValidateIncrementalLineageContract(m)
}
