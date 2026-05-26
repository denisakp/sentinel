package ports

import (
	"errors"
	"time"
)

// ManifestStore abstracts *internal/manifest.Adapter (current concrete implementation).
//
// It is the read/write seam for backup manifests — the .manifest.json
// sidecar that records integrity, encryption, and advanced-restore
// metadata for every backup artefact.
type ManifestStore interface {
	Write(path string, m *BackupManifest) error
	Read(path string) (*BackupManifest, error)
	LoadForRestore(path string) (*BackupManifest, error)
	VerifyHash(path, algorithm, expected string) error
	ValidateIncrementalLineage(m *BackupManifest) error
}

// ErrNoManifest is returned when the .manifest.json sidecar is missing.
//
// Callers MUST treat this as a soft signal (pre-v1.1 backup) rather than a
// hard failure: log a WARN and proceed with the raw file.
//
// Relocated from internal/manifest/manifest.go (single source of truth per
// spec 028 FR-003a).
var ErrNoManifest = errors.New("manifest not found")

// BackupManifest is the single source of truth for backup integrity and restore metadata.
// Written as <backup-filename>.manifest.json alongside the backup file.
//
// Relocated from internal/manifest/types.go (single source of truth per
// spec 028 FR-003a).
type BackupManifest struct {
	BackupID        string                   `json:"backup_id"`
	Database        string                   `json:"database"`
	DatabaseType    string                   `json:"database_type"`
	CreatedAt       time.Time                `json:"created_at"`
	SizeBytes       int64                    `json:"size_bytes"`
	Hash            HashInfo                 `json:"hash"`
	Encryption      *EncryptionInfo          `json:"encryption,omitempty"`
	AdvancedRestore *AdvancedRestoreMetadata `json:"advanced_restore,omitempty"`
}

// HashInfo holds the integrity fingerprint for a backup file.
type HashInfo struct {
	Algorithm      string `json:"algorithm"`
	Value          string `json:"value"`
	PlaintextValue string `json:"plaintext_value,omitempty"`
}

// EncryptionInfo holds encryption metadata (present only when backup is encrypted).
type EncryptionInfo struct {
	Algorithm       string `json:"algorithm"`
	KeyDerivation   string `json:"key_derivation"`
	Iterations      int    `json:"iterations"`
	Salt            string `json:"salt"`
	IV              string `json:"iv"`
	AuthTag         string `json:"auth_tag"`
	EnvelopeVersion int    `json:"envelope_version,omitempty"`
}

// AdvancedRestoreMetadata advertises advanced restore capabilities for a backup.
type AdvancedRestoreMetadata struct {
	Capabilities                  []string                    `json:"capabilities,omitempty"`
	InitialReleaseSupported       bool                        `json:"initial_release_supported,omitempty"`
	RecoverableWindowStartUTC     *time.Time                  `json:"recoverable_window_start_utc,omitempty"`
	RecoverableWindowEndUTC       *time.Time                  `json:"recoverable_window_end_utc,omitempty"`
	BaseBackupKind                string                      `json:"base_backup_kind,omitempty"`
	RequiresIntegrityVerification bool                        `json:"requires_integrity_verification,omitempty"`
	UnsupportedReason             string                      `json:"unsupported_reason,omitempty"`
	PostgresRecovery              *PostgresRecoveryMetadata   `json:"postgres_recovery,omitempty"`
	IncrementalLineage            *IncrementalLineageMetadata `json:"incremental_lineage,omitempty"`
}

// PostgresRecoveryMetadata stores WAL lineage and replay boundaries for PITR.
type PostgresRecoveryMetadata struct {
	TimelineID             string    `json:"timeline_id,omitempty"`
	WALStartLSN            string    `json:"wal_start_lsn,omitempty"`
	WALEndLSN              string    `json:"wal_end_lsn,omitempty"`
	BackupStartTimeUTC     time.Time `json:"backup_start_time_utc,omitempty"`
	BackupEndTimeUTC       time.Time `json:"backup_end_time_utc,omitempty"`
	WALArchivePrefix       string    `json:"wal_archive_prefix,omitempty"`
	WALSegments            []string  `json:"wal_segments,omitempty"`
	HistoryFiles           []string  `json:"history_files,omitempty"`
	RestoreCommandTemplate string    `json:"restore_command_template,omitempty"`
}

// IncrementalLineageMetadata stores compatibility and dependency details for incremental planning.
type IncrementalLineageMetadata struct {
	Enabled                     bool     `json:"enabled,omitempty"`
	ChainID                     string   `json:"chain_id,omitempty"`
	ChainIndex                  int      `json:"chain_index,omitempty"`
	MaxChainDepth               int      `json:"max_chain_depth,omitempty"`
	BaselineBackupID            string   `json:"baseline_backup_id,omitempty"`
	RequiredBackupIDs           []string `json:"required_backup_ids,omitempty"`
	CompatibleTargetFingerprint string   `json:"compatible_target_fingerprint,omitempty"`
	DeltaSizeBytes              int64    `json:"delta_size_bytes,omitempty"`
	FullBackupSizeBytes         int64    `json:"full_backup_size_bytes,omitempty"`
	CompressionRatio            float64  `json:"compression_ratio,omitempty"`
	Engine                      string   `json:"engine,omitempty"`
	TimelineID                  string   `json:"timeline_id,omitempty"`
	BinlogStartFile             string   `json:"binlog_start_file,omitempty"`
	BinlogStartPos              int64    `json:"binlog_start_pos,omitempty"`
	BinlogEndFile               string   `json:"binlog_end_file,omitempty"`
	BinlogEndPos                int64    `json:"binlog_end_pos,omitempty"`
	BinlogArtifacts             []string `json:"binlog_artifacts,omitempty"`
	OplogTSStart                string   `json:"oplog_ts_start,omitempty"`
	OplogTSEnd                  string   `json:"oplog_ts_end,omitempty"`
	OplogArtifactPath           string   `json:"oplog_artifact_path,omitempty"`
	ChecksumState               string   `json:"checksum_state,omitempty"`
	WALSummaryStartLSN          string   `json:"wal_summary_start_lsn,omitempty"`
	WALSummaryEndLSN            string   `json:"wal_summary_end_lsn,omitempty"`
	ExecutionSupported          bool     `json:"execution_supported,omitempty"`
}
