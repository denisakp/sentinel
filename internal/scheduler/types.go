package scheduler

import "time"

// JobInfo provides summary information for a scheduled job.
type JobInfo struct {
	Name           string
	ScheduleExpr   string
	NextExecution  time.Time
	LastExecution  time.Time
	LastStatus     string
	ExecutionCount int
}

// JobStatus provides detailed status information for a scheduled job.
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

// ExecutionRecord captures a single execution event.
type ExecutionRecord struct {
	Timestamp time.Time
	Duration  time.Duration
	Status    string
	Error     string
}

// Clock provides time control for scheduling and tests.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}
