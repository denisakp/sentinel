package backup

// Job + result types for the backup Executor (spec 038 Sub-PR K, FR-008).
// Pure data: ports + sibling domain + stdlib only. Adapter-backed behavior
// (encryption, binlog/oplog archival, hash verification seams) reaches the
// Executor exclusively through function-valued hooks wired by the driving
// factory (internal/cli/backup_factory.go).

import (
	"context"
	"errors"
	"time"

	"github.com/denisakp/sentinel/internal/domain/retention"
	"github.com/denisakp/sentinel/internal/ports"
)

// Job is the pure-data descriptor of one backup execution.
type Job struct {
	Name     string
	Engine   string // postgres | mysql | mariadb | mongodb
	Database string

	// DBConn feeds the optional connectivity gate (PingBeforeDump) and SQL
	// source enumeration through ports.DBProber.
	DBConn         ports.DatabaseConfig
	PingBeforeDump bool

	// Options is the engine-specific dump argument bag handed verbatim to
	// ports.DumpBuilder.Build. Opaque to the domain (marker interface).
	Options ports.EngineOptions

	// Artifact location knowledge for the post-dump pipeline (manifest,
	// encryption, history rows). Mirrors the driving adapter's storage
	// params after CLI overrides + scheduled-name resolution.
	StorageType string
	LocalPath   string
	OutName     string
	GCSBucket   string

	// StagingDir is set by the driving factory for REMOTE backups (spec 047):
	// the dump is redirected to write a real local artifact here so the domain
	// pipeline can hash/encrypt/manifest it before the Executor uploads the
	// (encrypted) artifact + manifest sidecar to the remote backend. The
	// Executor removes this directory on both success and failure. Empty for
	// local storage (no staging, no upload).
	StagingDir string

	// Incremental configuration, normalized by the driving adapter.
	IncrementalEnabled bool
	MaxChainDepth      int
	ForceFull          bool

	Scheduled bool

	Retention retention.Policy

	// EncryptionKeyHint is recorded on history rows when encryption fires
	// (the env var name, never the key).
	EncryptionKeyHint string

	// EncryptArtifact encrypts the artifact at path in place and returns
	// (encrypted, envelope info, ciphertext hash). nil = plaintext config.
	EncryptArtifact EncryptArtifactFunc

	// ArchiveBinlogs / ArchiveOplog capture engine incremental artifacts
	// next to the dump. Wired only for engines that need them.
	ArchiveBinlogs ArchiveFunc
	ArchiveOplog   ArchiveFunc

	// VerifyArtifactHash overrides the incremental artifact hash check
	// (test seam). nil → ports.ManifestStore.VerifyHash.
	VerifyArtifactHash func(path, algorithm, expected string) error
}

// EncryptArtifactFunc is the in-place artifact encryption hook.
type EncryptArtifactFunc func(path, backupID string) (encrypted bool, info *ports.EncryptionInfo, encryptedHash string, err error)

// ArchiveFunc captures incremental side artifacts for the given dump path.
type ArchiveFunc func(ctx context.Context, artifactPath string) (IncrementalArtifacts, error)

// IncrementalArtifacts is the outcome of an ArchiveFunc.
type IncrementalArtifacts struct {
	BinlogStartFile   string
	BinlogEndFile     string
	BinlogArtifacts   []string
	OplogArtifactPath string
}

// IncrementalContext mirrors the manifest lineage + history-row incremental
// fields. Relocated from internal/cli/backup.go::backupIncrementalContext.
type IncrementalContext struct {
	Enabled             bool
	BackupType          string
	ChainID             string
	ChainIndex          int
	MaxChainDepth       int
	BaselineBackupID    string
	RequiredBackupIDs   []string
	DeltaSizeBytes      int64
	FullBackupSizeBytes int64
	CompressionRatio    float64
	BinlogStartFile     string
	BinlogEndFile       string
	BinlogArtifacts     []string
	OplogArtifactPath   string
}

// SecurityOutcome holds hash, manifest, and encryption metadata produced by
// the post-dump pipeline. Relocated from
// internal/cli/backup.go::backupSecurityResult (fields exported).
type SecurityOutcome struct {
	HashAlgorithm string
	HashValue     string
	ManifestPath  string
	Encrypted     bool
	KeyHint       string
	BackupType    string
	ChainID       string
	ChainIndex    int
	DeltaSize     int64
	FullSize      int64
}

// RunResult is the outcome of Executor.Run.
type RunResult struct {
	Digest       string
	ArtifactPath string
	ArtifactSize int64
	ManifestPath string
	Encrypted    bool
	HashValue    string

	// Skipped is true when the job lock was held by another live holder.
	Skipped bool

	// Non-fatal post-run signals, surfaced for the driving adapter to print
	// (parity with the pre-carve CLI warning output).
	RecordErr   error
	NotifyErr   error
	Warnings    []string
	StartedAt   time.Time
	FinishedAt  time.Time
	Incremental IncrementalContext
}

// RetriableErr wraps connectivity-blip errors classified as retriable; the
// scheduler runtime loops on IsRetriable.
type RetriableErr struct{ Err error }

func (e *RetriableErr) Error() string { return e.Err.Error() }
func (e *RetriableErr) Unwrap() error { return e.Err }

// IsRetriable reports whether err (or anything it wraps) is a RetriableErr.
func IsRetriable(err error) bool {
	var r *RetriableErr
	return errors.As(err, &r)
}
