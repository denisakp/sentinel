package scheduler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/retention"
	"github.com/denisakp/sentinel/internal/scheduler"
	"github.com/denisakp/sentinel/internal/utils"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func buildSentinelBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binary := "sentinel"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	out := filepath.Join(dir, binary)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = repoRoot(t)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build sentinel binary: %v\n%s", err, stderr.String())
	}
	return out
}

func runSentinelCmd(t *testing.T, bin string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return out.String(), exitErr.ExitCode()
	}
	return out.String(), -1
}

func runScheduleStatusHelp(t *testing.T, bin, configPath string) (string, int) {
	t.Helper()
	return runSentinelCmd(t, bin, []string{"PG_PASSWORD=secret"}, "schedule", "status", "--help", "--config", configPath)
}

func assertOutputContainsAll(t *testing.T, output string, expected ...string) {
	t.Helper()
	for _, token := range expected {
		if !strings.Contains(output, token) {
			t.Fatalf("expected output to contain %q, got: %s", token, output)
		}
	}
}

func assertUniqueNames(t *testing.T, names ...string) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, name := range names {
		if _, ok := seen[name]; ok {
			t.Fatalf("expected unique names, got duplicate %q", name)
		}
		seen[name] = struct{}{}
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sentinel.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func writeConfigFromFixture(t *testing.T, fixtureName string) string {
	t.Helper()
	fixturePath := filepath.Join(repoRoot(t), "tests", "scheduler", "fixtures", fixtureName)
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}
	return writeConfigFile(t, string(content))
}

func parseJSONRowsFromCommandOutput(t *testing.T, output string) []map[string]any {
	t.Helper()
	idx := strings.Index(output, "[")
	if idx == -1 {
		t.Fatalf("json payload not found in output: %s", output)
	}

	var rows []map[string]any
	if err := json.Unmarshal([]byte(output[idx:]), &rows); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput=%s", err, output)
	}
	return rows
}

func TestScheduleListShowsInheritedDefaultSchedule(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFile(t, `version: "1.0"
defaults:
  schedule: "*/10 * * * *"
  storage:
    type: local
    local_path: ./backups
databases:
  inherited-job:
    type: postgres
    host: localhost
    username: sentinel
    password_env: PG_PASSWORD
    database: app
`)

	out, code := runSentinelCmd(t, bin, []string{"PG_PASSWORD=secret"}, "schedule", "list", "--config", configPath, "--format", "json")
	if code != 0 {
		t.Fatalf("schedule list exited %d: %s", code, out)
	}

	rows := parseJSONRowsFromCommandOutput(t, out)
	if len(rows) != 1 {
		t.Fatalf("expected 1 scheduled row, got %d (%s)", len(rows), out)
	}
	if got := rows[0]["schedule"]; got != "*/10 * * * *" {
		t.Fatalf("schedule = %v, want */10 * * * *", got)
	}
}

func TestScheduleListExplicitScheduleOverridesDefault(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFile(t, `version: "1.0"
defaults:
  schedule: "*/10 * * * *"
  storage:
    type: local
    local_path: ./backups
databases:
  override-job:
    type: postgres
    host: localhost
    username: sentinel
    password_env: PG_PASSWORD
    database: app
    schedule: "0 * * * *"
`)

	out, code := runSentinelCmd(t, bin, []string{"PG_PASSWORD=secret"}, "schedule", "list", "--config", configPath, "--format", "json")
	if code != 0 {
		t.Fatalf("schedule list exited %d: %s", code, out)
	}

	rows := parseJSONRowsFromCommandOutput(t, out)
	if len(rows) != 1 {
		t.Fatalf("expected 1 scheduled row, got %d (%s)", len(rows), out)
	}
	if got := rows[0]["schedule"]; got != "0 * * * *" {
		t.Fatalf("schedule = %v, want 0 * * * *", got)
	}
}

func TestScheduleListJSONOutputContractStable(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFile(t, `version: "1.0"
defaults:
  schedule: "*/10 * * * *"
  storage:
    type: local
    local_path: ./backups
databases:
  contract-job:
    type: postgres
    host: localhost
    username: sentinel
    password_env: PG_PASSWORD
    database: app
`)

	out, code := runSentinelCmd(t, bin, []string{"PG_PASSWORD=secret"}, "schedule", "list", "--config", configPath, "--format", "json")
	if code != 0 {
		t.Fatalf("schedule list exited %d: %s", code, out)
	}

	rows := parseJSONRowsFromCommandOutput(t, out)
	if len(rows) == 0 {
		t.Fatalf("expected at least one row in schedule list output")
	}

	for _, key := range []string{"type", "name", "schedule", "next_execution", "last_status"} {
		if _, ok := rows[0][key]; !ok {
			t.Fatalf("expected JSON key %q in row: %v", key, rows[0])
		}
	}
}

func TestScheduleStatusOutputContractStable(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFile(t, `version: "1.0"
defaults:
  schedule: "*/10 * * * *"
  storage:
    type: local
    local_path: ./backups
databases:
  status-job:
    type: postgres
    host: localhost
    username: sentinel
    password_env: PG_PASSWORD
    database: app
`)

	out, code := runSentinelCmd(t, bin, []string{"PG_PASSWORD=secret"}, "schedule", "status", "status-job", "--config", configPath)
	if code != 0 {
		t.Fatalf("schedule status exited %d: %s", code, out)
	}

	for _, expected := range []string{"Job:", "Schedule:", "Next Execution:", "Last Execution:", "Last Status:"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("status output missing %q: %s", expected, out)
		}
	}
}

func TestScheduleStatusHelpShowsRequiredJobNameInUsage(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFromFixture(t, "schedule_status_contract.yaml")

	out, code := runScheduleStatusHelp(t, bin, configPath)
	if code != 0 {
		t.Fatalf("schedule status help exited %d: %s", code, out)
	}
	assertOutputContainsAll(t, out, "Usage:", "status <job-name>")
}

func TestScheduleStatusHelpShowsExampleWithJobName(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFromFixture(t, "schedule_status_contract.yaml")

	out, code := runScheduleStatusHelp(t, bin, configPath)
	if code != 0 {
		t.Fatalf("schedule status help exited %d: %s", code, out)
	}
	assertOutputContainsAll(t, out, "sentinel schedule status postgres-sample --config sentinel.yaml")
}

func TestScheduleStatusMissingJobFailsViaArgValidation(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFromFixture(t, "schedule_status_contract.yaml")

	out, code := runSentinelCmd(t, bin, []string{"PG_PASSWORD=secret"}, "schedule", "status", "--config", configPath)
	if code == 0 {
		t.Fatalf("expected missing job name to fail, output: %s", out)
	}
	assertOutputContainsAll(t, out, "accepts 1 arg(s), received 0")
}

func TestScheduleStatusExtraArgFailsViaArgValidation(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFromFixture(t, "schedule_status_contract.yaml")

	out, code := runSentinelCmd(t, bin, []string{"PG_PASSWORD=secret"}, "schedule", "status", "postgres-sample", "extra", "--config", configPath)
	if code == 0 {
		t.Fatalf("expected extra arg invocation to fail, output: %s", out)
	}
	assertOutputContainsAll(t, out, "accepts 1 arg(s), received 2")
}

func TestScheduleStatusValidOneArgInvocationSucceeds(t *testing.T) {
	bin := buildSentinelBinary(t)
	configPath := writeConfigFromFixture(t, "schedule_status_contract.yaml")

	out, code := runSentinelCmd(t, bin, []string{"PG_PASSWORD=secret"}, "schedule", "status", "postgres-sample", "--config", configPath)
	if code != 0 {
		t.Fatalf("schedule status exited %d: %s", code, out)
	}
	assertOutputContainsAll(t, out, "Job: postgres-sample", "Schedule:", "Next Execution:", "Last Execution:", "Last Status:")
}

func TestAddJobInvalidCron(t *testing.T) {
	s := scheduler.NewScheduler(1)
	if err := s.AddJob("job", "invalid", func() error { return nil }); err == nil {
		t.Fatalf("expected error for invalid cron expression")
	}
}

func TestScheduledOutputFixedPrefixIsUnique(t *testing.T) {
	t.Cleanup(utils.ResetScheduledOutCountersForTest)
	fixed := time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC)

	first := utils.BuildScheduledOutName("postgres-dev", ".sql", "postgres-sample", fixed)
	second := utils.BuildScheduledOutName("postgres-dev", ".sql", "postgres-sample", fixed)

	assertUniqueNames(t, first, second)
	if first != "postgres-dev_2026-03-15T02-00-00.sql" {
		t.Fatalf("unexpected first artifact name: %s", first)
	}
	if second != "postgres-dev_2026-03-15T02-00-00-1.sql" {
		t.Fatalf("unexpected second artifact name: %s", second)
	}
}

func TestScheduledOutputEmptyPrefixUsesDefaultAndIsUnique(t *testing.T) {
	t.Cleanup(utils.ResetScheduledOutCountersForTest)
	restore := utils.SetNowForTest(func() time.Time {
		return time.Date(2026, 3, 15, 2, 10, 0, 0, time.UTC)
	})
	defer restore()

	first := utils.BuildScheduledOutName("", ".sql", "postgres-sample", utils.NowUTC())
	second := utils.BuildScheduledOutName("", ".sql", "postgres-sample", utils.NowUTC())

	if !strings.HasPrefix(first, "SENTINEL_2026-03-15T02-10-00_2026-03-15T02-10-00") {
		t.Fatalf("unexpected default-prefix artifact name: %s", first)
	}
	if !strings.HasSuffix(first, ".sql") || !strings.HasSuffix(second, ".sql") {
		t.Fatalf("expected .sql suffixes, got %q and %q", first, second)
	}
	if first == second {
		t.Fatalf("expected unique artifact names for same-second runs")
	}
}

func TestScheduledOutputStripsCanonicalExtension(t *testing.T) {
	t.Cleanup(utils.ResetScheduledOutCountersForTest)
	fixed := time.Date(2026, 3, 15, 2, 20, 0, 0, time.UTC)

	got := utils.BuildScheduledOutName("postgres-dev.sql", ".sql", "postgres-sample", fixed)
	if got != "postgres-dev_2026-03-15T02-20-00.sql" {
		t.Fatalf("canonical extension stripping mismatch: got %q", got)
	}
	if strings.Contains(got, ".sql_") {
		t.Fatalf("expected no double extension in artifact name: %q", got)
	}
}

func TestRetentionDeleteUnsupportedBackendYieldsWarningPath(t *testing.T) {
	candidates := []retention.BackupCandidate{{FilePath: "gdrive://folder/backup.sql", Timestamp: time.Now().UTC(), Status: "success"}}

	deleted, errs := retention.DeleteCandidates(context.Background(), candidates, "google-drive", config.StorageConfig{})
	if len(deleted) != 0 {
		t.Fatalf("expected no deletions for unsupported backend, got %d", len(deleted))
	}
	if len(errs) == 0 {
		t.Fatal("expected unsupported backend delete error")
	}
	if !strings.Contains(errs[0].Error(), "retention delete not supported") {
		t.Fatalf("unexpected error: %v", errs[0])
	}
}

func TestRetentionDeleteGCSFailureYieldsWarningPath(t *testing.T) {
	candidates := []retention.BackupCandidate{{FilePath: "gs://bucket/backup.sql", Timestamp: time.Now().UTC(), Status: "success"}}

	deleted, errs := retention.DeleteCandidates(context.Background(), candidates, "gcs", config.StorageConfig{})
	if len(deleted) != 0 {
		t.Fatalf("expected no deletions when gcs delete fails, got %d", len(deleted))
	}
	if len(errs) == 0 {
		t.Fatal("expected gcs delete error")
	}
}
