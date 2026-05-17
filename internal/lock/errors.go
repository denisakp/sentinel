package lock

import (
	"errors"
	"fmt"
	"time"
)

// ErrLockHeld is returned when the lock is owned by another holder
// (either a live process, or a non-stale recorded holder, or an
// uncontended flock).
var ErrLockHeld = errors.New("lock: held by another holder")

// ErrLockUnsupported is returned when the underlying filesystem does
// not support advisory file locks (e.g. some NFS configurations).
var ErrLockUnsupported = errors.New("lock: advisory locks not supported on this filesystem")

// ErrLockIO wraps filesystem errors that are not held/unsupported.
var ErrLockIO = errors.New("lock: filesystem error")

// ErrUnsupportedPlatform is returned when the lock package is built
// for a non-POSIX target. Production builds should fail at compile time.
var ErrUnsupportedPlatform = errors.New("lock: unsupported platform (linux or darwin required)")

// ErrLockExists is the pre-existing sentinel name for "another holder
// already has the lock". Kept as an alias of ErrLockHeld so older
// callers using errors.Is(err, lock.ErrLockExists) continue to work.
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
		e.State.PID, e.State.Hostname, truncDur(e.State.Age), e.State.Live)
}

func (e *HeldError) Unwrap() error { return ErrLockHeld }

func truncDur(d time.Duration) time.Duration { return d.Truncate(time.Second) }
