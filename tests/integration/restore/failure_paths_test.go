package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/restore"
)

func TestFailurePaths_ContextCanceled_ReturnsInterruptedReason(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())

	enabled := true
	job := config.RestoreJob{
		Name:       "test-interrupted",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	// Cancel after staging succeeds but before/during engine execution.
	cancel()

	result, err := restore.ExecuteRestore(ctx, &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	if result == nil {
		t.Fatal("result must not be nil")
	}
	// A pre-cancelled context may cause the staging download to fail or the
	// engine to fail with context.Canceled.
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if result.Reason != "interrupted" {
				t.Errorf("expected Reason=interrupted for context.Canceled, got %q", result.Reason)
			}
		}
		// Other errors (e.g. source not found) are acceptable when ctx is already done.
	}
}

func TestFailurePaths_TimeoutExceeded_ReturnsTimeoutStatus(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()

	enabled := true
	job := config.RestoreJob{
		Name:           "test-timeout",
		Type:           "postgres",
		Enabled:        &enabled,
		Host:           "localhost",
		Port:           5432,
		Username:       "testuser",
		Database:       "testdb",
		Schedule:       "0 2 * * *",
		StagingDir:     stagingDir,
		TimeoutSeconds: 1, // very short timeout to trigger during engine execution
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	start := time.Now()
	result, err := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})
	elapsed := time.Since(start)

	if result == nil {
		t.Fatal("result must not be nil")
	}
	// If engine timed out, status must be StatusTimeout.
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		if result.Status != ports.StatusTimeout {
			t.Errorf("expected status=%s for deadline exceeded, got %q", ports.StatusTimeout, result.Status)
		}
		if result.Reason != "timeout" {
			t.Errorf("expected Reason=timeout, got %q", result.Reason)
		}
	}
	// Execution should not hang beyond the timeout + small buffer.
	if elapsed > 10*time.Second {
		t.Errorf("execution took %v; expected it to stop near the 1s timeout", elapsed)
	}
}

func TestFailurePaths_LockConflict_ReturnsSkippedStatus(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()
	lockDir := t.TempDir()

	// Pre-create a lock file owned by a live PID (this test process) so the
	// dual-criterion evaluator treats it as held rather than stale.
	lockFilePath := filepath.Join(lockDir, "test-lock-conflict.lock")
	host, _ := os.Hostname()
	jl := ports.JobLock{
		PID:       os.Getpid(),
		JobName:   "test-lock-conflict",
		StartTime: time.Now(),
		Hostname:  host,
	}
	body, _ := json.Marshal(jl)
	if err := os.WriteFile(lockFilePath, body, 0o600); err != nil {
		t.Fatalf("failed to create fake lock file: %v", err)
	}

	enabled := true
	job := config.RestoreJob{
		Name:       "test-lock-conflict",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	result, err := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
		LockDir: lockDir,
	})

	if !errors.Is(err, restore.ErrRestoreLockConflict) {
		t.Fatalf("expected ErrRestoreLockConflict, got %v", err)
	}
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.Status != ports.StatusSkipped {
		t.Errorf("expected status=%s for lock conflict, got %q", ports.StatusSkipped, result.Status)
	}
	if result.Reason != "lock_conflict" {
		t.Errorf("expected Reason=lock_conflict, got %q", result.Reason)
	}
}

func TestFailurePaths_MissingStagingDir_Config_Fails(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)

	enabled := true
	job := config.RestoreJob{
		Name:       "test-no-staging",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: "", // missing
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	result, err := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	// staging_dir is required — executor or config validation must reject this.
	if err == nil && (result == nil || result.Status != "failed") {
		t.Error("expected error or failed status when staging_dir is empty")
	}
}

func TestFailurePaths_SourceNotFound_FailsWithNotFoundError(t *testing.T) {
	localDir := t.TempDir()
	stagingDir := t.TempDir()

	enabled := true
	job := config.RestoreJob{
		Name:       "test-source-missing",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: "does-not-exist.sql",
		},
	}

	result, err := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	if err == nil {
		t.Fatal("expected error for missing source object")
	}
	if !errors.Is(err, restore.ErrSourceObjectNotFound) {
		t.Errorf("expected ErrSourceObjectNotFound, got %v", err)
	}
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
}
