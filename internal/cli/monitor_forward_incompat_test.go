package cli

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
)

// stampForwardIncompatibleDB creates a SQLite file with schema_version stamped
// to a value above BinarySchemaVersion. It mirrors the minimum state a newer
// Sentinel release would have left behind.
func stampForwardIncompatibleDB(t *testing.T, dbPath string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(monitor.SchemaVersionTable); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO schema_version (id, version) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET version = excluded.version`,
		version,
	); err != nil {
		t.Fatalf("stamp schema_version=%d: %v", version, err)
	}
}

// tableExists reports whether the named SQLite table is present in dbPath.
func tableExists(t *testing.T, dbPath, name string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	var found string
	row := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, name)
	switch err := row.Scan(&found); err {
	case nil:
		return true
	case sql.ErrNoRows:
		return false
	default:
		t.Fatalf("query sqlite_master: %v", err)
		return false
	}
}

// runMonitorList drives RootCmd through `monitor list --config <cfg>` and
// returns the Execute error plus combined stdout/stderr captured from the
// command tree.
func runMonitorList(t *testing.T, cfgPath string) (error, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"monitor", "list", "--config", cfgPath})
	err := Execute()
	return err, buf.String()
}

func writeMinimalConfig(t *testing.T, dir, historyDB string) string {
	t.Helper()
	cfg := `version: "1.0"
history_db_path: ` + historyDB + `
databases:
  dummy:
    type: postgres
    host: localhost
    port: 5432
    username: u
    password_env: DUMMY_PW
    database: d
    schedule: "0 0 * * *"
    storage:
      type: local
      local_path: ` + filepath.Join(dir, "out") + `
`
	p := filepath.Join(dir, "sentinel.yaml")
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

// T014: opening the monitor against a forward-incompatible DB must return
// ErrForwardIncompatible, surface non-zero exit code via cli.Code, and leave
// the execution tables unborn (no inserts attempted).
func TestMonitorForwardIncompat_FailsClean(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")
	stampForwardIncompatibleDB(t, dbPath, 99)
	t.Setenv("DUMMY_PW", "x")
	cfgPath := writeMinimalConfig(t, dir, dbPath)

	err, out := runMonitorList(t, cfgPath)
	if err == nil {
		t.Fatal("expected ErrForwardIncompatible, got nil")
	}
	if !errors.Is(err, monitor.ErrForwardIncompatible) {
		t.Fatalf("errors.Is(err, ErrForwardIncompatible) = false; err = %v", err)
	}
	if got := Code(err); got == 0 {
		t.Fatalf("Code(err) = 0; want non-zero exit on forward-incompat")
	}
	// No rows are inserted because NewMonitor returns before creating the
	// execution tables. Asserting the table is absent is equivalent to (and
	// stronger than) row count = 0.
	if tableExists(t, dbPath, "backup_executions") {
		t.Error("backup_executions table created despite forward-incompat refusal")
	}
	if tableExists(t, dbPath, "restore_executions") {
		t.Error("restore_executions table created despite forward-incompat refusal")
	}

	// The error message must name both versions exactly once (T015 invariant).
	msg := err.Error()
	if strings.Count(msg, "monitor schema is ahead of this binary") != 1 {
		t.Errorf("expected the ahead-of-binary phrase exactly once; got %d in %q",
			strings.Count(msg, "monitor schema is ahead of this binary"), msg)
	}
	if !strings.Contains(msg, "99") || !strings.Contains(msg, strconv.Itoa(monitor.BinarySchemaVersion)) {
		t.Errorf("error message must name both versions; got %q", msg)
	}

	// The CLI formatter should have surfaced the constitution III block.
	if !strings.Contains(out, "What:") || !strings.Contains(out, "Why:") || !strings.Contains(out, "How:") {
		t.Errorf("expected what/why/how block in CLI output; got:\n%s", out)
	}
}
