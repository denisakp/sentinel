package lock

import (
	"os"
	"syscall"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

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
func EvaluateLockState(jl *ports.JobLock, staleThreshold time.Duration) ports.LockState {
	if jl == nil {
		return ports.LockState{}
	}
	age := time.Since(jl.StartTime)
	if age < 0 {
		age = 0
	}
	live := isProcessAlive(jl.PID)
	removable := staleThreshold > 0 && age > staleThreshold && !live
	return ports.LockState{
		PID:       jl.PID,
		Hostname:  jl.Hostname,
		Age:       age,
		Live:      live,
		Removable: removable,
	}
}

// CheckStale is the legacy boolean helper. It reports true only when the
// dual criterion holds: PID is dead AND age exceeds threshold.
func CheckStale(jl *ports.JobLock, threshold time.Duration) bool {
	if jl == nil {
		return false
	}
	return EvaluateLockState(jl, threshold).Removable
}
