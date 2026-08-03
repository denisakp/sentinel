package monitor

import (
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

// Legacy status aliases for backward compatibility with existing data.
// These are normalized to canonical ports.Status* constants via NormalizeStatus
// when reading older SQLite records.
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
		return ports.StatusCompleted
	case LegacyStatusFailure:
		return ports.StatusFailed
	case LegacyStatusInProgress:
		return ports.StatusRunning
	default:
		return status // Already normalized or unknown
	}
}

// Statistics aggregates execution statistics for a backup job.
//
// This type is implementation-private to the monitor adapter — no port
// method returns it directly (it is computed inline by report-generation
// code in the CLI). Kept here rather than relocated to ports.
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
	LastExecution     *ports.Execution
	Trend             string
}

// Migration represents a single applied schema migration record.
//
// SQLite-specific bookkeeping — not part of the recorder port.
type Migration struct {
	Version   int
	Name      string
	Checksum  string
	AppliedAt time.Time
}

// MigrationStatus represents the current migration state for the database.
//
// SQLite-specific bookkeeping — not part of the recorder port.
type MigrationStatus struct {
	CurrentVersion         int
	LatestAvailableVersion int
	PendingVersions        []int
	AppliedMigrations      []Migration
	IsUpToDate             bool
}
