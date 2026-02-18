package scheduler

import (
	"context"
	"fmt"
	"sync"

	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/storage"
)

// Executor runs jobs with bounded concurrency.
type Executor struct {
	limit chan struct{}
	wg    sync.WaitGroup
}

// NewExecutor constructs a bounded executor.
func NewExecutor(maxConcurrent int) *Executor {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Executor{
		limit: make(chan struct{}, maxConcurrent),
	}
}

// Execute runs a job with concurrency control.
func (e *Executor) Execute(job func()) {
	e.wg.Add(1)
	go func() {
		e.limit <- struct{}{}
		defer func() {
			<-e.limit
			e.wg.Done()
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
func ExecuteBackupWithCleanup(
	ctx context.Context,
	executionID string,
	backupPath string,
	store storage.Storage,
	mon *monitor.Monitor,
	backupFn func(context.Context) error,
) *BackupExecutionResult {
	result := &BackupExecutionResult{
		ExecutionID: executionID,
		BackupPath:  backupPath,
	}

	// Track whether cleanup should be attempted
	var cleanupAttempted bool
	var cleanupSucceeded *bool
	var cleanupError string

	// Defer cleanup handler - executes regardless of success/failure
	defer func() {
		if result.Error != nil && backupPath != "" {
			// Backup failed - attempt cleanup of partial artifacts
			cleanupAttempted = true
			if cleanupErr := store.DeleteBackup(ctx, backupPath); cleanupErr != nil {
				// Cleanup failed
				succeeded := false
				cleanupSucceeded = &succeeded
				cleanupError = cleanupErr.Error()
			} else {
				// Cleanup succeeded
				succeeded := true
				cleanupSucceeded = &succeeded
			}

			// Record failure with cleanup outcome
			if mon != nil && executionID != "" {
				errorMsg := result.Error.Error()
				if recordErr := mon.RecordFailure(ctx, executionID, errorMsg, cleanupAttempted, cleanupSucceeded, cleanupError); recordErr != nil {
					// Log but don't override original error
					fmt.Printf("Warning: failed to record execution failure: %v\n", recordErr)
				}
			}
		} else if result.Success && mon != nil && executionID != "" {
			// Backup succeeded - record success
			if recordErr := mon.RecordSuccess(ctx, executionID); recordErr != nil {
				fmt.Printf("Warning: failed to record execution success: %v\n", recordErr)
			}
		}
	}()

	// Execute the backup
	if err := backupFn(ctx); err != nil {
		result.Error = err
		result.CleanupNeeded = true
		return result
	}

	result.Success = true
	return result
}
