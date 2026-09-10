package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/lock"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// seedRunningExecution records one execution left in the `running` state, as an
// unclean shutdown would leave it, and returns its id.
func seedRunningExecution(t *testing.T, mon *monitor.Monitor, jobName string) string {
	t.Helper()
	exec := &ports.Execution{
		BackupName:   jobName,
		DatabaseType: "postgres",
		Timestamp:    time.Now().UTC().Add(-5 * time.Minute),
		Status:       ports.StatusRunning,
		FilePath:     "/backups/" + jobName + ".sql",
	}
	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}
	rows, err := mon.GetStaleRunningExecutions(context.Background())
	if err != nil {
		t.Fatalf("GetStaleRunningExecutions() error = %v", err)
	}
	for _, r := range rows {
		if r.BackupName == jobName {
			return r.ID
		}
	}
	t.Fatalf("seeded running execution for %q not found", jobName)
	return ""
}

func reconcileFixture(t *testing.T) (*monitor.Monitor, *config.Configuration, string) {
	t.Helper()
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })

	lockDir := filepath.Join(dir, "locks")
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Scheduler:     config.SchedulerConfig{LockDir: lockDir, StaleLockThreshold: 60},
	}
	return mon, cfg, lockDir
}

func statusOf(t *testing.T, mon *monitor.Monitor, id string) string {
	t.Helper()
	execs, err := mon.ListExecutions(context.Background(), &ports.Filter{}, 100, 0)
	if err != nil {
		t.Fatalf("ListExecutions() error = %v", err)
	}
	for _, e := range execs {
		if e.ID == id {
			return e.Status
		}
	}
	t.Fatalf("execution %s not found", id)
	return ""
}

// TestSchedulerStartLeavesLiveRunsAlone is the regression guard for #195.
//
// ReconcileStaleExecutions had no age guard and no liveness check: every row
// still marked `running` became `interrupted`, including a backup running at that
// very moment. Restarting the scheduler, a routine operation, therefore corrupted
// the history of work in progress. The run was recorded as interrupted while it
// carried on and completed normally, so anything reacting to a failed run acted on
// a false signal.
//
// The issue supposed this needed backups to take a lock first, which they did not
// at the time. They do now (#163), so a live run is distinguishable.
func TestSchedulerStartLeavesLiveRunsAlone(t *testing.T) {
	mon, cfg, lockDir := reconcileFixture(t)
	id := seedRunningExecution(t, mon, "pg-job")

	// A live lock, exactly as a backup running right now would hold.
	lm := lock.NewManager(lockDir)
	if _, err := lm.TryAcquire("pg-job", 0); err != nil {
		t.Fatalf("acquiring the job lock: %v", err)
	}
	t.Cleanup(func() { _ = lm.Release("pg-job") })

	n, err := reconcileStaleExecutionsOnStart(context.Background(), mon, cfg)
	if err != nil {
		t.Fatalf("reconcileStaleExecutionsOnStart() error = %v", err)
	}
	if n != 0 {
		t.Errorf("finalised %d execution(s) while the job holds a live lock; a running backup "+
			"was recorded as interrupted while it carried on", n)
	}
	if got := statusOf(t, mon, id); got != ports.StatusRunning {
		t.Errorf("execution status = %q, want %q: the in-flight run's history was rewritten",
			got, ports.StatusRunning)
	}
}

// TestSchedulerStartFinalisesAbandonedRuns: the guard must still do its job. A
// row left running by a process that died holds no lock, and finalising it is the
// entire point of reconciling at startup.
func TestSchedulerStartFinalisesAbandonedRuns(t *testing.T) {
	mon, cfg, _ := reconcileFixture(t)
	id := seedRunningExecution(t, mon, "pg-job")

	n, err := reconcileStaleExecutionsOnStart(context.Background(), mon, cfg)
	if err != nil {
		t.Fatalf("reconcileStaleExecutionsOnStart() error = %v", err)
	}
	if n != 1 {
		t.Errorf("finalised %d execution(s), want 1: a row with no lock is abandoned", n)
	}
	if got := statusOf(t, mon, id); got != ports.StatusInterrupted {
		t.Errorf("execution status = %q, want %q", got, ports.StatusInterrupted)
	}
}

// TestSchedulerStartLeavesForeignHostRunsAlone: in a multi-host deployment, a row
// whose lock is held elsewhere belongs to that host. Finalising it from here would
// rewrite the history of a run this process cannot see.
func TestSchedulerStartLeavesForeignHostRunsAlone(t *testing.T) {
	mon, cfg, lockDir := reconcileFixture(t)
	id := seedRunningExecution(t, mon, "pg-job")

	// A lock file naming another host, as a peer would have written.
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("creating lock dir: %v", err)
	}
	body := `{"job_name":"pg-job","pid":4242,"hostname":"another-host","acquired_at":"` +
		time.Now().UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(filepath.Join(lockDir, "pg-job.lock"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing foreign lock: %v", err)
	}

	n, err := reconcileStaleExecutionsOnStart(context.Background(), mon, cfg)
	if err != nil {
		t.Fatalf("reconcileStaleExecutionsOnStart() error = %v", err)
	}
	if n != 0 {
		t.Errorf("finalised %d execution(s) whose lock is held by another host", n)
	}
	if got := statusOf(t, mon, id); got != ports.StatusRunning {
		t.Errorf("execution status = %q, want %q: another host's run was rewritten", got, ports.StatusRunning)
	}
}
