package monitor

import "time"

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
}

// RestoreExecution represents a single restore execution record.
type RestoreExecution struct {
	ID                 string
	RestoreName        string
	DatabaseType       string
	DatabaseName       string
	Timestamp          time.Time
	DurationMs         int64
	Status             string
	ErrorMessage       string
	SourceBackupPath   string
	BytesRestored      int64
	VerificationPassed bool
	CreatedAt          time.Time
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
