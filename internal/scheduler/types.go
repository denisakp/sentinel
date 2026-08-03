// Runtime time source for the scheduler. Schedule reporting types
// (JobInfo, JobStatus, ExecutionRecord) live in internal/domain/schedule;
// this file no longer re-exports them.
package scheduler

import "time"

// Clock abstracts the time source used by the scheduler runtime so tests can
// inject a deterministic clock.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}
