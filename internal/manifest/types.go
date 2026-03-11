package manifest

import "time"

// BackupManifest is the single source of truth for backup integrity and restore metadata.
// Written as <backup-filename>.manifest.json alongside the backup file.
type BackupManifest struct {
	BackupID     string          `json:"backup_id"`
	Database     string          `json:"database"`
	DatabaseType string          `json:"database_type"`
	CreatedAt    time.Time       `json:"created_at"`
	SizeBytes    int64           `json:"size_bytes"`
	Hash         HashInfo        `json:"hash"`
	Encryption   *EncryptionInfo `json:"encryption,omitempty"`
}

// HashInfo holds the integrity fingerprint for a backup file.
type HashInfo struct {
	Algorithm      string `json:"algorithm"`
	Value          string `json:"value"`
	PlaintextValue string `json:"plaintext_value,omitempty"`
}

// EncryptionInfo holds encryption metadata (present only when backup is encrypted).
type EncryptionInfo struct {
	Algorithm     string `json:"algorithm"`
	KeyDerivation string `json:"key_derivation"`
	Iterations    int    `json:"iterations"`
	Salt          string `json:"salt"`
	IV            string `json:"iv"`
	AuthTag       string `json:"auth_tag"`
}
