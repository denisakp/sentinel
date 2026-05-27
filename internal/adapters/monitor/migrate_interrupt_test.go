package monitor

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestRunMigrationsLocked_StepRollback verifies that a failing migration step
// rolls back atomically: the step's schema_migrations row is not written,
// schema_version is not advanced, and pre-existing data survives.
//
// We force a deterministic failure by pre-creating backup_executions with the
// columns that migration 002 attempts to ADD, so the second migration step
// fails with a duplicate column error mid-application.
func TestRunMigrationsLocked_StepRollback(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Bootstrap: schema_migrations table + backup_executions in a shape that
	// makes migration 002 fail (columns already present).
	if _, err := db.Exec(SchemaMigrationsTable); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE backup_executions (
			id TEXT PRIMARY KEY,
			backup_name TEXT NOT NULL,
			database_type TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			status TEXT NOT NULL,
			duration_ms INTEGER,
			storage_backend TEXT NOT NULL,
			file_path TEXT NOT NULL,
			file_size_bytes INTEGER,
			error_message TEXT,
			checksum TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			-- Columns added by migration 002 — pre-creating them forces 002 to fail.
			finished_at DATETIME,
			cleanup_attempted INTEGER DEFAULT 0,
			cleanup_succeeded INTEGER,
			cleanup_error TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`); err != nil {
		t.Fatalf("seed backup_executions: %v", err)
	}
	// Seed a row that must survive the rollback.
	if _, err := db.Exec(
		`INSERT INTO backup_executions (id, backup_name, database_type, timestamp, status, storage_backend, file_path)
		 VALUES ('seed', 'job', 'postgres', CURRENT_TIMESTAMP, 'success', 'local', '/tmp/x')`,
	); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	err = runMigrationsLocked(dbPath, db)
	if err == nil {
		t.Fatal("expected migration error from duplicate-column conflict; got nil")
	}

	// Invariant (a): schema_version was not advanced.
	v, err := readSchemaVersionIfPresent(db)
	if err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if v >= BinarySchemaVersion {
		t.Errorf("schema_version = %d; want < %d (no advance after failure)", v, BinarySchemaVersion)
	}

	// Invariant (b): schema_migrations has no row for the failing step (002),
	// because its transaction rolled back. Step 001 may be recorded (its
	// CREATE TABLE IF NOT EXISTS succeeded cleanly).
	var has002 int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 2`).Scan(&has002); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if has002 != 0 {
		t.Errorf("schema_migrations row for version 2 present despite rollback")
	}

	// Invariant (c): the seeded row survives.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM backup_executions WHERE id = 'seed'`).Scan(&count); err != nil {
		t.Fatalf("count seed row: %v", err)
	}
	if count != 1 {
		t.Errorf("seeded row lost; got count = %d", count)
	}
}
