package ports

import (
	"context"
	"time"

	"github.com/denisakp/sentinel/internal/domain/retention"
)

// Recorder abstracts *internal/adapters/monitor.Monitor (current concrete implementation).
//
// Implementations persist backup and restore execution history. The SQLite
// schema is implementation-private and stays in the adapter (see
// internal/adapters/monitor/migrate.go); only the read/write API surface is exposed
// here.
type Recorder interface {
	RecordExecution(ctx context.Context, exec *Execution) error
	RecordRunning(ctx context.Context, exec *Execution) error
	RecordSuccess(ctx context.Context, id string) error
	RecordFailure(ctx context.Context, id string, errorMsg string, cleanupAttempted bool, cleanupSucceeded *bool, cleanupError string) error
	RecordRestoreExecution(ctx context.Context, exec *RestoreExecution) error
	RecordSecurityInfo(ctx context.Context, id, hashAlgo, hashValue, plaintextHash, manifestPath string, encrypted bool, keyHint string) error
	RecordRetryCount(ctx context.Context, id string, retryCount int) error
	ListExecutions(ctx context.Context, filter *Filter, limit, offset int) ([]Execution, error)
	GetExecution(ctx context.Context, id string) (*Execution, error)
	ListRestoreExecutions(ctx context.Context, filter *RestoreFilter, limit, offset int) ([]RestoreExecution, error)

	// RetentionDeleteRecords deletes backup_executions rows matching the
	// given candidate file paths for the given backup job. Empty candidates
	// is a no-op success. Wraps SQL errors with "retention delete: %w".
	//
	// The retention.BackupCandidate / retention.Policy parameter types come
	// from internal/domain/retention — a deliberate ports → domain import
	// exception (domain owns the shapes; the port re-uses them as the single
	// source of truth).
	RetentionDeleteRecords(ctx context.Context, jobName string, candidates []retention.BackupCandidate) error

	// DeleteRestoreExecutions deletes restore_executions rows for the given
	// restore job according to the retention policy. When policy.DryRun is
	// true the implementation MUST count without deleting and return nil.
	// Wraps SQL errors with "delete restore executions: %w".
	DeleteRestoreExecutions(ctx context.Context, jobName string, policy retention.Policy) error

	// RecordIntegrityCheck persists one integrity sweep as a grouped run: all
	// per-artifact results in run.Results are written under a single
	// transaction, sharing run.RunID and run.Trigger. An
	// empty run.Results is a no-op success (an empty/recency-bounded repo is
	// not a failure). Implementations enforce the result and trigger
	// vocabularies at the store boundary (a value outside the allowed set is
	// rejected). Wraps SQL errors with "record integrity check: %w".
	RecordIntegrityCheck(ctx context.Context, run IntegrityRun) error
}

// IntegrityRun is one integrity sweep's durable audit unit: an identifier
// grouping its per-artifact results plus what triggered it.
type IntegrityRun struct {
	// RunID groups every IntegrityResult produced by a single sweep.
	RunID string
	// Trigger is what invoked the sweep: "manual" or "scheduled".
	Trigger string
	// Results is the per-artifact outcome collection (one row per backup).
	Results []IntegrityResult
}

// IntegrityResult is the forensic record of a single artifact's health at a
// point in time within an IntegrityRun.
type IntegrityResult struct {
	// BackupID is the recorded backup execution id this result verifies.
	BackupID string
	// Job is the backup job name the artifact belongs to.
	Job string
	// Result is the outcome: one of "ok", "corrupted", "missing_artifact",
	// "missing_manifest".
	Result string
	// StoredHash is the manifest-recorded fingerprint (may be empty).
	StoredHash string
	// ComputedHash is the freshly re-computed fingerprint (may be empty).
	ComputedHash string
	// StorageBackend is the backend the artifact lives on (local/s3/…).
	StorageBackend string
	// ArtifactPath is the recorded artifact reference (path or object key).
	ArtifactPath string
	// CheckedAt is when this artifact was verified.
	CheckedAt time.Time
}

// Integrity result vocabulary — the four states an integrity check can
// record. Mirrors the CHECK constraint on integrity_checks.result
// and the verifyStatus* constants in the CLI sweep.
const (
	IntegrityResultOK              = "ok"
	IntegrityResultCorrupted       = "corrupted"
	IntegrityResultMissingArtifact = "missing_artifact"
	IntegrityResultMissingManifest = "missing_manifest"
)

// Integrity trigger vocabulary — what invoked a sweep.
// Mirrors the CHECK constraint on integrity_checks.trigger.
const (
	IntegrityTriggerManual    = "manual"
	IntegrityTriggerScheduled = "scheduled"
)

// Execution status constants — relocated from internal/monitor/types.go.
// Bare string constants (not a typed enum) to minimise churn in this slice;
// promoting to `type Status string` is deferred to a later cleanup if
// callers want type safety.
const (
	StatusPending     = "pending"
	StatusRunning     = "running"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
	StatusSuccess     = "success"
	StatusTimeout     = "timeout"
	StatusSkipped     = "skipped"
)

// Execution represents a single backup execution record.
//
// Relocated from internal/monitor/types.go (single source of truth).
type Execution struct {
	ID             string
	BackupName     string
	DatabaseType   string
	Timestamp      time.Time
	DurationMs     int64
	Status         string
	ErrorMessage   string
	StorageBackend string
	FilePath       string
	FileSizeBytes  int64
	Checksum       string
	CreatedAt      time.Time
	// V1 Consolidation: Cleanup and interruption fields
	FinishedAt       *time.Time
	CleanupAttempted bool
	CleanupSucceeded *bool
	CleanupError     string
	UpdatedAt        time.Time
	// V1.1.0: Security and integrity fields (migration 004)
	HashAlgorithm       string
	HashValue           string
	PlaintextHashValue  string
	Encrypted           bool
	EncryptionKeyHint   string
	ManifestPath        string
	RetryCount          int
	BackupType          string
	ChainID             string
	ChainIndex          int
	DeltaSizeBytes      int64
	FullBackupSizeBytes int64
}

// RestoreExecution represents a single restore execution record.
type RestoreExecution struct {
	ID                   string
	RestoreName          string
	DatabaseType         string
	DatabaseName         string
	RestoreMode          string
	PlanningStatus       string
	RequestedPITRTimeUTC *time.Time
	BaselineBackupID     string
	FallbackDecision     string
	FallbackReason       string
	FallbackBackupID     string
	RecoveryTimelineID   string
	SourceType           string
	ConflictStrategy     string
	Timestamp            time.Time
	DurationMs           int64
	Status               string
	ErrorMessage         string
	ErrorReason          string
	Reason               string
	SourceBackupPath     string
	StagedFilePath       string
	StagedFileRetained   bool
	BytesRestored        int64
	VerificationPassed   bool
	TimeoutSeconds       int
	CreatedAt            time.Time
	// V1 Consolidation: Cleanup and interruption fields
	FinishedAt         *time.Time
	CleanupAttempted   bool
	CleanupSucceeded   *bool
	CleanupError       string
	UpdatedAt          time.Time
	ChainDepth         int
	ChainID            string
	AssemblyDurationMs int64
}

// Filter specifies query filters for listing executions.
type Filter struct {
	BackupName     string
	Status         string
	DatabaseType   string
	StorageBackend string
	StartDate      time.Time
	EndDate        time.Time
}

// RestoreFilter specifies query filters for listing restore executions.
type RestoreFilter struct {
	RestoreName  string
	Status       string
	DatabaseType string
	StartDate    time.Time
	EndDate      time.Time
}
