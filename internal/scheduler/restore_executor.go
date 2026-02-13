package scheduler

import (
	"context"
	"fmt"
	"time"
)

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
