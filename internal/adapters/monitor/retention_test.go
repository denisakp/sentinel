package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	domainret "github.com/denisakp/sentinel/internal/domain/retention"
	"github.com/denisakp/sentinel/internal/ports"
)

func newRetentionTestMonitor(t *testing.T) *Monitor {
	t.Helper()
	m, err := NewMonitor(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func seedBackupExecutions(t *testing.T, m *Monitor, jobName string, paths ...string) {
	t.Helper()
	now := time.Now().UTC()
	for i, p := range paths {
		exec := &ports.Execution{
			BackupName:    jobName,
			DatabaseType:  "postgres",
			Timestamp:     now.Add(-time.Duration(len(paths)-i) * time.Hour),
			Status:        "success",
			FilePath:      p,
			FileSizeBytes: 10,
		}
		if err := m.RecordExecution(context.Background(), exec); err != nil {
			t.Fatalf("RecordExecution(%s) error = %v", p, err)
		}
	}
}

func countBackupExecutions(t *testing.T, m *Monitor, jobName string) int {
	t.Helper()
	execs, err := m.ListExecutions(context.Background(), &ports.Filter{BackupName: jobName}, 1000, 0)
	if err != nil {
		t.Fatalf("ListExecutions() error = %v", err)
	}
	return len(execs)
}

func TestRetentionDeleteRecordsDeletesMatchingRows(t *testing.T) {
	m := newRetentionTestMonitor(t)
	seedBackupExecutions(t, m, "pg-job", "/b/old.sql", "/b/mid.sql", "/b/new.sql")

	candidates := []domainret.BackupCandidate{
		{FilePath: "/b/old.sql"},
		{FilePath: "/b/mid.sql"},
	}
	if err := m.RetentionDeleteRecords(context.Background(), "pg-job", candidates); err != nil {
		t.Fatalf("RetentionDeleteRecords() error = %v", err)
	}

	if got := countBackupExecutions(t, m, "pg-job"); got != 1 {
		t.Fatalf("remaining rows = %d, want 1", got)
	}
}

func TestRetentionDeleteRecordsEmptyCandidatesIsNoOp(t *testing.T) {
	m := newRetentionTestMonitor(t)
	seedBackupExecutions(t, m, "pg-job", "/b/only.sql")

	if err := m.RetentionDeleteRecords(context.Background(), "pg-job", nil); err != nil {
		t.Fatalf("RetentionDeleteRecords(nil) error = %v", err)
	}
	if got := countBackupExecutions(t, m, "pg-job"); got != 1 {
		t.Fatalf("remaining rows = %d, want 1", got)
	}
}

func TestRetentionDeleteRecordsIdempotentOnMissingPath(t *testing.T) {
	m := newRetentionTestMonitor(t)
	seedBackupExecutions(t, m, "pg-job", "/b/only.sql")

	candidates := []domainret.BackupCandidate{{FilePath: "/does/not/exist.sql"}}
	if err := m.RetentionDeleteRecords(context.Background(), "pg-job", candidates); err != nil {
		t.Fatalf("RetentionDeleteRecords(missing) error = %v", err)
	}
	if got := countBackupExecutions(t, m, "pg-job"); got != 1 {
		t.Fatalf("remaining rows = %d, want 1", got)
	}
}

func seedRestoreExecutions(t *testing.T, m *Monitor, jobName string, n int) {
	t.Helper()
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		exec := &ports.RestoreExecution{
			RestoreName:  jobName,
			DatabaseType: "postgres",
			DatabaseName: "db",
			RestoreMode:  "full",
			Timestamp:    now.Add(-time.Duration(n-i) * time.Hour),
			Status:       "success",
		}
		if err := m.RecordRestoreExecution(context.Background(), exec); err != nil {
			t.Fatalf("RecordRestoreExecution() error = %v", err)
		}
	}
}

func countRestoreExecutions(t *testing.T, m *Monitor, jobName string) int {
	t.Helper()
	execs, err := m.ListRestoreExecutions(context.Background(), &ports.RestoreFilter{RestoreName: jobName}, 1000, 0)
	if err != nil {
		t.Fatalf("ListRestoreExecutions() error = %v", err)
	}
	return len(execs)
}

func TestDeleteRestoreExecutionsKeepLast(t *testing.T) {
	m := newRetentionTestMonitor(t)
	seedRestoreExecutions(t, m, "restore-job", 5)

	policy := domainret.Policy{KeepLast: 2}
	if err := m.DeleteRestoreExecutions(context.Background(), "restore-job", policy); err != nil {
		t.Fatalf("DeleteRestoreExecutions() error = %v", err)
	}
	if got := countRestoreExecutions(t, m, "restore-job"); got != 2 {
		t.Fatalf("remaining rows = %d, want 2", got)
	}
}

func TestDeleteRestoreExecutionsDryRunDeletesNothing(t *testing.T) {
	m := newRetentionTestMonitor(t)
	seedRestoreExecutions(t, m, "restore-job", 5)

	policy := domainret.Policy{KeepLast: 2, DryRun: true}
	if err := m.DeleteRestoreExecutions(context.Background(), "restore-job", policy); err != nil {
		t.Fatalf("DeleteRestoreExecutions(dry-run) error = %v", err)
	}
	if got := countRestoreExecutions(t, m, "restore-job"); got != 5 {
		t.Fatalf("remaining rows = %d, want 5 (dry-run must not delete)", got)
	}
}
