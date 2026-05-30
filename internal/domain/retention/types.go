package retention

import "time"

// Policy defines retention rules for backups. Pure data.
type Policy struct {
	KeepLast int
	KeepDays int
	DryRun   bool
}

// BackupRecord represents a backup execution record used for retention evaluation.
type BackupRecord struct {
	FilePath   string
	Timestamp  time.Time
	FileSize   int64
	Status     string
	BackupType string
	ChainID    string
	ChainIndex int
}

// BackupCandidate represents a retention deletion candidate.
type BackupCandidate struct {
	FilePath      string
	Timestamp     time.Time
	FileSize      int64
	Status        string
	ReasonDeleted string
	BackupType    string
	ChainID       string
	ChainIndex    int
}

// DeletedBackup represents a deletion result.
type DeletedBackup struct {
	FilePath      string
	FileSize      int64
	DeletionTime  time.Time
	ReasonDeleted string
}

// ApplySummary summarizes retention actions across one or more jobs.
type ApplySummary struct {
	TotalDeleted int64
	TotalSize    int64
	ByBackupJob  map[string]int
	Errors       []string
}

// ReasonProtectedActiveBaseline is the marker reason for a candidate kept by
// the active-baseline safety filter. Driving adapters MUST skip deletion of
// candidates whose ReasonDeleted contains this marker.
const ReasonProtectedActiveBaseline = "protected active baseline"
