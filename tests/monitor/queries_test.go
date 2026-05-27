package monitor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestListExecutionsWithFilters(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	start := time.Now().Add(-2 * time.Hour)
	insertExecution(t, mon, "job-a", "postgres", "success", start, 100)
	insertExecution(t, mon, "job-a", "postgres", "failure", start.Add(30*time.Minute), 50)
	insertExecution(t, mon, "job-b", "mysql", "success", start.Add(90*time.Minute), 75)

	filter := &ports.Filter{BackupName: "job-a"}
	execs, err := mon.ListExecutions(context.Background(), filter, 10, 0)
	if err != nil {
		t.Fatalf("list executions failed: %v", err)
	}
	if len(execs) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(execs))
	}

	filter = &ports.Filter{Status: "success"}
	execs, err = mon.ListExecutions(context.Background(), filter, 10, 0)
	if err != nil {
		t.Fatalf("list executions failed: %v", err)
	}
	if len(execs) != 2 {
		t.Fatalf("expected 2 successes, got %d", len(execs))
	}

	filter = &ports.Filter{StartDate: start.Add(45 * time.Minute)}
	execs, err = mon.ListExecutions(context.Background(), filter, 10, 0)
	if err != nil {
		t.Fatalf("list executions failed: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution after time filter, got %d", len(execs))
	}
}

func TestGetExecution(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	id := insertExecution(t, mon, "job-a", "postgres", "success", time.Now(), 100)

	exec, err := mon.GetExecution(context.Background(), id)
	if err != nil {
		t.Fatalf("get execution failed: %v", err)
	}
	if exec.ID != id {
		t.Fatalf("expected id %s, got %s", id, exec.ID)
	}
}

func TestGetAggregateStatisticsAllJobs(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	now := time.Now().UTC()
	insertExecution(t, mon, "job-a", "postgres", "success", now.Add(-2*time.Hour), 100)
	insertExecution(t, mon, "job-b", "mysql", "failed", now.Add(-time.Hour), 200)
	insertExecution(t, mon, "job-c", "postgres", "completed", now, 300)

	stats, err := mon.GetAggregateStatistics(context.Background(), 0)
	if err != nil {
		t.Fatalf("get aggregate statistics failed: %v", err)
	}

	if stats.BackupName != "all jobs" {
		t.Fatalf("expected aggregate backup name label, got %q", stats.BackupName)
	}
	if stats.TotalExecutions != 3 {
		t.Fatalf("expected 3 executions, got %d", stats.TotalExecutions)
	}
	if stats.SuccessCount != 2 {
		t.Fatalf("expected 2 successes, got %d", stats.SuccessCount)
	}
	if stats.FailureCount != 1 {
		t.Fatalf("expected 1 failure, got %d", stats.FailureCount)
	}
}

func TestGetAggregateStatisticsRespectsTimeWindow(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	now := time.Now().UTC()
	insertExecution(t, mon, "job-old", "postgres", "success", now.Add(-72*time.Hour), 100)
	insertExecution(t, mon, "job-recent", "postgres", "success", now.Add(-2*time.Hour), 200)

	stats, err := mon.GetAggregateStatistics(context.Background(), 1)
	if err != nil {
		t.Fatalf("get aggregate statistics with day filter failed: %v", err)
	}

	if stats.TotalExecutions != 1 {
		t.Fatalf("expected 1 recent execution in 1-day window, got %d", stats.TotalExecutions)
	}
}

// TestMigrationReconciliation verifies that monitor queries work correctly after schema migrations.
// This is a regression test for US2 (Schema Migration Visibility).
func TestMigrationReconciliation(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	// Verify migration status query works
	status, err := mon.GetMigrationStatus(context.Background())
	if err != nil {
		t.Fatalf("get migration status failed: %v", err)
	}

	if !status.IsUpToDate {
		t.Fatalf("expected migrations to be up-to-date, but current=%d latest=%d",
			status.CurrentVersion, status.LatestAvailableVersion)
	}

	if len(status.AppliedMigrations) == 0 {
		t.Fatal("expected at least one migration to be applied")
	}

	// Verify new status values (pending, running, interrupted) are supported
	now := time.Now()
	testCases := []struct {
		name   string
		status string
	}{
		{"pending execution", "pending"},
		{"running execution", "running"},
		{"completed execution", "completed"},
		{"failed execution", "failed"},
		{"interrupted execution", "interrupted"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			id := insertExecution(t, mon, "job-reconcile", "postgres", tc.status, now, 100)

			exec, err := mon.GetExecution(context.Background(), id)
			if err != nil {
				t.Fatalf("get execution failed for status %s: %v", tc.status, err)
			}

			if exec.Status != tc.status {
				t.Fatalf("expected status %s, got %s", tc.status, exec.Status)
			}
		})
	}

	// Verify cleanup columns are accessible
	id := insertExecution(t, mon, "job-cleanup", "postgres", "failed", now, 100)
	exec, err := mon.GetExecution(context.Background(), id)
	if err != nil {
		t.Fatalf("get execution with cleanup fields failed: %v", err)
	}

	// Cleanup fields should be zero-valued (not set yet)
	if exec.FinishedAt != nil {
		t.Fatalf("expected FinishedAt to be nil for new execution")
	}
}

func newTestMonitor(t *testing.T) *monitor.Monitor {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "history.db")

	mon, err := monitor.NewMonitor(path)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}
	return mon
}

func insertExecution(t *testing.T, mon *monitor.Monitor, jobName, dbType, status string, timestamp time.Time, size int64) string {
	t.Helper()
	exec := &ports.Execution{
		BackupName:     jobName,
		DatabaseType:   dbType,
		Timestamp:      timestamp.UTC(),
		DurationMs:     1234,
		Status:         status,
		StorageBackend: "local",
		FilePath:       filepath.Join(os.TempDir(), "backup.sql"),
		FileSizeBytes:  size,
	}

	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		t.Fatalf("record execution failed: %v", err)
	}
	return exec.ID
}
