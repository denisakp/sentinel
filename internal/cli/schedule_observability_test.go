package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/monitor"
	internalrestore "github.com/denisakp/sentinel/internal/restore"
	"github.com/denisakp/sentinel/internal/scheduler"
	"github.com/spf13/cobra"
)

func newScheduleStatusCommandForContractTest() *cobra.Command {
	return scheduleStatusCmd
}

func writeStatusContractConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sentinel.yaml")
	content := `version: "1.0"
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
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestScheduleStatusUsageIncludesRequiredJobName(t *testing.T) {
	cmd := newScheduleStatusCommandForContractTest()
	if !strings.Contains(cmd.Use, "status <job-name>") {
		t.Fatalf("expected status command use to include required job-name, got %q", cmd.Use)
	}
}

func TestScheduleStatusHelpContainsValidExample(t *testing.T) {
	cmd := newScheduleStatusCommandForContractTest()
	if !strings.Contains(cmd.Example, "sentinel schedule status postgres-sample --config sentinel.yaml") {
		t.Fatalf("expected status command example to include job name invocation, got %q", cmd.Example)
	}
}

func TestScheduleStatusArgValidationIsExactOne(t *testing.T) {
	cmd := newScheduleStatusCommandForContractTest()
	if cmd.Args == nil {
		t.Fatal("expected status command args validator to be configured")
	}
	if err := cmd.Args(cmd, []string{"job"}); err != nil {
		t.Fatalf("expected one arg to pass validation, got %v", err)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Fatal("expected zero args to fail validation")
	}
	if err := cmd.Args(cmd, []string{"job", "extra"}); err == nil {
		t.Fatal("expected two args to fail validation")
	}
}

func TestScheduleStatusUnknownJobDiffersFromMissingArgValidation(t *testing.T) {
	configPath := writeStatusContractConfig(t)

	cmd := newScheduleStatusCommandForContractTest()
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Fatal("expected missing args to fail validation")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Fatalf("expected cobra arg validation error, got %v", err)
	}

	cmd.Flags().Set("config", configPath)
	err = cmd.RunE(cmd, []string{"unknown-job"})
	if err == nil {
		t.Fatal("expected unknown job execution to fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "job") {
		t.Fatalf("expected unknown-job error context, got %v", err)
	}
	if strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Fatalf("unknown-job path must remain distinct from missing-arg validation, got %v", err)
	}
}

func TestScheduleListTableHeadersAndLastStatusRemoval(t *testing.T) {
	next := time.Date(2026, 3, 12, 2, 0, 0, 0, time.UTC)
	rows := buildScheduleListRows([]scheduler.JobInfo{
		{
			Name:          "prod-postgres",
			ScheduleExpr:  "0 2 * * *",
			NextExecution: next,
			LastStatus:    "failed",
		},
	}, map[string]config.RestoreJob{})

	table := renderScheduleListTable(rows)
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected table output with header and row, got: %q", table)
	}

	headerLine := lines[0]
	for _, col := range []string{"TYPE", "NAME", "SCHEDULE", "NEXT EXECUTION"} {
		if !strings.Contains(headerLine, col) {
			t.Fatalf("schedule list header missing column %q: %q", col, headerLine)
		}
	}
	if strings.Contains(headerLine, "LAST STATUS") {
		t.Fatalf("table header must not include LAST STATUS: %q", headerLine)
	}
}

func TestScheduleListTypeResolutionIncludesRestore(t *testing.T) {
	next := time.Date(2026, 3, 12, 2, 0, 0, 0, time.UTC)
	rows := buildScheduleListRows([]scheduler.JobInfo{
		{Name: "nightly-backup", ScheduleExpr: "0 2 * * *", NextExecution: next},
		{Name: "restore-drill", ScheduleExpr: "0 4 * * 0", NextExecution: next},
	}, map[string]config.RestoreJob{"restore-drill": {Type: "postgres"}})

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Type != "backup" {
		t.Fatalf("expected first row to be backup, got %q", rows[0].Type)
	}
	if rows[1].Type != "restore" {
		t.Fatalf("expected second row to be restore, got %q", rows[1].Type)
	}
}

func TestScheduleListJSONCompatibilityIncludesLastStatus(t *testing.T) {
	next := time.Date(2026, 3, 12, 2, 0, 0, 0, time.UTC)
	rows := buildScheduleListRows([]scheduler.JobInfo{
		{
			Name:          "prod-postgres",
			ScheduleExpr:  "0 2 * * *",
			NextExecution: next,
			LastStatus:    "failed",
		},
	}, map[string]config.RestoreJob{})

	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("failed to marshal json rows: %v", err)
	}
	payload := string(data)
	if !strings.Contains(payload, "\"last_status\":\"failed\"") {
		t.Fatalf("expected last_status in json payload, got %s", payload)
	}
}

func TestExecuteRestoreJob_UsesCLIRestoreRunner(t *testing.T) {
	prevScheduled := runScheduledRestoreExecution
	prevRunner := runRestoreExecution
	t.Cleanup(func() {
		runScheduledRestoreExecution = prevScheduled
		runRestoreExecution = prevRunner
	})

	runnerInvoked := false
	runRestoreExecution = func(ctx context.Context, req *internalrestore.ExecutionRequest) (*internalrestore.ExecutionResult, error) {
		runnerInvoked = true
		return &internalrestore.ExecutionResult{Status: monitor.StatusCompleted}, nil
	}

	runScheduledRestoreExecution = func(
		ctx context.Context,
		cfg *config.Configuration,
		jobName string,
		job config.RestoreJob,
		mon *monitor.Monitor,
		limiter chan struct{},
		runner scheduler.SharedRestoreRunner,
	) (*internalrestore.ExecutionResult, error) {
		_, err := runner(ctx, &internalrestore.ExecutionRequest{})
		if err != nil {
			return nil, err
		}
		return &internalrestore.ExecutionResult{Status: monitor.StatusCompleted}, nil
	}

	err := executeRestoreJob(nil, &config.Configuration{}, nil, config.RestoreJob{Name: "restore-job"}, nil)
	if err != nil {
		t.Fatalf("executeRestoreJob() error = %v, want nil", err)
	}
	if !runnerInvoked {
		t.Fatal("expected runRestoreExecution to be used by scheduled restore path")
	}
}
