package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/monitor"
	internalrestore "github.com/denisakp/sentinel/internal/restore"
	"github.com/denisakp/sentinel/internal/storage"
)

var runSharedRestoreExecution = internalrestore.ExecuteRestore

// RestoreExecutionConfig defines parameters for a scheduled restore operation
type RestoreExecutionConfig struct {
	// Job identifier/name
	JobName string

	// Database type: "postgres", "mysql", "mariadb", "mongodb"
	DatabaseType string

	// Connection parameters
	Host     string
	Port     int
	Username string
	Password string
	Database string // for SQL databases
	URI      string // for MongoDB

	// Restore source (backup file or location)
	BackupPath   string
	BackupSource string // "local", "s3", "gdrive", "azure"

	// Database-specific restore options
	Options map[string]interface{}

	// Restore strategy
	OnConflict string // "ignore", "replace", "error"

	// Whether to verify restored data
	VerifyAfterRestore bool

	// Timeout for the entire restore operation
	Timeout time.Duration
}

// RestoreExecutor handles scheduled restore operations
type RestoreExecutor struct {
	// Function to execute the restore (injected for testing)
	restoreFn func(ctx context.Context, config *RestoreExecutionConfig) (*RestoreResult, error)
}

// RestoreResult captures the outcome of a restore operation
type RestoreResult struct {
	Success            bool
	DatabaseType       string
	BackupFile         string
	RestoredDatabase   string
	BytesRestored      int64
	Duration           time.Duration
	StartTime          time.Time
	EndTime            time.Time
	ErrorMessage       string
	VerificationPassed bool // true if verification succeed or skipped
}

// NewRestoreExecutor creates a RestoreExecutor with the provided function
func NewRestoreExecutor(restoreFn func(ctx context.Context, config *RestoreExecutionConfig) (*RestoreResult, error)) *RestoreExecutor {
	return &RestoreExecutor{
		restoreFn: restoreFn,
	}
}

// Execute performs a scheduled restore operation
func (re *RestoreExecutor) Execute(ctx context.Context, config *RestoreExecutionConfig) (*RestoreResult, error) {
	if err := validateRestoreConfig(config); err != nil {
		return nil, fmt.Errorf("invalid restore configuration: %w", err)
	}

	// Apply timeout from config if specified
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	result, err := re.restoreFn(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("restore execution failed: %w", err)
	}

	return result, nil
}

// validateRestoreConfig validates required restore parameters
func validateRestoreConfig(config *RestoreExecutionConfig) error {
	if config == nil {
		return fmt.Errorf("restore configuration cannot be nil")
	}

	if config.JobName == "" {
		return fmt.Errorf("job name is required")
	}

	if config.DatabaseType == "" {
		return fmt.Errorf("database type is required")
	}

	// Validate database type
	validTypes := map[string]bool{
		"postgres": true,
		"mysql":    true,
		"mariadb":  true,
		"mongodb":  true,
	}
	if !validTypes[config.DatabaseType] {
		return fmt.Errorf("unsupported database type: %s", config.DatabaseType)
	}

	// Validate connection parameters based on type
	switch config.DatabaseType {
	case "postgres", "mysql", "mariadb":
		if config.Host == "" {
			return fmt.Errorf("host is required for %s restore", config.DatabaseType)
		}
		if config.Username == "" {
			return fmt.Errorf("username is required for %s restore", config.DatabaseType)
		}
		if config.Database == "" {
			return fmt.Errorf("database name is required for %s restore", config.DatabaseType)
		}
	case "mongodb":
		if config.URI == "" {
			return fmt.Errorf("MongoDB URI is required")
		}
	}

	// Validate backup source
	if config.BackupPath == "" && config.BackupSource == "" {
		return fmt.Errorf("backup path or backup source is required")
	}

	// Validate conflict strategy if specified
	if config.OnConflict != "" {
		validStrategies := map[string]bool{
			"ignore":  true,
			"replace": true,
			"error":   true,
		}
		if !validStrategies[config.OnConflict] {
			return fmt.Errorf("invalid conflict strategy: %s", config.OnConflict)
		}
	}

	return nil
}

// PostRestoreVerifier provides optional verification after restore completes
type PostRestoreVerifier interface {
	// Verify checks the integrity and completeness of a restored database
	// Returns true if verification passes, false if it fails
	Verify(ctx context.Context, databaseType string, host string, port int, username string, password string, database string) (bool, error)
}

// RestoreExecutionResult captures the outcome of a restore operation with cleanup tracking.
type RestoreExecutionResult struct {
	Success       bool
	ExecutionID   string
	BackupPath    string
	Error         error
	CleanupNeeded bool
}

// ExecuteRestoreWithCleanup wraps restore execution with automatic cleanup on failure.
// This ensures partial artifacts or temporary files are deleted if the restore fails.
func ExecuteRestoreWithCleanup(
	ctx context.Context,
	executionID string,
	backupPath string,
	store storage.Storage,
	mon *monitor.Monitor,
	restoreFn func(context.Context) error,
) *RestoreExecutionResult {
	result := &RestoreExecutionResult{
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
			// Restore failed - attempt cleanup of partial restore artifacts
			cleanupAttempted = true
			if cleanupErr := store.DeleteBackup(ctx, backupPath); cleanupErr != nil {
				// Cleanup failed (backup file might be on remote storage, cleanup may not apply)
				succeeded := false
				cleanupSucceeded = &succeeded
				cleanupError = cleanupErr.Error()
			} else {
				// Cleanup succeeded or not needed
				succeeded := true
				cleanupSucceeded = &succeeded
			}

			// Record failure with cleanup outcome
			if mon != nil && executionID != "" {
				errorMsg := result.Error.Error()
				if recordErr := mon.RecordFailure(ctx, executionID, errorMsg, cleanupAttempted, cleanupSucceeded, cleanupError); recordErr != nil {
					// Log but don't override original error
					fmt.Printf("Warning: failed to record restore failure: %v\n", recordErr)
				}
			}
		} else if result.Success && mon != nil && executionID != "" {
			// Restore succeeded - record success
			if recordErr := mon.RecordSuccess(ctx, executionID); recordErr != nil {
				fmt.Printf("Warning: failed to record restore success: %v\n", recordErr)
			}
		}
	}()

	// Execute the restore
	if err := restoreFn(ctx); err != nil {
		result.Error = err
		result.CleanupNeeded = true
		return result
	}

	result.Success = true
	return result
}

// ExecuteScheduledRestore executes a restore job through the shared restore
// executor and enforces optional scheduler-level concurrency limits.
func ExecuteScheduledRestore(
	ctx context.Context,
	cfg *config.Configuration,
	jobName string,
	job config.RestoreJob,
	mon *monitor.Monitor,
	limiter chan struct{},
) (*internalrestore.ExecutionResult, error) {
	if limiter != nil {
		select {
		case limiter <- struct{}{}:
			defer func() { <-limiter }()
		default:
			result := &internalrestore.ExecutionResult{
				Status:             monitor.StatusSkipped,
				Reason:             "concurrency_limit_reached",
				StartedAt:          time.Now().UTC(),
				CompletedAt:        time.Now().UTC(),
				SourceType:         job.BackupSource.Type,
				ConflictStrategy:   effectiveConflictStrategy(job.ConflictStrategy),
				TimeoutSeconds:     job.TimeoutSeconds,
				VerificationPassed: false,
			}

			if mon != nil {
				_ = mon.RecordRestoreExecution(ctx, &monitor.RestoreExecution{
					RestoreName:      jobName,
					DatabaseType:     job.Type,
					DatabaseName:     job.Database,
					SourceType:       job.BackupSource.Type,
					ConflictStrategy: effectiveConflictStrategy(job.ConflictStrategy),
					Timestamp:        result.StartedAt,
					DurationMs:       0,
					Status:           monitor.StatusSkipped,
					Reason:           "concurrency_limit_reached",
					ErrorReason:      "concurrency_limit_reached",
					SourceBackupPath: job.BackupSource.BackupPath,
					CreatedAt:        result.StartedAt,
					FinishedAt:       &result.CompletedAt,
				})
			}

			return result, nil
		}
	}

	result, err := runSharedRestoreExecution(ctx, &internalrestore.ExecutionRequest{
		JobName: jobName,
		Job:     job,
		Config:  cfg,
		Monitor: mon,
		LockDir: cfg.Scheduler.LockDir,
	})
	if err != nil {
		if result != nil && result.Status == monitor.StatusSkipped {
			return result, nil
		}
		return result, err
	}

	return result, nil
}

func effectiveConflictStrategy(strategy string) string {
	if strategy == "" {
		return "error"
	}
	return strategy
}
