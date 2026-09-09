package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// noopDumpBuilder satisfies ports.DumpBuilder. The test never runs a dump; it
// only needs the factory to reach its construction step.
type noopDumpBuilder struct{}

func (noopDumpBuilder) Build(ports.BuildContext) (ports.BuildResult, error) {
	return ports.BuildResult{}, nil
}

// TestBackupExecutorTakesAJobLock is the regression guard for #163.
//
// The executor was constructed with a nil LockManager, under a comment saying
// the scheduler owned serialization. It did not: the scheduler's RunWithLock and
// RunWithTimeout had no callers anywhere, including tests. The only overlap
// guard was an in-process flag inside one scheduler process, which does nothing
// for two `sentinel backup` invocations, for a cron entry running beside a
// scheduler, or for two hosts sharing a storage target. Two overlapping backups
// of one job could interleave their output and their history rows.
//
// Restores already took this lock, so what was missing was consistency, not a
// mechanism.
func TestBackupExecutorTakesAJobLock(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "locks")
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: filepath.Join(t.TempDir(), "history.db"),
		Scheduler:     config.SchedulerConfig{LockDir: lockDir},
	}
	job := config.BackupJob{
		Name: "pg-job",
		Type: "postgres",
		Host: "localhost",
		Port: 5432,
	}

	exec, err := NewBackupExecutorFromConfig(cfg, job, nil, false, false, nil, noopDumpBuilder{})
	if err != nil {
		t.Fatalf("NewBackupExecutorFromConfig() error = %v", err)
	}
	if exec == nil {
		t.Fatal("NewBackupExecutorFromConfig() returned no executor")
	}

	if exec.exec.Locks() == nil {
		t.Error("backup executor was built with no lock manager, so nothing serializes " +
			"two backups of this job across processes")
	}

	// The manager must serialize on the configured directory, which is also the
	// one restores use, so a backup and a restore of the same job contend.
	held, err := exec.exec.Locks().TryAcquire(job.Name, 0)
	if err != nil {
		t.Fatalf("acquiring the job lock through the executor's manager: %v", err)
	}
	if held == nil {
		t.Fatal("TryAcquire returned no lock")
	}
	if _, statErr := os.Stat(filepath.Join(lockDir, job.Name+".lock")); statErr != nil {
		t.Errorf("no lock file under the configured lock_dir %q: %v", lockDir, statErr)
	}
}

// TestBackupExecutorLockDirDefaultsToSomethingUsable ties #163 to #154: wiring a
// lock is only an improvement if the directory it lands in can be created. The
// previous default, /var/run/sentinel, could not be by an unprivileged user, so
// wiring the lock without fixing the default would have turned every non-root
// backup into a hard failure.
func TestBackupExecutorLockDirDefaultsToSomethingUsable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where the system lock directory is correct")
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", t.TempDir())

	dir := config.DefaultLockDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("the lock directory a backup would default to is not creatable: %q: %v", dir, err)
	}
}
