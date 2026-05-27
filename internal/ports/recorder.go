package ports

import (
	"context"
	"time"
)

// Recorder abstracts *internal/monitor.Monitor (current concrete implementation).
//
// Implementations persist backup and restore execution history. The SQLite
// schema is implementation-private and stays in the adapter (see
// internal/monitor/migrate.go); only the read/write API surface is exposed
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
}

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
// Relocated from internal/monitor/types.go (single source of truth per spec
// 028 FR-003a).
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
