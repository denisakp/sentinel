package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRestoreEnabledStateConfig writes a config with one restore job whose enabled state
// the caller chooses. The backup source deliberately points at a path that does
// not exist: if the job is refused as it should be, that never matters, and if
// it is not, the run gets far enough to prove it.
func writeRestoreEnabledStateConfig(t *testing.T, enabled bool) string {
	t.Helper()
	t.Setenv("SENTINEL_TEST_RESTORE_PW", "pw")
	dir := t.TempDir()
	body := `version: "1.0"
history_db_path: ` + filepath.Join(dir, "history.db") + `
defaults:
  output_dir: ` + dir + `
databases:
  src:
    type: postgres
    host: localhost
    port: 5432
    database: src
    username: src
    password_env: SENTINEL_TEST_RESTORE_PW
    enabled: true
    storage:
      type: local
      local_path: ` + dir + `
restores:
  nightly:
    type: postgres
    database: target
    host: localhost
    port: 5432
    username: src
    password_env: SENTINEL_TEST_RESTORE_PW
    schedule: "0 3 * * *"
    enabled: ` + map[bool]string{true: "true", false: "false"}[enabled] + `
    backup_source:
      type: local
      local_path: ` + filepath.Join(dir, "nonexistent-backups") + `
      backup_path: nightly.sql
`
	path := filepath.Join(dir, "sentinel.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

// TestRestoreRunRefusesDisabledJob is the regression guard for #139.
//
// `enabled` gated `restore run --all` and the scheduler, and not the single-job
// path. A job explicitly marked `enabled: false` passed validation, ACQUIRED THE
// LOCK, and reached the staging step; only a nonexistent backup path stopped it.
// With a real source it would have restored. For an operation that overwrites a
// database, `enabled: false` has to mean it will not run.
//
// The error must arrive before anything is locked or opened, which is why the
// check sits in the handler rather than inside the execution path.
func TestRestoreRunRefusesDisabledJob(t *testing.T) {
	cfgPath := writeRestoreEnabledStateConfig(t, false)
	prev := restoreConfigFile
	restoreConfigFile = cfgPath
	t.Cleanup(func() { restoreConfigFile = prev })

	err := handleRestoreRun(nil, []string{"nightly"})
	if err == nil {
		t.Fatal("restore run executed a job marked enabled: false")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("the refusal does not say the job is disabled: %v", err)
	}
	if !strings.Contains(err.Error(), "enabled: true") {
		t.Errorf("the refusal does not tell the operator how to allow the job: %v", err)
	}

	// Nothing may have been staged or locked. A lock file for this job would mean
	// the refusal came too late to matter.
	dir := filepath.Dir(cfgPath)
	if entries, readErr := os.ReadDir(dir); readErr == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".lock") {
				t.Errorf("a lock file was created for a job that was refused: %s", e.Name())
			}
		}
	}
}

// TestRestoreRunAcceptsEnabledJob: the guard must refuse the disabled job only.
// An enabled job still reaches execution, where it fails on its nonexistent
// backup source, which is a different error and the proof that the check let it
// through.
func TestRestoreRunAcceptsEnabledJob(t *testing.T) {
	cfgPath := writeRestoreEnabledStateConfig(t, true)
	prev := restoreConfigFile
	restoreConfigFile = cfgPath
	t.Cleanup(func() { restoreConfigFile = prev })

	err := handleRestoreRun(nil, []string{"nightly"})
	if err == nil {
		t.Skip("the enabled job unexpectedly succeeded; nothing to assert about the refusal path")
	}
	if strings.Contains(err.Error(), "is disabled") {
		t.Errorf("an enabled job was refused as disabled: %v", err)
	}
	_ = context.Background()
}
