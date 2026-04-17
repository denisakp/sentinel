package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/spf13/cobra"
)

func TestMonitorListHeaderOrderAndIDHandoff(t *testing.T) {
	timestamp := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	executions := []monitor.Execution{
		{
			ID:             "exec-001",
			BackupName:     "prod-postgres",
			BackupType:     "incremental",
			ChainID:        "chain-001",
			ChainIndex:     2,
			DeltaSizeBytes: 2048,
			Status:         "failed",
			Timestamp:      timestamp,
			DurationMs:     1500,
			ErrorMessage:   "dial tcp 10.0.0.1:5432: connect: connection refused after multiple retries",
		},
	}

	cmd := &cobra.Command{}
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := printTable(cmd, executions); err != nil {
		t.Fatalf("printTable returned error: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and at least one row, got: %q", output)
	}

	headers := strings.Fields(lines[0])
	expected := []string{"ID", "JOB", "TYPE", "CHAIN", "STATUS", "TIMESTAMP", "DURATION", "DELTA", "ERROR"}
	if strings.Join(headers, "|") != strings.Join(expected, "|") {
		t.Fatalf("unexpected headers: got %v want %v", headers, expected)
	}

	rowFields := strings.Fields(lines[1])
	if len(rowFields) == 0 || rowFields[0] != "exec-001" {
		t.Fatalf("expected first row to start with execution ID, got %q", lines[1])
	}

	showCmd := &cobra.Command{}
	showBuf := &bytes.Buffer{}
	showCmd.SetOut(showBuf)
	showCmd.SetErr(showBuf)
	printExecution(showCmd, &executions[0])

	if !strings.Contains(showBuf.String(), "ID: exec-001") {
		t.Fatalf("monitor show output should include handed-off id, got:\n%s", showBuf.String())
	}
	for _, expected := range []string{"Backup Type: incremental", "Chain ID: chain-001", "Chain Index: 2", "Delta Size: 2048"} {
		if !strings.Contains(showBuf.String(), expected) {
			t.Fatalf("monitor show output missing %q, got:\n%s", expected, showBuf.String())
		}
	}
}

func TestRestoreHistoryShowsRestoreStatuses(t *testing.T) {
	t.Setenv("TEST_PG_PASSWORD", "secret")
	dbPath := filepath.Join(t.TempDir(), "restore-history.db")
	cfgPath := writeRestoreHistoryConfig(t, dbPath)

	mon, err := monitor.NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })

	now := time.Now().UTC()
	records := []monitor.RestoreExecution{
		{RestoreName: "r-success", DatabaseType: "postgres", DatabaseName: "db", Status: "completed", Timestamp: now, CreatedAt: now},
		{RestoreName: "r-failed", DatabaseType: "postgres", DatabaseName: "db", Status: "failure", Timestamp: now, CreatedAt: now},
	}
	for _, rec := range records {
		rec := rec
		if err := mon.RecordRestoreExecution(context.Background(), &rec); err != nil {
			t.Fatalf("RecordRestoreExecution() error = %v", err)
		}
	}

	prevCfg := restoreConfigFile
	t.Cleanup(func() { restoreConfigFile = prevCfg })
	restoreConfigFile = cfgPath

	cmd := &cobra.Command{}
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := handleRestoreHistory(cmd, nil); err != nil {
		t.Fatalf("handleRestoreHistory() error = %v", err)
	}

	output := buf.String()
	for _, expected := range []string{"success", "failed"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("restore history output missing normalized status %q in:\n%s", expected, output)
		}
	}

	if got := normalizeRestoreStatus(monitor.StatusTimeout); got != monitor.StatusTimeout {
		t.Fatalf("normalizeRestoreStatus(timeout) = %q, want %q", got, monitor.StatusTimeout)
	}
	if got := normalizeRestoreStatus(monitor.StatusSkipped); got != monitor.StatusSkipped {
		t.Fatalf("normalizeRestoreStatus(skipped) = %q, want %q", got, monitor.StatusSkipped)
	}
}

func writeRestoreHistoryConfig(t *testing.T, historyPath string) string {
	t.Helper()
	content := "version: \"1.0\"\n" +
		"history_db_path: " + historyPath + "\n" +
		"defaults:\n" +
		"  storage:\n" +
		"    type: local\n" +
		"    local_path: ./backups\n" +
		"databases:\n" +
		"  pg:\n" +
		"    type: postgres\n" +
		"    host: localhost\n" +
		"    username: sentinel\n" +
		"    password_env: TEST_PG_PASSWORD\n" +
		"    database: app\n" +
		"restores:\n" +
		"  restore-job:\n" +
		"    enabled: true\n" +
		"    type: postgres\n" +
		"    host: localhost\n" +
		"    username: sentinel\n" +
		"    password_env: TEST_PG_PASSWORD\n" +
		"    database: app\n" +
		"    staging_dir: /tmp/sentinel\n" +
		"    schedule: \"0 1 * * *\"\n" +
		"    backup_source:\n" +
		"      type: local\n" +
		"      local_path: ./backups\n" +
		"      backup_path: app.sql\n"

	path := filepath.Join(t.TempDir(), "restore-history.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func TestMonitorListErrorPreviewTruncation(t *testing.T) {
	longError := "this is a long error message that should be truncated in deterministic form for table rendering"
	previewA := truncatePreview(longError, defaultErrorPreviewLen)
	previewB := truncatePreview(longError, defaultErrorPreviewLen)

	if previewA != previewB {
		t.Fatalf("truncatePreview must be deterministic: %q != %q", previewA, previewB)
	}
	if len(previewA) > defaultErrorPreviewLen {
		t.Fatalf("preview exceeded max length: got %d want <= %d", len(previewA), defaultErrorPreviewLen)
	}
	if !strings.HasSuffix(previewA, "...") {
		t.Fatalf("expected truncated preview to end with ellipsis, got %q", previewA)
	}
}

func TestMonitorListEmptyResultMessage(t *testing.T) {
	cmd := &cobra.Command{}
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := printTable(cmd, nil); err != nil {
		t.Fatalf("printTable returned error for empty input: %v", err)
	}

	if !strings.Contains(buf.String(), noMatchingRecordsMessage) {
		t.Fatalf("expected empty-result message %q, got %q", noMatchingRecordsMessage, buf.String())
	}
}

func TestLoadStatisticsAggregateWhenJobOmitted(t *testing.T) {
	mon := newCLITestMonitor(t)
	defer mon.Close()

	now := time.Now().UTC()
	recordCLITestExecution(t, mon, "job-a", "success", now, 100)
	recordCLITestExecution(t, mon, "job-b", "failed", now.Add(time.Minute), 200)

	stats, err := loadStatistics(context.Background(), mon, "", 0)
	if err != nil {
		t.Fatalf("loadStatistics returned error: %v", err)
	}
	if stats.BackupName != "all jobs" {
		t.Fatalf("expected aggregate scope label, got %q", stats.BackupName)
	}
	if stats.TotalExecutions != 2 {
		t.Fatalf("expected 2 executions, got %d", stats.TotalExecutions)
	}
}

func TestLoadStatisticsJobScoped(t *testing.T) {
	mon := newCLITestMonitor(t)
	defer mon.Close()

	now := time.Now().UTC()
	recordCLITestExecution(t, mon, "job-a", "success", now, 100)
	recordCLITestExecution(t, mon, "job-b", "failed", now.Add(time.Minute), 200)

	stats, err := loadStatistics(context.Background(), mon, "job-a", 0)
	if err != nil {
		t.Fatalf("loadStatistics returned error: %v", err)
	}
	if stats.BackupName != "job-a" {
		t.Fatalf("expected job-scoped stats for job-a, got %q", stats.BackupName)
	}
	if stats.TotalExecutions != 1 {
		t.Fatalf("expected 1 execution for job-a, got %d", stats.TotalExecutions)
	}
}

func TestLoadStatisticsEmptyResultJobNotFound(t *testing.T) {
	mon := newCLITestMonitor(t)
	defer mon.Close()

	stats, err := loadStatistics(context.Background(), mon, "does-not-exist", 0)
	if err != nil {
		t.Fatalf("loadStatistics returned error: %v", err)
	}
	if stats.TotalExecutions != 0 {
		t.Fatalf("expected 0 executions for unknown job, got %d", stats.TotalExecutions)
	}
}

func TestLoadStatisticsEmptyResultAggregate(t *testing.T) {
	mon := newCLITestMonitor(t)
	defer mon.Close()

	stats, err := loadStatistics(context.Background(), mon, "", 0)
	if err != nil {
		t.Fatalf("loadStatistics returned error: %v", err)
	}
	if stats.TotalExecutions != 0 {
		t.Fatalf("expected 0 executions for empty history, got %d", stats.TotalExecutions)
	}
}

func newCLITestMonitor(t *testing.T) *monitor.Monitor {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	mon, err := monitor.NewMonitor(path)
	if err != nil {
		t.Fatalf("failed to initialize monitor: %v", err)
	}
	return mon
}

func recordCLITestExecution(t *testing.T, mon *monitor.Monitor, job, status string, timestamp time.Time, size int64) {
	t.Helper()
	exec := &monitor.Execution{
		BackupName:     job,
		DatabaseType:   "postgres",
		Timestamp:      timestamp,
		DurationMs:     1200,
		Status:         status,
		StorageBackend: "local",
		FilePath:       "/tmp/backup.sql",
		FileSizeBytes:  size,
	}
	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		t.Fatalf("record execution failed: %v", err)
	}
}
