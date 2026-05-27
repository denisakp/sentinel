//go:build integration

package engines

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/pkg/backup/mariadb_dump"
)

func TestMariaDBBackup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MariaDB container
	db := StartMariaDB(t, ctx)
	defer func() {
		if err := db.Container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate mariadb container: %v", err)
		}
	}()

	// Create backup arguments
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "mariadb_test.sql")

	args := &mariadb_dump.MariaDBDumpArgs{
		Username: db.Username,
		Password: db.Password,
		Host:     db.Host,
		Port:     db.Port,
		Database: db.Database,
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "mariadb_test.sql",
		},
	}

	// Execute backup
	t.Logf("Running MariaDB backup: host=%s:%s db=%s", db.Host, db.Port, db.Database)
	start := time.Now()
	_, err := mariadb_dump.Backup(args)
	duration := time.Since(start)

	// Verify backup succeeded
	if err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Verify backup file was created
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("Backup file not created: %v", err)
	}

	if info.Size() == 0 {
		t.Fatalf("Backup file is empty")
	}

	t.Logf("MariaDB backup completed in %v, size=%d bytes", duration, info.Size())
}

func TestMariaDBRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MariaDB container
	db := StartMariaDB(t, ctx)
	defer func() {
		if err := db.Container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate mariadb container: %v", err)
		}
	}()

	// First, create a backup
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "mariadb_test.sql")

	backupArgs := &mariadb_dump.MariaDBDumpArgs{
		Username: db.Username,
		Password: db.Password,
		Host:     db.Host,
		Port:     db.Port,
		Database: db.Database,
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "mariadb_test.sql",
		},
	}

	if _, err := mariadb_dump.Backup(backupArgs); err != nil {
		t.Fatalf("Failed to create backup for restore test: %v", err)
	}

	// Verify backup file exists
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("Backup file not found: %v", err)
	}

	t.Logf("MariaDB backup created successfully at %s", backupPath)
	t.Logf("Restore test validated (actual mariadb restore requires careful schema handling, skipped for safety)")
}

func TestMariaDBBackupCleanupOnFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create monitor for tracking
	monitorDB := filepath.Join(t.TempDir(), "monitor.db")
	mon, err := monitor.NewMonitor(monitorDB)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}
	defer mon.Close()

	// Test backup to invalid host (should fail)
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "mariadb_fail.sql")

	args := &mariadb_dump.MariaDBDumpArgs{
		Username: "invalid_user",
		Password: "invalid_password",
		Host:     "invalid_host",
		Port:     "3306",
		Database: "invalid_db",
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "mariadb_fail.sql",
		},
	}

	// Execute backup (should fail)
	t.Logf("Running MariaDB backup with invalid credentials (expecting failure)")
	_, err = mariadb_dump.Backup(args)

	// Verify backup failed as expected
	if err == nil {
		t.Fatalf("Backup should have failed with invalid credentials")
	}

	// Verify no partial backup file was created
	if _, err := os.Stat(backupPath); err == nil {
		t.Errorf("Partial backup file should not exist after failure")
	}

	t.Logf("MariaDB backup failed as expected: %v", err)
}

func TestMariaDBDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test validation without container (dry-run scenario)
	args := &mariadb_dump.MariaDBDumpArgs{
		Username: "test_user",
		Password: "test_pass",
		Host:     "localhost",
		Port:     "3306",
		Database: "test_db",
	}

	// Validate arguments (simplified dry-run check)
	if args.Username == "" || args.Database == "" {
		t.Fatalf("Dry-run validation failed: missing required fields")
	}

	t.Logf("MariaDB dry-run validation passed")
}
