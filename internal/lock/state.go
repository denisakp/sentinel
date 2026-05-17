package lock

import (
	"os"
	"syscall"
	"time"
)

// LockState is the runtime view of a lock at evaluation time.
//
// Callers should branch on Removable rather than recomputing from Age and
// Live, so that the staleness rule the package used at evaluation is the
// same rule the caller acts on.
type LockState struct {
	PID       int
	Hostname  string
	Age       time.Duration
	Live      bool
	Removable bool
}

// isProcessAlive probes whether pid corresponds to a live process via
// signal(0). It is a package-level var so tests can swap it.
var isProcessAlive = func(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

// EvaluateLockState computes a LockState from a parsed JobLock and a
// stale threshold. Negative ages (clock skew) clamp to 0. A non-positive
// threshold disables auto-stale evaluation (Removable=false).
func EvaluateLockState(jl *JobLock, staleThreshold time.Duration) LockState {
	if jl == nil {
		return LockState{}
	}
	age := time.Since(jl.StartTime)
	if age < 0 {
		age = 0
	}
	live := isProcessAlive(jl.PID)
	removable := staleThreshold > 0 && age > staleThreshold && !live
	return LockState{
		PID:       jl.PID,
		Hostname:  jl.Hostname,
		Age:       age,
		Live:      live,
		Removable: removable,
	}
}

// CheckStale is the legacy boolean helper. It reports true only when the
// dual criterion holds: PID is dead AND age exceeds threshold.
func CheckStale(jl *JobLock, threshold time.Duration) bool {
	if jl == nil {
		return false
	}
	return EvaluateLockState(jl, threshold).Removable
}
