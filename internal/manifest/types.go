package manifest

import "time"

// BackupManifest is the single source of truth for backup integrity and restore metadata.
// Written as <backup-filename>.manifest.json alongside the backup file.
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
	Algorithm     string `json:"algorithm"`
	KeyDerivation string `json:"key_derivation"`
	Iterations    int    `json:"iterations"`
	Salt          string `json:"salt"`
	IV            string `json:"iv"`
	AuthTag       string `json:"auth_tag"`
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
	BaselineBackupID            string   `json:"baseline_backup_id,omitempty"`
	RequiredBackupIDs           []string `json:"required_backup_ids,omitempty"`
	CompatibleTargetFingerprint string   `json:"compatible_target_fingerprint,omitempty"`
	ChecksumState               string   `json:"checksum_state,omitempty"`
	WALSummaryStartLSN          string   `json:"wal_summary_start_lsn,omitempty"`
	WALSummaryEndLSN            string   `json:"wal_summary_end_lsn,omitempty"`
	ExecutionSupported          bool     `json:"execution_supported,omitempty"`
}
