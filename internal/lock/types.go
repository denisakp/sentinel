package lock

import "time"

// JobLock represents the content of a file-based job lock.
// It is serialised as JSON in the lock file.
type JobLock struct {
	PID       int               `json:"pid"`
	JobName   string            `json:"job_name"`
	StartTime time.Time         `json:"start_time"`
	Hostname  string            `json:"hostname"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
