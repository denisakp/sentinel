package monitor

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/denisakp/sentinel/internal/ports"
)

// TestRecordIntegrityCheck_RoundTrip inserts one grouped run of mixed-outcome
// results and reads it back grouped by run_id, asserting every column plus the
// shared trigger label survive the round trip.
func TestRecordIntegrityCheck_RoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	defer mon.Close()

	checkedAt := time.Date(2026, 8, 3, 6, 0, 0, 0, time.UTC)
	run := ports.IntegrityRun{
		RunID:   "run-abc",
		Trigger: ports.IntegrityTriggerScheduled,
		Results: []ports.IntegrityResult{
			{BackupID: "b1", Job: "pg", Result: ports.IntegrityResultOK, StoredHash: "aa", ComputedHash: "aa", StorageBackend: "local", ArtifactPath: "/a.sql", CheckedAt: checkedAt},
			{BackupID: "b2", Job: "pg", Result: ports.IntegrityResultCorrupted, StoredHash: "bb", ComputedHash: "cc", StorageBackend: "s3", ArtifactPath: "b.sql", CheckedAt: checkedAt},
			{BackupID: "b3", Job: "mysql", Result: ports.IntegrityResultMissingManifest, StorageBackend: "local", ArtifactPath: "/c.sql", CheckedAt: checkedAt},
		},
	}
	if err := mon.RecordIntegrityCheck(context.Background(), run); err != nil {
		t.Fatalf("RecordIntegrityCheck: %v", err)
	}

	rows, err := mon.db.Query(`SELECT backup_id, job_name, result, stored_hash, computed_hash, storage_backend, artifact_path, trigger
		FROM integrity_checks WHERE run_id = ? ORDER BY backup_id`, "run-abc")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type row struct {
		backupID, job, result, stored, computed, backend, path, trigger string
	}
	got := map[string]row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.backupID, &r.job, &r.result, &r.stored, &r.computed, &r.backend, &r.path, &r.trigger); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[r.backupID] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("rows for run-abc = %d, want 3", len(got))
	}
	if r := got["b2"]; r.result != "corrupted" || r.stored != "bb" || r.computed != "cc" || r.backend != "s3" || r.path != "b.sql" || r.trigger != "scheduled" {
		t.Fatalf("b2 row mismatch: %+v", r)
	}
	if r := got["b1"]; r.result != "ok" || r.trigger != "scheduled" {
		t.Fatalf("b1 row mismatch: %+v", r)
	}
	if r := got["b3"]; r.result != "missing_manifest" || r.job != "mysql" {
		t.Fatalf("b3 row mismatch: %+v", r)
	}
}

// TestRecordIntegrityCheck_EmptyRunNoOp asserts an empty Results slice records
// nothing and is not an error (empty / recency-bounded sweep).
func TestRecordIntegrityCheck_EmptyRunNoOp(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	defer mon.Close()

	if err := mon.RecordIntegrityCheck(context.Background(), ports.IntegrityRun{RunID: "empty", Trigger: "scheduled"}); err != nil {
		t.Fatalf("RecordIntegrityCheck(empty): %v", err)
	}
	var n int
	if err := mon.db.QueryRow(`SELECT COUNT(*) FROM integrity_checks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}

// TestRecordIntegrityCheck_RejectsBadResult asserts the result CHECK constraint
// rejects an out-of-vocabulary outcome, and the whole run is rolled back
// (SC-004 — the store is the vocabulary boundary).
func TestRecordIntegrityCheck_RejectsBadResult(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	defer mon.Close()

	run := ports.IntegrityRun{
		RunID:   "bad",
		Trigger: ports.IntegrityTriggerManual,
		Results: []ports.IntegrityResult{
			{BackupID: "b1", Job: "pg", Result: ports.IntegrityResultOK, CheckedAt: time.Now().UTC()},
			{BackupID: "b2", Job: "pg", Result: "rotten", CheckedAt: time.Now().UTC()}, // not in the four states
		},
	}
	if err := mon.RecordIntegrityCheck(context.Background(), run); err == nil {
		t.Fatal("expected CHECK-constraint rejection for result='rotten', got nil")
	}

	// The transaction rolled back: the valid first row must not have landed.
	var n int
	if err := mon.db.QueryRow(`SELECT COUNT(*) FROM integrity_checks WHERE run_id = 'bad'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows after rejected run = %d, want 0 (rollback)", n)
	}
}

// TestRecordIntegrityCheck_RejectsBadTrigger asserts the trigger CHECK
// constraint rejects an out-of-vocabulary trigger value.
func TestRecordIntegrityCheck_RejectsBadTrigger(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	defer mon.Close()

	run := ports.IntegrityRun{
		RunID:   "badtrig",
		Trigger: "cronjob", // not manual|scheduled
		Results: []ports.IntegrityResult{
			{BackupID: "b1", Job: "pg", Result: ports.IntegrityResultOK, CheckedAt: time.Now().UTC()},
		},
	}
	if err := mon.RecordIntegrityCheck(context.Background(), run); err == nil {
		t.Fatal("expected CHECK-constraint rejection for trigger='cronjob', got nil")
	}
}

// TestMigration005_AppliesOnSeededV4DB seeds a genuine v4 database (migrations
// 001–004 only, schema_version=4) and asserts that opening it with the current
// binary applies migration 005: the integrity_checks table appears, a
// schema_migrations row for version 5 is recorded, and schema_version advances
// to BinarySchemaVersion.
func TestMigration005_AppliesOnSeededV4DB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	seedSchemaVersion(t, dbPath, 4)

	// Precondition: the v4 seed must NOT have the integrity_checks table.
	if tableExistsRaw(t, dbPath, "integrity_checks") {
		t.Fatal("seeded v4 DB unexpectedly already has integrity_checks")
	}

	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor on seeded v4 DB: %v", err)
	}
	defer mon.Close()

	if !tableExistsRaw(t, dbPath, "integrity_checks") {
		t.Fatal("integrity_checks table missing after 4→5 migration")
	}
	v, err := readSchemaVersion(mon.db)
	if err != nil {
		t.Fatalf("readSchemaVersion: %v", err)
	}
	if v != BinarySchemaVersion {
		t.Fatalf("schema_version=%d, want %d", v, BinarySchemaVersion)
	}
	var applied int
	if err := mon.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 5`).Scan(&applied); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if applied != 1 {
		t.Fatalf("schema_migrations rows for version 5 = %d, want 1 (migration must have run)", applied)
	}

	// The migrated table is usable: a record round-trips.
	if err := mon.RecordIntegrityCheck(context.Background(), ports.IntegrityRun{
		RunID:   "post-migrate",
		Trigger: ports.IntegrityTriggerScheduled,
		Results: []ports.IntegrityResult{{BackupID: "b1", Job: "pg", Result: ports.IntegrityResultOK, CheckedAt: time.Now().UTC()}},
	}); err != nil {
		t.Fatalf("RecordIntegrityCheck after migration: %v", err)
	}
}

// seedSchemaVersion builds a database at the given schema version by applying
// only migrations with version <= target from the embedded FS, recording them
// in schema_migrations, and stamping schema_version. It mirrors the migration
// runner without the current binary's newer migrations, producing a genuine
// "older" DB.
func seedSchemaVersion(t *testing.T, path string, target int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(SchemaMigrationsTable); err != nil {
		t.Fatalf("ensure schema_migrations: %v", err)
	}
	if _, err := db.Exec(SchemaVersionTable); err != nil {
		t.Fatalf("ensure schema_version: %v", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	type mf struct {
		version  int
		name     string
		fileName string
	}
	var files []mf
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, name, perr := parseMigrationFileName(e.Name())
		if perr != nil {
			t.Fatalf("parse migration name %q: %v", e.Name(), perr)
		}
		if v <= target {
			files = append(files, mf{version: v, name: name, fileName: e.Name()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })

	for _, f := range files {
		content, rerr := migrationsFS.ReadFile("migrations/" + f.fileName)
		if rerr != nil {
			t.Fatalf("read migration %s: %v", f.fileName, rerr)
		}
		if _, err := db.Exec(string(content)); err != nil {
			t.Fatalf("apply migration %s: %v", f.fileName, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, f.version, f.name); err != nil {
			t.Fatalf("record migration %s: %v", f.fileName, err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin stamp tx: %v", err)
	}
	if err := writeSchemaVersion(tx, target); err != nil {
		_ = tx.Rollback()
		t.Fatalf("stamp schema_version: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit stamp: %v", err)
	}
}

// tableExistsRaw reports whether a table exists in the SQLite file at path,
// opening it independently of the Monitor.
func tableExistsRaw(t *testing.T, path, table string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return name == table
}
