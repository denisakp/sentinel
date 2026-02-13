package retention

import "time"

// Policy defines retention rules for backups.
type Policy struct {
	KeepLast int
	KeepDays int
	DryRun   bool
}

// BackupRecord represents a backup execution record used for retention evaluation.
type BackupRecord struct {
	FilePath  string
	Timestamp time.Time
	FileSize  int64
	Status    string
}

// BackupCandidate represents a retention deletion candidate.
type BackupCandidate struct {
	FilePath      string
	Timestamp     time.Time
	FileSize      int64
	Status        string
	ReasonDeleted string
}

// DeletedBackup represents a deletion result.
type DeletedBackup struct {
	FilePath      string
	FileSize      int64
	DeletionTime  time.Time
	ReasonDeleted string
}

// ApplySummary summarizes retention actions.
type ApplySummary struct {
	TotalDeleted int64
	TotalSize    int64
	ByBackupJob  map[string]int
	Errors       []string
}
