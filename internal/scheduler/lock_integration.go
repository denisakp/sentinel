package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/lock"
)

// ScanAndCleanStaleLocks scans lockDir for *.lock files and removes stale ones.
// A lock is stale when its PID is dead AND the lock age exceeds threshold.
// Foreign-host locks are warned about but not deleted (T046).
func ScanAndCleanStaleLocks(lockDir string, threshold time.Duration) {
	mgr := lock.NewManager(lockDir)
	files, err := mgr.ListLockFiles()
	if err != nil {
		slog.Warn("failed to list lock files during startup scan", "error", err)
		return
	}

	hostname := currentHostname()

	for _, path := range files {
		// Derive job name from file name
		base := filepath.Base(path)
		jobName := strings.TrimSuffix(base, ".lock")

		jl, err := mgr.ReadLock(jobName)
		if err != nil || jl == nil {
			continue
		}

		if jl.Hostname != hostname {
			// Foreign-host lock: warn but do not delete
			slog.Warn("foreign host lock detected — not cleaning",
				"event", "foreign_host_lock_detected",
				"job", jobName,
				"lock_host", jl.Hostname,
				"lock_pid", jl.PID,
			)
			continue
		}

		if lock.CheckStale(jl, threshold) {
			if releaseErr := mgr.Release(jobName); releaseErr != nil {
				slog.Warn("failed to release stale lock",
					"event", "stale_lock_release_failed",
					"job", jobName,
					"error", releaseErr,
				)
			} else {
				slog.Info("stale lock cleaned",
					"event", "stale_lock_cleaned",
					"job", jobName,
					"stale_pid", jl.PID,
				)
			}
		}
	}
}

// RunWithLock wraps fn with file-based lock acquisition and release.
// If the lock already exists (job already running), logs an info event and returns nil.
// If the lock cannot be acquired for another reason, returns an error.
// On success, defer-releases the lock after fn completes. (T045)
func RunWithLock(ctx context.Context, lockDir, jobName string, fn func() error) error {
	mgr := lock.NewManager(lockDir)
	_, err := mgr.Acquire(jobName)
	if err != nil {
		if errors.Is(err, lock.ErrLockExists) {
			slog.InfoContext(ctx, "job already running — skipping",
				"event", "job_already_running",
				"job", jobName,
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
// On cancellation, the caller's fn is expected to respect ctx; SIGKILL is sent
// by the subprocess layer when it detects cancellation. (T047)
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

// currentHostname returns the machine hostname, falling back to "unknown".
func currentHostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "unknown"
	}
	return h
}
