package monitor

import "time"

// Execution status constants for backup/restore lifecycle.
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

// Legacy status aliases for backward compatibility with existing data.
const (
	LegacyStatusSuccess    = "success"
	LegacyStatusFailure    = "failure"
	LegacyStatusInProgress = "in-progress"
)

// NormalizeStatus converts legacy status values to canonical form.
// This preserves compatibility with historical records while standardizing new writes.
func NormalizeStatus(status string) string {
	switch status {
	case LegacyStatusSuccess:
		return StatusCompleted
	case LegacyStatusFailure:
		return StatusFailed
	case LegacyStatusInProgress:
		return StatusRunning
	default:
		return status // Already normalized or unknown
	}
}

// Execution represents a single backup execution record.
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
	HashAlgorithm      string
	HashValue          string
	PlaintextHashValue string
	Encrypted          bool
	EncryptionKeyHint  string
	ManifestPath       string
	RetryCount         int
}

// RestoreExecution represents a single restore execution record.
type RestoreExecution struct {
	ID                 string
	RestoreName        string
	DatabaseType       string
	DatabaseName       string
	SourceType         string
	ConflictStrategy   string
	Timestamp          time.Time
	DurationMs         int64
	Status             string
	ErrorMessage       string
	ErrorReason        string
	Reason             string
	SourceBackupPath   string
	StagedFilePath     string
	StagedFileRetained bool
	BytesRestored      int64
	VerificationPassed bool
	TimeoutSeconds     int
	CreatedAt          time.Time
	// V1 Consolidation: Cleanup and interruption fields
	FinishedAt       *time.Time
	CleanupAttempted bool
	CleanupSucceeded *bool
	CleanupError     string
	UpdatedAt        time.Time
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

// Statistics aggregates execution statistics for a backup job.
type Statistics struct {
	BackupName        string
	JobsPeriod        string
	TotalExecutions   int
	SuccessCount      int
	FailureCount      int
	SuccessRate       float32
	TotalBackupSize   int64
	AverageDurationMs int64
	MedianDurationMs  int64
	MinDurationMs     int64
	MaxDurationMs     int64
	LastExecution     *Execution
	Trend             string
}

// Migration represents a single applied schema migration record.
type Migration struct {
	Version   int
	Name      string
	Checksum  string
	AppliedAt time.Time
}

// MigrationStatus represents the current migration state for the database.
type MigrationStatus struct {
	CurrentVersion         int
	LatestAvailableVersion int
	PendingVersions        []int
	AppliedMigrations      []Migration
	IsUpToDate             bool
}
