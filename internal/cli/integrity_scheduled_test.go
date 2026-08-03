package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// mockDispatcher is a ports.Dispatcher test fake that records Notify calls and
// optionally returns a delivery error, for the scheduled-integrity notify
// matrix.
type mockDispatcher struct {
	calls   []*ports.BackupContext
	failErr error
}

func (m *mockDispatcher) Notify(b *ports.BackupContext) error {
	m.calls = append(m.calls, b)
	return m.failErr
}
func (m *mockDispatcher) NotifyAsync(b *ports.BackupContext) <-chan error {
	ch := make(chan error, 1)
	ch <- m.Notify(b)
	close(ch)
	return ch
}
func (m *mockDispatcher) NotifyRestore(*ports.RestoreContext) error { return nil }
func (m *mockDispatcher) NotifyRestoreAsync(*ports.RestoreContext) <-chan error {
	ch := make(chan error, 1)
	close(ch)
	return ch
}
func (m *mockDispatcher) AddWebhookNotifier(*ports.WebhookNotificationConfig) error { return nil }
func (m *mockDispatcher) AddEmailNotifier(*ports.EmailNotificationConfig) error     { return nil }
func (m *mockDispatcher) Clear()                                                    {}
func (m *mockDispatcher) Count() int                                                { return 0 }
func (m *mockDispatcher) SetTimeout(time.Duration)                                  {}

// swapIntegrityDispatcher points newIntegrityDispatcher at a fixed fake for the
// duration of a test.
func swapIntegrityDispatcher(t *testing.T, d ports.Dispatcher) {
	t.Helper()
	prev := newIntegrityDispatcher
	newIntegrityDispatcher = func(_ []config.NotificationChannel) (ports.Dispatcher, error) {
		return d, nil
	}
	t.Cleanup(func() { newIntegrityDispatcher = prev })
}

// openSweepHistoryDB opens the history DB behind a sweep config directly (the
// Monitor does not expose an integrity read API — one new port method only).
func openSweepHistoryDB(t *testing.T, cfgPath string) *sql.DB {
	t.Helper()
	path, err := readSweepConfig(t, cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open history db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// loadSweepConfigAndMonitor loads a sweep config + opens its monitor.
func loadSweepConfigAndMonitor(t *testing.T, cfgPath string) (*config.Configuration, *monitor.Monitor) {
	t.Helper()
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		t.Fatalf("open monitor: %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })
	return cfg, mon
}

// TestScheduledIntegrityCheck_DirectInvoke_RecordsRun exercises the registered
// job body directly (the controllable-invoke stand-in for a cron tick): it runs
// the shared sweep over a seeded repo and records one grouped, scheduled-
// triggered run with the correct per-artifact verdicts (US1 + US2).
func TestScheduledIntegrityCheck_DirectInvoke_RecordsRun(t *testing.T) {
	cfgPath, ids := seedSweepRepo(t, []sweepSpec{
		{file: "ok.sql", seedArtifact: true, seedManifest: true},
		{file: "corrupt.sql", seedArtifact: true, seedManifest: true, manifestHash: "0000000000000000000000000000000000000000000000000000000000000000"},
		{file: "nomanifest.sql", seedArtifact: true, seedManifest: false},
	})
	cfg, mon := loadSweepConfigAndMonitor(t, cfgPath)

	if err := runScheduledIntegrityCheck(context.Background(), cfg, mon, config.IntegrityScheduledCheck{NotifyOn: "never"}); err != nil {
		t.Fatalf("runScheduledIntegrityCheck: %v", err)
	}

	db := openSweepHistoryDB(t, cfgPath)
	rows, err := db.Query(`SELECT backup_id, result, trigger, storage_backend FROM integrity_checks`)
	if err != nil {
		t.Fatalf("query integrity_checks: %v", err)
	}
	defer rows.Close()

	resultByID := map[string]string{}
	var runTriggers, backends = map[string]struct{}{}, map[string]struct{}{}
	for rows.Next() {
		var backupID, result, trigger, backend string
		if err := rows.Scan(&backupID, &result, &trigger, &backend); err != nil {
			t.Fatalf("scan: %v", err)
		}
		resultByID[backupID] = result
		runTriggers[trigger] = struct{}{}
		backends[backend] = struct{}{}
	}
	if len(resultByID) != 3 {
		t.Fatalf("recorded rows = %d, want 3 (%v)", len(resultByID), resultByID)
	}
	if resultByID[ids["ok.sql"]] != "ok" ||
		resultByID[ids["corrupt.sql"]] != "corrupted" ||
		resultByID[ids["nomanifest.sql"]] != "missing_manifest" {
		t.Fatalf("verdict mismatch: %v", resultByID)
	}
	if _, ok := runTriggers["scheduled"]; !ok || len(runTriggers) != 1 {
		t.Fatalf("trigger label wrong: %v", runTriggers)
	}
	if _, ok := backends["s3"]; !ok {
		t.Fatalf("storage_backend not recorded: %v", backends)
	}

	// All rows share one run_id (grouped run).
	var distinctRuns int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT run_id) FROM integrity_checks`).Scan(&distinctRuns); err != nil {
		t.Fatalf("count distinct run_id: %v", err)
	}
	if distinctRuns != 1 {
		t.Fatalf("distinct run_id = %d, want 1", distinctRuns)
	}
}

// TestScheduledIntegrityCheck_EmptyRepo asserts a sweep over a repo with no
// eligible backups records zero rows and is not a failure.
func TestScheduledIntegrityCheck_EmptyRepo(t *testing.T) {
	cfgPath, _ := seedSweepRepo(t, nil)
	cfg, mon := loadSweepConfigAndMonitor(t, cfgPath)

	if err := runScheduledIntegrityCheck(context.Background(), cfg, mon, config.IntegrityScheduledCheck{}); err != nil {
		t.Fatalf("empty sweep should not error, got: %v", err)
	}
	db := openSweepHistoryDB(t, cfgPath)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM integrity_checks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}

// TestScheduledIntegrityCheck_NotifyMatrix asserts the notify_on semantics:
// failure+corruption→dispatch; clean+failure→silent; always+clean→
// dispatch; never+corruption→silent; a dispatcher delivery error is swallowed
// as a warning while the run is still recorded.
func TestScheduledIntegrityCheck_NotifyMatrix(t *testing.T) {
	cleanSpecs := []sweepSpec{{file: "ok.sql", seedArtifact: true, seedManifest: true}}
	corruptSpecs := []sweepSpec{
		{file: "ok.sql", seedArtifact: true, seedManifest: true},
		{file: "bad.sql", seedArtifact: true, seedManifest: true, manifestHash: "0000000000000000000000000000000000000000000000000000000000000000"},
	}

	cases := []struct {
		name         string
		specs        []sweepSpec
		notifyOn     string
		dispErr      error
		wantDispatch bool
		wantStatus   ports.BackupStatus
	}{
		{"failure+corruption dispatches", corruptSpecs, "failure", nil, true, ports.NotifyStatusFailure},
		{"failure+clean silent", cleanSpecs, "failure", nil, false, ""},
		{"default(empty)+corruption dispatches", corruptSpecs, "", nil, true, ports.NotifyStatusFailure},
		{"always+clean dispatches success", cleanSpecs, "always", nil, true, ports.NotifyStatusSuccess},
		{"always+corruption dispatches failure", corruptSpecs, "always", nil, true, ports.NotifyStatusFailure},
		{"never+corruption silent", corruptSpecs, "never", nil, false, ""},
		{"dispatcher error swallowed", corruptSpecs, "failure", errTestDispatch, true, ports.NotifyStatusFailure},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgPath, _ := seedSweepRepo(t, tc.specs)
			cfg, mon := loadSweepConfigAndMonitor(t, cfgPath)

			md := &mockDispatcher{failErr: tc.dispErr}
			swapIntegrityDispatcher(t, md)

			err := runScheduledIntegrityCheck(context.Background(), cfg, mon, config.IntegrityScheduledCheck{NotifyOn: tc.notifyOn})
			if err != nil {
				t.Fatalf("runScheduledIntegrityCheck returned error (must swallow delivery failures): %v", err)
			}

			if tc.wantDispatch {
				if len(md.calls) != 1 {
					t.Fatalf("dispatch count = %d, want 1", len(md.calls))
				}
				if got := md.calls[0].Status; got != tc.wantStatus {
					t.Fatalf("notify status = %q, want %q", got, tc.wantStatus)
				}
				if md.calls[0].BackupName == "" || md.calls[0].BackupName[:10] != "integrity:" {
					t.Fatalf("notify BackupName = %q, want integrity:<runID>", md.calls[0].BackupName)
				}
			} else if len(md.calls) != 0 {
				t.Fatalf("dispatch count = %d, want 0 (silent)", len(md.calls))
			}

			// The run is always recorded, even when delivery failed.
			db := openSweepHistoryDB(t, cfgPath)
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM integrity_checks`).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != len(tc.specs) {
				t.Fatalf("recorded rows = %d, want %d", n, len(tc.specs))
			}
		})
	}
}

var errTestDispatch = errTest("dispatch boom")

type errTest string

func (e errTest) Error() string { return string(e) }

// TestScheduleList_RegistersIntegrityJob asserts the scheduler registers the
// reserved __integrity_check job (labelled "integrity") when the scheduled
// check is enabled, and omits it when disabled (US1 additive registration).
func TestScheduleList_RegistersIntegrityJob(t *testing.T) {
	t.Setenv("PG_PASSWORD", "x")
	enabled := scheduleListJSON(t, integrityConfigYAML(t, true, "0 3 * * 0"))
	if row := findRow(enabled, config.IntegrityCheckJobName); row == nil {
		t.Fatalf("enabled config: __integrity_check not registered; rows=%v", enabled)
	} else if row.Type != "integrity" {
		t.Fatalf("integrity job labelled %q, want integrity", row.Type)
	}

	disabled := scheduleListJSON(t, integrityConfigYAML(t, false, ""))
	if row := findRow(disabled, config.IntegrityCheckJobName); row != nil {
		t.Fatalf("disabled config: __integrity_check must NOT be registered; got %v", row)
	}
	// backup job registration is unaffected either way.
	if findRow(disabled, "pg-job") == nil {
		t.Fatalf("backup job pg-job missing from listing: %v", disabled)
	}
}

// integrityConfigYAML writes a minimal valid config with a backup job and an
// integrity.scheduled_check block toggled per the args.
func integrityConfigYAML(t *testing.T, enabled bool, cron string) string {
	t.Helper()
	dir := t.TempDir()
	sc := "    enabled: false\n"
	if enabled {
		sc = "    enabled: true\n    cron: \"" + cron + "\"\n"
	}
	content := `version: "1.0"
history_db_path: ` + dir + `/history.db
defaults:
  storage:
    type: local
    local_path: ./backups
databases:
  pg-job:
    type: postgres
    host: localhost
    username: sentinel
    password_env: PG_PASSWORD
    database: app
    schedule: "0 2 * * *"
integrity:
  scheduled_check:
` + sc
	return writeYAML(t, dir, content)
}

func scheduleListJSON(t *testing.T, cfgPath string) []scheduleListRow {
	t.Helper()
	buf := &bytes.Buffer{}
	scheduleListCmd.SetOut(buf)
	scheduleListCmd.SetErr(buf)
	if err := scheduleListCmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set config flag: %v", err)
	}
	if err := scheduleListCmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format flag: %v", err)
	}
	if err := scheduleListCmd.RunE(scheduleListCmd, nil); err != nil {
		t.Fatalf("schedule list: %v", err)
	}
	var rows []scheduleListRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("parse schedule list json: %v\n%s", err, buf.String())
	}
	return rows
}

func findRow(rows []scheduleListRow, name string) *scheduleListRow {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}
