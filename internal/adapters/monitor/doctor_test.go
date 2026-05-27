package monitor

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnose_Missing(t *testing.T) {
	dir := t.TempDir()
	r, err := Diagnose(filepath.Join(dir, "absent.db"))
	if err != nil {
		t.Fatalf("Diagnose error: %v", err)
	}
	if r.Status != StatusMissing {
		t.Errorf("status = %q; want %q", r.Status, StatusMissing)
	}
	if r.CurrentVersion != 0 {
		t.Errorf("current_version = %d; want 0", r.CurrentVersion)
	}
	if r.RequiredVersion != BinarySchemaVersion {
		t.Errorf("required_version = %d; want %d", r.RequiredVersion, BinarySchemaVersion)
	}
	if r.Hint == "" {
		t.Error("hint must be populated for missing")
	}
}

func TestDiagnose_Corrupt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(p, []byte("this is not a sqlite file"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Diagnose(p)
	if err != nil {
		t.Fatalf("Diagnose error: %v", err)
	}
	if r.Status != StatusCorrupt {
		t.Errorf("status = %q; want %q", r.Status, StatusCorrupt)
	}
	if r.Error == "" {
		t.Error("error field must be populated for corrupt")
	}
}

func TestDiagnose_Current(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	mon, err := NewMonitor(p)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	_ = mon.Close()

	r, err := Diagnose(p)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if r.Status != StatusCurrent {
		t.Errorf("status = %q; want %q", r.Status, StatusCurrent)
	}
	if r.CurrentVersion != BinarySchemaVersion {
		t.Errorf("current_version = %d; want %d", r.CurrentVersion, BinarySchemaVersion)
	}
	if len(r.PendingMigrations) != 0 {
		t.Errorf("pending = %v; want empty", r.PendingMigrations)
	}
	if len(r.Tables) == 0 {
		t.Error("tables must list at least schema_version + schema_migrations")
	}
}

func TestDiagnose_StalePending(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	// Bring DB to current then roll the stamp back to simulate a stale state.
	mon, err := NewMonitor(p)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	_ = mon.Close()

	rollSchemaVersion(t, p, 1)

	r, err := Diagnose(p)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if r.Status != StatusStalePending {
		t.Errorf("status = %q; want %q", r.Status, StatusStalePending)
	}
	if r.CurrentVersion != 1 {
		t.Errorf("current_version = %d; want 1", r.CurrentVersion)
	}
	// Migrations 002..N must appear in pending (we have 001-004).
	if len(r.PendingMigrations) == 0 {
		t.Error("pending list must be non-empty when current < required")
	}
}

func TestDiagnose_ForwardIncompatible(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	stampSchemaVersionRaw(t, p, BinarySchemaVersion+5)

	r, err := Diagnose(p)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if r.Status != StatusForwardIncompatible {
		t.Errorf("status = %q; want %q", r.Status, StatusForwardIncompatible)
	}
	if !strings.Contains(r.Hint, "upgrade") {
		t.Errorf("hint should suggest upgrade; got %q", r.Hint)
	}
}

func TestRepair_StalePendingBecomesCurrent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	mon, err := NewMonitor(p)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	_ = mon.Close()
	rollSchemaVersion(t, p, BinarySchemaVersion-1)

	pre, err := Diagnose(p)
	if err != nil {
		t.Fatalf("pre Diagnose: %v", err)
	}
	if pre.Status != StatusStalePending {
		t.Fatalf("pre status = %q; want stale-pending", pre.Status)
	}

	post, err := Repair(p)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if post.Status != StatusCurrent {
		t.Errorf("post status = %q; want current", post.Status)
	}
	if len(post.AppliedThisRun) == 0 {
		t.Error("applied_this_run must list the re-applied migration(s)")
	}
}

func TestRepair_RefusesForwardIncompatible(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	stampSchemaVersionRaw(t, p, BinarySchemaVersion+1)

	post, err := Repair(p)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if post.Status != StatusForwardIncompatible {
		t.Errorf("status = %q; want forward-incompatible (repair refuses)", post.Status)
	}
	if len(post.AppliedThisRun) != 0 {
		t.Errorf("applied_this_run = %v; want empty", post.AppliedThisRun)
	}
}

// --- test helpers --------------------------------------------------------

func stampSchemaVersionRaw(t *testing.T, dbPath string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(SchemaVersionTable); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO schema_version (id, version) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET version = excluded.version`,
		version,
	); err != nil {
		t.Fatal(err)
	}
}

func rollSchemaVersion(t *testing.T, dbPath string, version int) {
	t.Helper()
	stampSchemaVersionRaw(t, dbPath, version)
}
