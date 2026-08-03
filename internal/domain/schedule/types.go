package schedule

// Relocated from internal/scheduler/types.go. Pure: stdlib only.

import "time"

// JobInfo summarises a registered scheduled job for listing / display use.
type JobInfo struct {
	Name           string
	ScheduleExpr   string
	NextExecution  time.Time
	LastExecution  time.Time
	LastStatus     string
	ExecutionCount int
}

// JobStatus is the detailed snapshot of one scheduled job, including its
// recent execution history.
type JobStatus struct {
	Name             string
	Schedule         string
	Enabled          bool
	NextExecution    time.Time
	LastExecution    time.Time
	LastStatus       string
	LastError        string
	ExecutionHistory []ExecutionRecord
}

// ExecutionRecord is one entry in a JobStatus.ExecutionHistory ring.
type ExecutionRecord struct {
	Timestamp time.Time
	Duration  time.Duration
	Status    string
	Error     string
}
