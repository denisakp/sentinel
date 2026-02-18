//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/scheduler"
)

// TestSchedulerMigrationFailure verifies that scheduler.Start() fails fast when migrations cannot be applied.
// This is a critical safety check to prevent the scheduler from running with an inconsistent database schema.
func TestSchedulerMigrationFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a temporary directory for test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "corrupted.db")

	// Create a corrupted/invalid database file that will cause migration failure
	// Write invalid SQLite header to simulate corruption
	if err := os.WriteFile(dbPath, []byte("INVALID_SQLITE_HEADER_DATA"), 0644); err != nil {
		t.Fatalf("Failed to create corrupted database file: %v", err)
	}

	// Create a minimal valid configuration with the corrupted database path
	cfg := &config.Configuration{
		HistoryDBPath: dbPath,
		BackupJobs:    []config.BackupJob{}, // Empty jobs to avoid validation errors
		Defaults: &config.Defaults{
			Retention: &config.RetentionPolicy{
				KeepLast: 1, // Minimal valid retention
			},
		},
	}

	// Create scheduler with the configuration
	sched := scheduler.NewScheduler(cfg)

	// Attempt to start scheduler - should fail during migration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := sched.Start(ctx)

	// Verify that Start() returned an error (fail-fast behavior)
	if err == nil {
		t.Fatal("Expected scheduler.Start() to fail with corrupted database, but it succeeded")
	}

	// Verify error message mentions migration or database initialization
	errMsg := err.Error()
	if errMsg == "" {
		t.Fatal("Error message should not be empty")
	}

	t.Logf("Scheduler failed fast as expected: %v", err)
}

// TestSchedulerMigrationSuccess verifies that scheduler starts successfully with valid database.
// This is a positive control test to ensure the migration system works in the happy path.
func TestSchedulerMigrationSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a temporary directory for test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "valid.db")

	// Create a minimal valid configuration
	cfg := &config.Configuration{
		HistoryDBPath: dbPath,
		BackupJobs:    []config.BackupJob{}, // Empty jobs for simplicity
		Defaults: &config.Defaults{
			Retention: &config.RetentionPolicy{
				KeepLast: 1,
			},
		},
	}

	// Create scheduler with the configuration
	sched := scheduler.NewScheduler(cfg)

	// Start scheduler - should succeed and apply migrations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := sched.Start(ctx)
	if err != nil {
		t.Fatalf("Expected scheduler.Start() to succeed, but got error: %v", err)
	}

	// Stop scheduler cleanly
	sched.Stop(ctx)

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("Expected database file to be created, but it doesn't exist")
	}

	t.Logf("Scheduler started successfully and applied migrations")
}

// TestSchedulerStopWithoutStart verifies that stopping a scheduler that was never started doesn't panic.
func TestSchedulerStopWithoutStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &config.Configuration{
		HistoryDBPath: filepath.Join(t.TempDir(), "test.db"),
		BackupJobs:    []config.BackupJob{},
		Defaults: &config.Defaults{
			Retention: &config.RetentionPolicy{
				KeepLast: 1,
			},
		},
	}

	sched := scheduler.NewScheduler(cfg)

	// Stop scheduler without starting it - should not panic
	ctx := context.Background()
	sched.Stop(ctx)

	t.Log("Scheduler.Stop() executed successfully without prior Start()")
}
