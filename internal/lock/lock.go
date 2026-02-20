package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrLockExists is returned when a lock file already exists for the job.
var ErrLockExists = errors.New("lock file already exists")

// Manager handles file-based job locking.
type Manager struct {
	dir string
}

// NewManager returns a Manager that stores lock files in dir.
// The directory is created if it does not exist.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// lockPath returns the full path for the named job's lock file.
func (m *Manager) lockPath(jobName string) string {
	return filepath.Join(m.dir, jobName+".lock")
}

// Acquire creates a lock file for jobName atomically.
// Returns ErrLockExists if a lock file already exists.
func (m *Manager) Acquire(jobName string) (*JobLock, error) {
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return nil, fmt.Errorf("lock: failed to create lock dir %q: %w", m.dir, err)
	}

	path := m.lockPath(jobName)

	hostname, _ := os.Hostname()
	jl := &JobLock{
		PID:       os.Getpid(),
		JobName:   jobName,
		StartTime: time.Now().UTC(),
		Hostname:  hostname,
	}

	data, err := json.Marshal(jl)
	if err != nil {
		return nil, fmt.Errorf("lock: failed to marshal lock: %w", err)
	}

	// O_CREATE|O_EXCL ensures atomicity — fails if file already exists
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrLockExists
		}
		return nil, fmt.Errorf("lock: failed to create lock file %q: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		os.Remove(path) // best-effort cleanup
		return nil, fmt.Errorf("lock: failed to write lock file: %w", err)
	}

	return jl, nil
}

// Release deletes the lock file for jobName.
func (m *Manager) Release(jobName string) error {
	path := m.lockPath(jobName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("lock: failed to release lock %q: %w", path, err)
	}
	return nil
}

// ReadLock reads the current lock file for jobName.
// Returns nil if no lock file exists.
func (m *Manager) ReadLock(jobName string) (*JobLock, error) {
	path := m.lockPath(jobName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("lock: failed to read lock %q: %w", path, err)
	}

	var jl JobLock
	if err := json.Unmarshal(data, &jl); err != nil {
		return nil, fmt.Errorf("lock: failed to parse lock %q: %w", path, err)
	}

	return &jl, nil
}

// CheckStale returns true when lock's process is dead AND the lock age exceeds threshold.
func CheckStale(jl *JobLock, threshold time.Duration) bool {
	if jl == nil {
		return false
	}

	// Check if process is alive using kill(pid, 0)
	proc, err := os.FindProcess(jl.PID)
	if err != nil {
		// Process not found → stale
		return time.Since(jl.StartTime) > threshold
	}

	// On Unix, Signal(0) checks process existence without sending a signal
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		// ESRCH: no such process → dead → check age
		return time.Since(jl.StartTime) > threshold
	}

	// Process is alive → not stale
	return false
}

// ListLockFiles returns all *.lock file paths in the lock directory.
func (m *Manager) ListLockFiles() ([]string, error) {
	pattern := filepath.Join(m.dir, "*.lock")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("lock: failed to list lock files: %w", err)
	}
	return matches, nil
}
