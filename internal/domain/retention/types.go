package retention

import "time"

// Policy defines retention rules for backups. Pure data.
type Policy struct {
	KeepLast int
	KeepDays int
	DryRun   bool
	// GFS, when non-nil and non-zero, adds Grandfather-Father-Son calendar-tier
	// retention on top of the flat KeepLast/KeepDays rules. A backup is kept if
	// ANY configured rule (flat or GFS) keeps it (union of keeps); it becomes a
	// deletion candidate only when every configured rule would discard it.
	GFS *GFSPolicy
}

// GFSPolicy expresses Grandfather-Father-Son calendar-tier retention. Each field
// is a count of the most-recent occupied calendar buckets to retain the newest
// backup of. Zero (or negative) disables that tier. All bucketing is computed in
// UTC. Pure data.
type GFSPolicy struct {
	KeepDaily   int // newest backup of each of the last N calendar days
	KeepWeekly  int // newest backup of each of the last N ISO weeks (Mon–Sun)
	KeepMonthly int // newest backup of each of the last N calendar months
	KeepYearly  int // newest backup of each of the last N calendar years
}

// IsZero reports whether the policy retains nothing (all tier counts <= 0).
func (g *GFSPolicy) IsZero() bool {
	if g == nil {
		return true
	}
	return g.KeepDaily <= 0 && g.KeepWeekly <= 0 && g.KeepMonthly <= 0 && g.KeepYearly <= 0
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

// ReasonNotRetainedByGFS is the reason recorded for a candidate deleted because
// it anchors zero GFS buckets (aggregate reason — a GFS deletion candidate is by
// definition outside every configured tier, so per-tier attribution adds nothing).
const ReasonNotRetainedByGFS = "not retained by gfs"
