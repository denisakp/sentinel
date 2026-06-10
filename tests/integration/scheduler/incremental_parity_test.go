package schedulerintegration_test

import (
	"context"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	internalrestore "github.com/denisakp/sentinel/internal/adapters/restore/runtime"
	"github.com/denisakp/sentinel/internal/scheduler"
)

func TestIncrementalSchedulerExecutionRequestParityWithCLIRequest(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.Scheduler.LockDir = "/tmp/sentinel-locks"

	job := config.RestoreJob{
		Name:                "restore-incremental-job",
		Type:                "postgres",
		Database:            "app",
		RestoreMode:         "incremental",
		ConfirmFullFallback: true,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  "/tmp/backups",
			BackupPath: "app-latest.dump",
		},
	}

	expected := &internalrestore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
		Config:  cfg,
		LockDir: cfg.Scheduler.LockDir,
		Monitor: nil,
	}

	var captured *internalrestore.ExecutionRequest
	_, err := scheduler.ExecuteScheduledRestoreWithRunner(
		context.Background(),
		cfg,
		job.Name,
		job,
		nil,
		nil,
		func(_ context.Context, req *internalrestore.ExecutionRequest) (*internalrestore.ExecutionResult, error) {
			captured = req
			return &internalrestore.ExecutionResult{Status: ports.StatusCompleted}, nil
		},
	)
	if err != nil {
		t.Fatalf("ExecuteScheduledRestoreWithRunner() error = %v", err)
	}
	if captured == nil {
		t.Fatal("captured request is nil")
	}

	if captured.JobName != expected.JobName {
		t.Fatalf("job name mismatch: got %q want %q", captured.JobName, expected.JobName)
	}
	if captured.Job.RestoreMode != expected.Job.RestoreMode {
		t.Fatalf("restore mode mismatch: got %q want %q", captured.Job.RestoreMode, expected.Job.RestoreMode)
	}
	if captured.Job.ConfirmFullFallback != expected.Job.ConfirmFullFallback {
		t.Fatalf("confirm_full_fallback mismatch: got %v want %v", captured.Job.ConfirmFullFallback, expected.Job.ConfirmFullFallback)
	}
	if captured.Job.BackupSource.Type != expected.Job.BackupSource.Type {
		t.Fatalf("source type mismatch: got %q want %q", captured.Job.BackupSource.Type, expected.Job.BackupSource.Type)
	}
	if captured.Job.BackupSource.BackupPath != expected.Job.BackupSource.BackupPath {
		t.Fatalf("source backup path mismatch: got %q want %q", captured.Job.BackupSource.BackupPath, expected.Job.BackupSource.BackupPath)
	}
	if captured.LockDir != expected.LockDir {
		t.Fatalf("lock dir mismatch: got %q want %q", captured.LockDir, expected.LockDir)
	}
}
