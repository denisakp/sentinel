package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/denisakp/sentinel/internal/lock"
	"github.com/denisakp/sentinel/internal/ports"
)

// ScanAndCleanStaleLocks delegates to lock.Manager.ScanStale, which applies
// the same dual-criterion + advisory-flock evaluator as the runtime acquire
// path. Foreign-host locks are preserved by the package itself.
func ScanAndCleanStaleLocks(lockDir string, threshold time.Duration) {
	mgr := lock.NewManager(lockDir)
	if _, err := mgr.ScanStale(threshold); err != nil {
		slog.Warn("stale lock scan failed",
			"event", "stale_lock_scan_failed",
			"error", err,
		)
	}
}

// RunWithLock wraps fn with file-based lock acquisition and release.
// If the lock is held by another live holder (or non-stale recorded
// holder), logs `event=job_already_running` and returns nil.
// Other errors propagate. On success defer-releases the lock.
func RunWithLock(ctx context.Context, lockDir, jobName string, fn func() error) error {
	mgr := lock.NewManager(lockDir)
	_, err := mgr.TryAcquire(jobName, 0)
	if err != nil {
		var he *ports.HeldError
		if errors.As(err, &he) {
			slog.InfoContext(ctx, "job already running — skipping",
				"event", "job_already_running",
				"job", jobName,
				"holder_pid", he.State.PID,
			)
			return nil
		}
		return err
	}
	defer func() {
		if releaseErr := mgr.Release(jobName); releaseErr != nil {
			slog.WarnContext(ctx, "failed to release job lock",
				"event", "lock_release_failed",
				"job", jobName,
				"error", releaseErr,
			)
		}
	}()
	return fn()
}

// RunWithTimeout wraps fn with a per-job context deadline.
// On cancellation, the caller's fn is expected to respect ctx; SIGKILL is
// sent by the subprocess layer when it detects cancellation.
func RunWithTimeout(
	ctx context.Context,
	timeoutMinutes int,
	fn func(context.Context) error,
) error {
	if timeoutMinutes <= 0 {
		return fn(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMinutes)*time.Minute)
	defer cancel()
	return fn(ctx)
}
