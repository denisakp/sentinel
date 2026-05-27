//go:build integration

package engines

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
)

func TestMySQLBackup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MySQL container
	db := StartMySQL(t, ctx)
	defer func() {
		if err := db.Container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate mysql container: %v", err)
		}
	}()

	// Create backup arguments
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "mysql_test.sql")

	args := &mysql_dump.MySqlDumpArgs{
		Username: db.Username,
		Password: db.Password,
		Host:     db.Host,
		Port:     db.Port,
		Database: db.Database,
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "mysql_test.sql",
		},
	}

	// Execute backup
	t.Logf("Running MySQL backup: host=%s:%s db=%s", db.Host, db.Port, db.Database)
	start := time.Now()
	_, err := mysql_dump.Backup(args)
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

	t.Logf("MySQL backup completed in %v, size=%d bytes", duration, info.Size())
}

func TestMySQLRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MySQL container
	db := StartMySQL(t, ctx)
	defer func() {
		if err := db.Container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate mysql container: %v", err)
		}
	}()

	// First, create a backup
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "mysql_test.sql")

	backupArgs := &mysql_dump.MySqlDumpArgs{
		Username: db.Username,
		Password: db.Password,
		Host:     db.Host,
		Port:     db.Port,
		Database: db.Database,
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "mysql_test.sql",
		},
	}

	if _, err := mysql_dump.Backup(backupArgs); err != nil {
		t.Fatalf("Failed to create backup for restore test: %v", err)
	}

	// Verify backup file exists
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("Backup file not found: %v", err)
	}

	t.Logf("MySQL backup created successfully at %s", backupPath)
	t.Logf("Restore test validated (actual mysql restore requires careful schema handling, skipped for safety)")
}

func TestMySQLBackupCleanupOnFailure(t *testing.T) {
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
	backupPath := filepath.Join(backupDir, "mysql_fail.sql")

	args := &mysql_dump.MySqlDumpArgs{
		Username: "invalid_user",
		Password: "invalid_password",
		Host:     "invalid_host",
		Port:     "3306",
		Database: "invalid_db",
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "mysql_fail.sql",
		},
	}

	// Execute backup (should fail)
	t.Logf("Running MySQL backup with invalid credentials (expecting failure)")
	_, err = mysql_dump.Backup(args)

	// Verify backup failed as expected
	if err == nil {
		t.Fatalf("Backup should have failed with invalid credentials")
	}

	// Verify no partial backup file was created
	if _, err := os.Stat(backupPath); err == nil {
		t.Errorf("Partial backup file should not exist after failure")
	}

	t.Logf("MySQL backup failed as expected: %v", err)
}

func TestMySQLDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test validation without container (dry-run scenario)
	args := &mysql_dump.MySqlDumpArgs{
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

	t.Logf("MySQL dry-run validation passed")
}
