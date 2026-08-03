package ports

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// LockManager abstracts *internal/lock.Manager (current concrete implementation).
//
// Implementations coordinate file-based advisory locks across processes.
// Acquire methods return a *JobLock describing the holder; Release frees
// the lock. Inspect/ListLockFiles/ScanStale support the stale-lock recovery
// flow documented in docs/runbooks/.
type LockManager interface {
	TryAcquire(jobName string, staleThreshold time.Duration) (*JobLock, error)
	AcquireContext(ctx context.Context, jobName string, staleThreshold time.Duration) (*JobLock, error)
	AcquireWithTimeout(ctx context.Context, jobName string, staleThreshold, timeout time.Duration) (*JobLock, error)
	Release(jobName string) error
	ReadLock(jobName string) (*JobLock, error)
	Inspect(jobName string, staleThreshold time.Duration) (*LockState, error)
	ListLockFiles() ([]string, error)
	ScanStale(staleThreshold time.Duration) (removed []string, err error)
}

// JobLock represents the content of a file-based job lock.
// It is serialised as JSON in the lock file.
//
// Relocated from internal/lock/types.go (single source of truth).
type JobLock struct {
	PID       int               `json:"pid"`
	JobName   string            `json:"job_name"`
	StartTime time.Time         `json:"start_time"`
	Hostname  string            `json:"hostname"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// LockState is the runtime view of a lock at evaluation time.
//
// Callers should branch on Removable rather than recomputing from Age and
// Live, so that the staleness rule the package used at evaluation is the
// same rule the caller acts on.
//
// Relocated from internal/lock/state.go (single source of truth).
type LockState struct {
	PID       int
	Hostname  string
	Age       time.Duration
	Live      bool
	Removable bool
}

// Lock sentinel errors — relocated from internal/lock/errors.go.

// ErrLockHeld is returned when the lock is owned by another holder
// (either a live process, or a non-stale recorded holder, or an
// uncontended flock).
var ErrLockHeld = errors.New("lock: held by another holder")

// ErrLockUnsupported is returned when the underlying filesystem does
// not support advisory file locks (e.g. some NFS configurations).
var ErrLockUnsupported = errors.New("lock: advisory locks not supported on this filesystem")

// ErrLockIO wraps filesystem errors that are not held/unsupported.
var ErrLockIO = errors.New("lock: filesystem error")

// ErrLockExists is the pre-existing sentinel name for "another holder
// already has the lock". Kept as an alias of ErrLockHeld so older
// callers using errors.Is(err, ports.ErrLockExists) continue to work.
//
// Deprecated: use ErrLockHeld for new code.
var ErrLockExists = ErrLockHeld

// HeldError wraps ErrLockHeld with the current LockState so callers
// (and logs) can describe the holder without re-reading the file.
type HeldError struct {
	State LockState
}

func (e *HeldError) Error() string {
	return fmt.Sprintf("lock: held by pid=%d host=%q age=%s (live=%t)",
		e.State.PID, e.State.Hostname, e.State.Age.Truncate(time.Second), e.State.Live)
}

func (e *HeldError) Unwrap() error { return ErrLockHeld }
