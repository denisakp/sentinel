package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const minimalValidConfig = `
version: "1.0"
max_concurrent_backups: 1
history_db_path: ./test-history.db
defaults:
  storage:
    type: local
    local_path: ./backups
databases:
  noop-job:
    type: postgres
    enabled: false
    host: localhost
    username: sentinel
    password_env: TEST_DB_PASSWORD
    database: sentinel
    storage:
      type: local
      local_path: ./backups
`

func writeDefaultConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "sentinel-config.yaml")
	if err := os.WriteFile(path, []byte(minimalValidConfig), 0644); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	return path
}

func runCLI(t *testing.T, workDir string, args ...string) (stdout, stderr string, exitErr error) {
	t.Helper()
	bin := buildSentinel(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "TEST_DB_PASSWORD=dummy-password")

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	exitErr = cmd.Run()
	return outBuf.String(), errBuf.String(), exitErr
}

func TestDefaultConfig_BackupDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeDefaultConfig(t, dir)

	_, stderr, err := runCLI(t, dir, "backup")
	if err != nil && strings.Contains(stderr, "--config") {
		t.Errorf("backup command asked for --config while default file exists; stderr=%q", stderr)
	}
	if strings.Contains(stderr, "no configuration file found") {
		t.Errorf("unexpected missing-config error; stderr=%q", stderr)
	}
}

func TestDefaultConfig_MonitorListDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeDefaultConfig(t, dir)

	_, stderr, _ := runCLI(t, dir, "monitor", "list")
	if strings.Contains(stderr, "--config is required") {
		t.Errorf("monitor list asked for --config while default file exists; stderr=%q", stderr)
	}
}

func TestDefaultConfig_ScheduleListDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeDefaultConfig(t, dir)

	_, stderr, _ := runCLI(t, dir, "schedule", "list")
	if strings.Contains(stderr, "--config is required") {
		t.Errorf("schedule list asked for --config while default file exists; stderr=%q", stderr)
	}
}

func TestDefaultConfig_RetentionPreviewDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeDefaultConfig(t, dir)

	_, stderr, _ := runCLI(t, dir, "retention", "preview")
	if strings.Contains(stderr, "--config is required") {
		t.Errorf("retention preview asked for --config while default file exists; stderr=%q", stderr)
	}
}

func TestDefaultConfig_RestoreListDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeDefaultConfig(t, dir)

	_, stderr, _ := runCLI(t, dir, "restore", "list")
	if strings.Contains(stderr, "--config") && strings.Contains(stderr, "required") {
		t.Errorf("restore list asked for --config while default file exists; stderr=%q", stderr)
	}
}

func TestExplicitOverride_BackupAndMonitor(t *testing.T) {
	dir := t.TempDir()
	writeDefaultConfig(t, dir)

	altPath := filepath.Join(dir, "alt.yaml")
	if err := os.WriteFile(altPath, []byte(minimalValidConfig+"\n# alternate\n"), 0644); err != nil {
		t.Fatalf("write alt config: %v", err)
	}

	_, stderr, err := runCLI(t, dir, "backup", "--config", altPath)
	if err != nil && strings.Contains(stderr, "no configuration file found") {
		t.Errorf("backup with explicit --config failed with missing-config; stderr=%q", stderr)
	}

	_, stderr2, _ := runCLI(t, dir, "monitor", "list", "--config", altPath)
	if strings.Contains(stderr2, "--config is required") {
		t.Errorf("monitor list with explicit --config failed; stderr=%q", stderr2)
	}
}

func TestExplicitOverride_ScheduleAndRestore(t *testing.T) {
	dir := t.TempDir()
	writeDefaultConfig(t, dir)

	altPath := filepath.Join(dir, "alt.yaml")
	if err := os.WriteFile(altPath, []byte(minimalValidConfig), 0644); err != nil {
		t.Fatalf("write alt config: %v", err)
	}

	_, stderr, _ := runCLI(t, dir, "schedule", "list", "--config", altPath)
	if strings.Contains(stderr, "--config is required") {
		t.Errorf("schedule list with explicit --config failed; stderr=%q", stderr)
	}

	_, stderr2, _ := runCLI(t, dir, "restore", "list", "--config", altPath)
	if strings.Contains(stderr2, "--config") && strings.Contains(stderr2, "required") {
		t.Errorf("restore list with explicit --config failed; stderr=%q", stderr2)
	}
}

func TestMissingConfig_MonitorAndSchedule(t *testing.T) {
	dir := t.TempDir()

	_, stderr, err := runCLI(t, dir, "monitor", "list")
	if err == nil {
		t.Error("expected non-zero exit when no config found")
	}
	if !strings.Contains(stderr+err.Error(), "sentinel-config.yaml") {
		t.Errorf("error does not mention searched path; output=%q", stderr)
	}

	_, _, err2 := runCLI(t, dir, "schedule", "list")
	if err2 == nil {
		t.Error("expected non-zero exit when no config found")
	}
}

func TestMissingConfig_BackupAndRestore(t *testing.T) {
	dir := t.TempDir()

	_, _, err := runCLI(t, dir, "restore", "list")
	if err == nil {
		t.Error("expected non-zero exit for restore list with no config")
	}

	_, _, err2 := runCLI(t, dir, "backup")
	if err2 == nil {
		t.Error("expected non-zero exit for backup with no config and no --type")
	}
}
