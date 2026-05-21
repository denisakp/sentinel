package monitor

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DoctorStatus enumerates the high-level state the monitor DB can be in.
// Stable across releases; do not rename values without bumping the JSON
// schema in specs/017-monitor-schema-migration/contracts/.
const (
	StatusCurrent             = "current"
	StatusStalePending        = "stale-pending"
	StatusForwardIncompatible = "forward-incompatible"
	StatusMissing             = "missing"
	StatusCorrupt             = "corrupt"
)

// TableRow is one entry in DoctorReport.Tables.
type TableRow struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
}

// DoctorReport is the structured output of Diagnose/Repair. JSON tags mirror
// contracts/monitor-doctor.json.schema.json. Optional fields are emitted with
// omitempty so the wire shape matches the schema's conditional presence rules.
type DoctorReport struct {
	DatabasePath      string     `json:"database_path"`
	Status            string     `json:"status"`
	CurrentVersion    int        `json:"current_version"`
	RequiredVersion   int        `json:"required_version"`
	PendingMigrations []string   `json:"pending_migrations"`
	AppliedThisRun    []string   `json:"applied_this_run,omitempty"`
	Tables            []TableRow `json:"tables"`
	Error             string     `json:"error,omitempty"`
	Hint              string     `json:"hint,omitempty"`
}

// hintFor returns the operator-actionable hint for non-current statuses,
// matching the messages in contracts/monitor-doctor-cli.md.
func hintFor(status string) string {
	switch status {
	case StatusStalePending:
		return "run `sentinel monitor doctor --repair` to apply pending migrations."
	case StatusForwardIncompatible:
		return "upgrade the sentinel binary or restore the prior monitor database."
	case StatusMissing:
		return "the file does not exist; the next backup or restore run will create it."
	case StatusCorrupt:
		return "restore the monitor database from backup or remove the file to let sentinel recreate it."
	default:
		return ""
	}
}

// Diagnose inspects the monitor database without acquiring the migration lock
// and without mutating any state. It always returns a fully-populated
// DoctorReport; the returned error is non-nil only when the diagnosis itself
// could not run (e.g., the migration FS is unreadable — never expected in a
// shipped binary).
func Diagnose(historyDBPath string) (DoctorReport, error) {
	report := DoctorReport{
		DatabasePath:      historyDBPath,
		RequiredVersion:   BinarySchemaVersion,
		PendingMigrations: []string{},
		Tables:            []TableRow{},
	}

	if historyDBPath == "" {
		report.Status = StatusMissing
		report.Error = "history_db_path is not configured"
		report.Hint = hintFor(StatusMissing)
		return report, nil
	}

	if abs, err := filepath.Abs(historyDBPath); err == nil {
		report.DatabasePath = abs
	}

	if _, err := os.Stat(historyDBPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			report.Status = StatusMissing
			report.Hint = hintFor(StatusMissing)
			return report, nil
		}
		report.Status = StatusCorrupt
		report.Error = err.Error()
		report.Hint = hintFor(StatusCorrupt)
		return report, nil
	}

	db, err := sql.Open("sqlite", historyDBPath)
	if err != nil {
		report.Status = StatusCorrupt
		report.Error = err.Error()
		report.Hint = hintFor(StatusCorrupt)
		return report, nil
	}
	defer db.Close()

	// Sanity query — fails on a non-sqlite or truncated file.
	if _, err := db.Exec(`SELECT 1`); err != nil {
		report.Status = StatusCorrupt
		report.Error = err.Error()
		report.Hint = hintFor(StatusCorrupt)
		return report, nil
	}

	// schema_version may not exist on a pre-versioning DB; treat absent as 0.
	current := 0
	if v, err := readSchemaVersionIfPresent(db); err != nil {
		report.Status = StatusCorrupt
		report.Error = err.Error()
		report.Hint = hintFor(StatusCorrupt)
		return report, nil
	} else {
		current = v
	}
	report.CurrentVersion = current

	pending, err := pendingMigrationNames(db, current)
	if err != nil {
		// Pending discovery failure indicates the DB is unreadable in
		// non-trivial ways. Surface as corrupt rather than misreport state.
		report.Status = StatusCorrupt
		report.Error = err.Error()
		report.Hint = hintFor(StatusCorrupt)
		return report, nil
	}
	report.PendingMigrations = pending

	tables, err := listTables(db)
	if err != nil {
		report.Status = StatusCorrupt
		report.Error = err.Error()
		report.Hint = hintFor(StatusCorrupt)
		return report, nil
	}
	report.Tables = tables

	switch {
	case current > BinarySchemaVersion:
		report.Status = StatusForwardIncompatible
		report.Hint = hintFor(StatusForwardIncompatible)
	case current < BinarySchemaVersion:
		report.Status = StatusStalePending
		report.Hint = hintFor(StatusStalePending)
	default:
		report.Status = StatusCurrent
	}
	return report, nil
}

// Repair runs Diagnose, then — if the DB is stale-pending — acquires the
// migration lock, applies pending migrations via runMigrationsLocked, and
// re-diagnoses. Forward-incompatible, missing, and corrupt DBs are returned
// unchanged (repair refuses).
func Repair(historyDBPath string) (DoctorReport, error) {
	pre, err := Diagnose(historyDBPath)
	if err != nil {
		return pre, err
	}
	switch pre.Status {
	case StatusForwardIncompatible, StatusMissing, StatusCorrupt:
		return pre, nil
	case StatusCurrent:
		return pre, nil
	}

	// Stale-pending: open the DB, run the locked migration, then re-diagnose.
	db, err := sql.Open("sqlite", historyDBPath)
	if err != nil {
		pre.Status = StatusCorrupt
		pre.Error = err.Error()
		pre.Hint = hintFor(StatusCorrupt)
		return pre, nil
	}
	defer db.Close()

	if _, err := db.Exec(SchemaMigrationsTable); err != nil {
		pre.Status = StatusCorrupt
		pre.Error = err.Error()
		pre.Hint = hintFor(StatusCorrupt)
		return pre, nil
	}

	if err := runMigrationsLocked(historyDBPath, db); err != nil {
		// Forward-incompat would have surfaced already in Diagnose; any other
		// error here means migration failed. Re-diagnose to capture the
		// resulting state (likely still stale).
		post, _ := Diagnose(historyDBPath)
		post.AppliedThisRun = []string{}
		post.Error = err.Error()
		if post.Hint == "" {
			post.Hint = "inspect the previous error and retry; logs in stderr name the failing migration."
		}
		return post, nil
	}

	post, derr := Diagnose(historyDBPath)
	if derr != nil {
		return post, derr
	}
	// Anything in pre.PendingMigrations that is now absent from post was applied.
	post.AppliedThisRun = subtract(pre.PendingMigrations, post.PendingMigrations)
	return post, nil
}

// readSchemaVersionIfPresent returns the stored schema version, or 0 if the
// table is absent. Distinct from readSchemaVersion which assumes the table
// has already been ensured.
func readSchemaVersionIfPresent(db *sql.DB) (int, error) {
	var name string
	row := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = 'schema_version'`)
	switch err := row.Scan(&name); err {
	case sql.ErrNoRows:
		return 0, nil
	case nil:
		// fall through
	default:
		return 0, err
	}
	return readSchemaVersion(db)
}

// pendingMigrationNames returns embedded migration basenames whose version
// integer is greater than current. Ordering matches application order.
func pendingMigrationNames(db *sql.DB, current int) ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	type entry struct {
		version int
		name    string
	}
	var all []entry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, name, err := parseMigrationFileName(e.Name())
		if err != nil {
			return nil, err
		}
		all = append(all, entry{v, name})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].version < all[j].version })

	out := []string{}
	for _, e := range all {
		if e.version > current {
			out = append(out, e.name)
		}
	}
	return out, nil
}

// listTables returns every user table in the DB with its row count.
func listTables(db *sql.DB) ([]TableRow, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]TableRow, 0, len(names))
	for _, n := range names {
		var count int64
		// Identifier is whitelisted by sqlite_master; quote defensively.
		if err := db.QueryRow(`SELECT COUNT(*) FROM "` + n + `"`).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s: %w", n, err)
		}
		out = append(out, TableRow{Name: n, RowCount: count})
	}
	return out, nil
}

func subtract(a, b []string) []string {
	bset := make(map[string]struct{}, len(b))
	for _, s := range b {
		bset[s] = struct{}{}
	}
	out := []string{}
	for _, s := range a {
		if _, ok := bset[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}
