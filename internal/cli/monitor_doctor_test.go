package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
)

// writeDoctorConfig writes a minimal valid sentinel.yaml pointing at dbPath.
func writeDoctorConfig(t *testing.T, dir, dbPath string) string {
	t.Helper()
	t.Setenv("DUMMY_PW", "x")
	cfg := `version: "1.0"
history_db_path: ` + dbPath + `
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
		t.Fatal(err)
	}
	return p
}

func stampDoctorDB(t *testing.T, dbPath string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(monitor.SchemaVersionTable); err != nil {
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

// runDoctor drives `monitor doctor` through RootCmd and returns (err, stdout, stderr-ish combined).
// Resets the doctor cmd's flags so subtests don't inherit state from prior runs.
func runDoctor(t *testing.T, args ...string) (error, string) {
	t.Helper()
	_ = monitorDoctorCmd.Flags().Set("repair", "false")
	_ = monitorDoctorCmd.Flags().Set("json", "false")
	buf := &bytes.Buffer{}
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs(args)
	err := Execute()
	return err, buf.String()
}

func TestMonitorDoctor_JSON_SchemaShape(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")
	// Build a current DB via NewMonitor so schema is fully present.
	mon, err := monitor.NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	_ = mon.Close()

	cfg := writeDoctorConfig(t, dir, dbPath)
	err, out := runDoctor(t, "monitor", "doctor", "--json", "--config", cfg)
	if err != nil {
		t.Fatalf("doctor returned err: %v\nout:\n%s", err, out)
	}

	// Extract JSON: stdout may carry the cobra header lines on misconfig,
	// but on success the buffer holds only the JSON object.
	var rep map[string]any
	if err := json.Unmarshal(bytes.TrimSpace([]byte(out)), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, out)
	}

	// Required fields per contracts/monitor-doctor.json.schema.json.
	required := []string{"database_path", "status", "current_version", "required_version", "pending_migrations", "tables"}
	for _, k := range required {
		if _, ok := rep[k]; !ok {
			t.Errorf("missing required field %q in %v", k, rep)
		}
	}
	if rep["status"] != "current" {
		t.Errorf("status = %v; want current", rep["status"])
	}
	// pending_migrations must be array (possibly empty).
	if _, ok := rep["pending_migrations"].([]any); !ok {
		t.Errorf("pending_migrations not array: %T", rep["pending_migrations"])
	}
	// tables must be array of {name, row_count}.
	tables, _ := rep["tables"].([]any)
	for _, item := range tables {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("table item not object: %T", item)
		}
		if _, ok := row["name"].(string); !ok {
			t.Errorf("table.name missing/non-string: %v", row)
		}
		if _, ok := row["row_count"].(float64); !ok {
			t.Errorf("table.row_count missing/non-number: %v", row)
		}
	}
}

// TestMonitorDoctor_ExitCodes walks the (Status, --repair?) → exit code matrix
// from data-model.md.
func TestMonitorDoctor_ExitCodes(t *testing.T) {
	type tcase struct {
		name      string
		setup     func(t *testing.T, dbPath string)
		repair    bool
		wantCode  int
		wantErrIs error
	}
	cases := []tcase{
		{
			name: "missing/inspect = 3",
			setup: func(t *testing.T, _ string) {
				// no file
			},
			wantCode:  3,
			wantErrIs: ErrDoctorMissing,
		},
		{
			name: "missing/repair = 3",
			setup: func(t *testing.T, _ string) {
				// no file
			},
			repair:    true,
			wantCode:  3,
			wantErrIs: ErrDoctorMissing,
		},
		{
			name: "corrupt/inspect = 4",
			setup: func(t *testing.T, dbPath string) {
				if err := os.WriteFile(dbPath, []byte("garbage"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantCode:  4,
			wantErrIs: ErrDoctorCorrupt,
		},
		{
			name: "forward-incompat/inspect = 2",
			setup: func(t *testing.T, dbPath string) {
				stampDoctorDB(t, dbPath, monitor.BinarySchemaVersion+3)
			},
			wantCode:  2,
			wantErrIs: ErrDoctorForwardIncompat,
		},
		{
			name: "forward-incompat/repair = 2",
			setup: func(t *testing.T, dbPath string) {
				stampDoctorDB(t, dbPath, monitor.BinarySchemaVersion+3)
			},
			repair:    true,
			wantCode:  2,
			wantErrIs: ErrDoctorForwardIncompat,
		},
		{
			name: "current/inspect = 0",
			setup: func(t *testing.T, dbPath string) {
				mon, err := monitor.NewMonitor(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				_ = mon.Close()
			},
			wantCode: 0,
		},
		{
			name: "stale-pending/inspect = 1",
			setup: func(t *testing.T, dbPath string) {
				mon, err := monitor.NewMonitor(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				_ = mon.Close()
				stampDoctorDB(t, dbPath, monitor.BinarySchemaVersion-1)
			},
			wantCode:  1,
			wantErrIs: ErrDoctorStalePending,
		},
		{
			name: "stale-pending/repair = 0",
			setup: func(t *testing.T, dbPath string) {
				mon, err := monitor.NewMonitor(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				_ = mon.Close()
				stampDoctorDB(t, dbPath, monitor.BinarySchemaVersion-1)
			},
			repair:   true,
			wantCode: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "history.db")
			tc.setup(t, dbPath)
			cfg := writeDoctorConfig(t, dir, dbPath)
			args := []string{"monitor", "doctor", "--config", cfg}
			if tc.repair {
				args = append(args, "--repair")
			}
			err, out := runDoctor(t, args...)
			if got := Code(err); got != tc.wantCode {
				t.Errorf("Code = %d; want %d (err=%v)\nout:\n%s", got, tc.wantCode, err, out)
			}
			if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
				t.Errorf("errors.Is(_, %v) = false; got %v", tc.wantErrIs, err)
			}
			if tc.wantErrIs == nil && err != nil {
				t.Errorf("expected nil err; got %v", err)
			}
		})
	}
}
