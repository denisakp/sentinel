package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/storage"
)

// Executor runs jobs with bounded concurrency.
type Executor struct {
	limit  chan struct{}
	wg     sync.WaitGroup
	logger *slog.Logger
}

// NewExecutor constructs a bounded executor using slog.Default().
func NewExecutor(maxConcurrent int) *Executor {
	return NewExecutorWithLogger(maxConcurrent, slog.Default())
}

// NewExecutorWithLogger constructs a bounded executor with a custom logger,
// used by tests to capture panic-recovery log records.
func NewExecutorWithLogger(maxConcurrent int, logger *slog.Logger) *Executor {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		limit:  make(chan struct{}, maxConcurrent),
		logger: logger,
	}
}

// Execute runs a job with concurrency control. A panic inside job is recovered
// by an outer safety-net defer that logs the panic + stack trace. The deferred
// slot release runs before wg.Done so any caller blocked on Wait observes
// restored capacity. (FR-001, FR-002, FR-005, FR-007)
func (e *Executor) Execute(job func()) {
	e.wg.Add(1)
	go func() {
		e.limit <- struct{}{}
		// Declared first → runs last: signals completion after slot release.
		defer e.wg.Done()
		// Declared second → runs middle: returns slot to the pool.
		defer func() { <-e.limit }()
		// Declared third → runs first: catches any in-flight panic.
		defer func() {
			if pErr, stack := handlePanic(recover()); pErr != nil {
				e.logger.Error("scheduler: unrecovered panic in worker",
					"error", pErr,
					"stack", string(stack),
				)
			}
		}()
		job()
	}()
}

// Wait blocks until all jobs complete.
func (e *Executor) Wait() {
	e.wg.Wait()
}

// BackupExecutionResult captures the outcome of a backup operation.
type BackupExecutionResult struct {
	Success       bool
	ExecutionID   string
	BackupPath    string
	Error         error
	CleanupNeeded bool
}

// ExecuteBackupWithCleanup wraps backup execution with automatic cleanup on failure.
// This ensures partial artifacts are deleted if the backup fails or is interrupted.
//
// A panic inside backupFn is converted to result.Error via wrapPanic so the
// existing cleanup defer records it as an ordinary failure (FR-002, FR-004).
// If executionID is empty when the panic fires, a start record is synthesized
// (Q4) so every failure has a matching start.
func ExecuteBackupWithCleanup(
	ctx context.Context,
	executionID string,
	backupPath string,
	store storage.Storage,
	mon *monitor.Monitor,
	backupFn func(context.Context) error,
) *BackupExecutionResult {
	return ExecuteBackupWithCleanupContext(ctx, executionID, backupPath, "", "", store, mon, backupFn)
}

// ExecuteBackupWithCleanupContext is like ExecuteBackupWithCleanup but accepts
// jobName/database to populate a synthesized start record when a panic fires
// before run-start was registered.
func ExecuteBackupWithCleanupContext(
	ctx context.Context,
	executionID string,
	backupPath string,
	jobName string,
	database string,
	store storage.Storage,
	mon *monitor.Monitor,
	backupFn func(context.Context) error,
) (result *BackupExecutionResult) {
	result = &BackupExecutionResult{
		ExecutionID: executionID,
		BackupPath:  backupPath,
	}

	var cleanupAttempted bool
	var cleanupSucceeded *bool
	var cleanupError string

	// Cleanup defer — declared first, runs LAST so it observes result.Error
	// set either by ordinary error return or by the inner-recover below.
	// Body is wrapped in defer-recover so a panic *during* monitor recording
	// itself is best-effort logged and does not propagate.
	defer func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Default().Error("scheduler: panic during backup cleanup recording",
					"error", fmt.Sprintf("%v", r),
				)
			}
		}()
		execID := result.ExecutionID
		if result.Error != nil && backupPath != "" {
			cleanupAttempted = true
			if cleanupErr := store.DeleteBackup(ctx, backupPath); cleanupErr != nil {
				succeeded := false
				cleanupSucceeded = &succeeded
				cleanupError = cleanupErr.Error()
			} else {
				succeeded := true
				cleanupSucceeded = &succeeded
			}
		}

		if result.Error != nil && mon != nil && execID != "" {
			errorMsg := result.Error.Error()
			if recordErr := mon.RecordFailure(ctx, execID, errorMsg, cleanupAttempted, cleanupSucceeded, cleanupError); recordErr != nil {
				slog.Default().Warn("failed to record execution failure", "error", recordErr)
			}
		} else if result.Success && mon != nil && execID != "" {
			if recordErr := mon.RecordSuccess(ctx, execID); recordErr != nil {
				slog.Default().Warn("failed to record execution success", "error", recordErr)
			}
		}
	}()

	// Inner-recover — declared second, runs FIRST (before cleanup defer).
	// Converts a panic into result.Error so cleanup proceeds normally.
	defer func() {
		if pErr, stack := handlePanic(recover()); pErr != nil {
			result.Error = pErr
			slog.Default().Error("scheduler: backup worker panic",
				"error", pErr,
				"stack", string(stack),
				"job", jobName,
				"database", database,
			)
			// If start was never registered, synthesize one so the failure
			// record has a matching start row (Q4).
			if result.ExecutionID == "" && mon != nil {
				if id, serr := synthesizeRunStart(ctx, mon, jobName, database); serr == nil {
					result.ExecutionID = id
				} else {
					slog.Default().Warn("failed to synthesize start record", "error", serr)
				}
			}
		}
	}()

	if err := backupFn(ctx); err != nil {
		result.Error = err
		result.CleanupNeeded = true
		return result
	}

	result.Success = true
	return result
}

// synthesizeRunStart persists a "running" record for an execution that
// panicked before its start could be recorded. Used by *WithCleanup helpers.
func synthesizeRunStart(ctx context.Context, mon *monitor.Monitor, jobName, database string) (string, error) {
	exec := &ports.Execution{
		BackupName:   jobName,
		DatabaseType: database,
		Timestamp:    time.Now().UTC(),
	}
	if err := mon.RecordRunning(ctx, exec); err != nil {
		return "", err
	}
	return exec.ID, nil
}

// withRetry executes fn up to maxAttempts times with the given backoffs between attempts.
// Non-retriable errors (config errors, cert errors) cause immediate failure without retry.
func withRetry(fn func() error, maxAttempts int, backoffs []time.Duration) error {
	var lastErr error
	for attempt := range maxAttempts {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		// Non-retriable errors skip remaining attempts
		if isNonRetriable(lastErr) {
			return lastErr
		}
		if attempt < len(backoffs) {
			time.Sleep(backoffs[attempt])
		}
	}
	return lastErr
}

// isNonRetriable returns true for errors that should not be retried:
// configuration errors, certificate errors, and authentication errors.
func isNonRetriable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "certificate") ||
		strings.Contains(msg, "tls") ||
		strings.Contains(msg, "auth") ||
		strings.Contains(msg, "config")
}
