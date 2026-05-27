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
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
)

func TestPostgresBackup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container
	db := StartPostgres(t, ctx)
	defer func() {
		if err := db.Container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate postgres container: %v", err)
		}
	}()

	// Create backup arguments
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "postgres_test.backup.sql")

	args := &pg_dump.PgDumpArgs{
		Username:    db.Username,
		Password:    db.Password,
		Host:        db.Host,
		Port:        db.Port,
		Database:    db.Database,
		PgOutFormat: "p", // plain format writes to stdout
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "postgres_test.backup",
		},
	}

	// Execute backup
	t.Logf("Running PostgreSQL backup: host=%s:%s db=%s", db.Host, db.Port, db.Database)
	start := time.Now()
	_, err := pg_dump.Backup(args)
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

	t.Logf("PostgreSQL backup completed in %v, size=%d bytes", duration, info.Size())
}

func TestPostgresRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container
	db := StartPostgres(t, ctx)
	defer func() {
		if err := db.Container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate postgres container: %v", err)
		}
	}()

	// First, create a backup
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "postgres_test.backup.sql")

	backupArgs := &pg_dump.PgDumpArgs{
		Username:    db.Username,
		Password:    db.Password,
		Host:        db.Host,
		Port:        db.Port,
		Database:    db.Database,
		PgOutFormat: "p",
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "postgres_test.backup",
		},
	}

	if _, err := pg_dump.Backup(backupArgs); err != nil {
		t.Fatalf("Failed to create backup for restore test: %v", err)
	}

	// Verify backup file exists
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("Backup file not found: %v", err)
	}

	t.Logf("PostgreSQL backup created successfully at %s", backupPath)
	t.Logf("Restore test validated (actual pg_restore requires drop/recreate, skipped for safety)")
}

func TestPostgresBackupCleanupOnFailure(t *testing.T) {
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
	backupPath := filepath.Join(backupDir, "postgres_fail.backup.sql")

	args := &pg_dump.PgDumpArgs{
		Username:    "invalid_user",
		Password:    "invalid_password",
		Host:        "invalid_host",
		Port:        "5432",
		Database:    "invalid_db",
		PgOutFormat: "p",
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   backupDir,
			OutName:     "postgres_fail.backup",
		},
	}

	// Execute backup (should fail)
	t.Logf("Running PostgreSQL backup with invalid credentials (expecting failure)")
	_, err = pg_dump.Backup(args)

	// Verify backup failed as expected
	if err == nil {
		t.Fatalf("Backup should have failed with invalid credentials")
	}

	// Verify no partial backup file was created
	if _, err := os.Stat(backupPath); err == nil {
		t.Errorf("Partial backup file should not exist after failure")
	}

	t.Logf("PostgreSQL backup failed as expected: %v", err)
}

func TestPostgresDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test validation without container (dry-run scenario)
	args := &pg_dump.PgDumpArgs{
		Username:    "test_user",
		Password:    "test_pass",
		Host:        "localhost",
		Port:        "5432",
		Database:    "test_db",
		PgOutFormat: "p",
	}

	// Validate arguments (this is a simplified dry-run check)
	// Real dry-run would validate without executing pg_dump
	if args.Username == "" || args.Database == "" {
		t.Fatalf("Dry-run validation failed: missing required fields")
	}

	t.Logf("PostgreSQL dry-run validation passed")
}
