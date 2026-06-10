package integration_test

import (
	"context"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/scheduler"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestScheduledRestoreSkipsWhenConcurrencyLimitReached(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.Scheduler.LockDir = t.TempDir()

	job := config.RestoreJob{
		Type:     "postgres",
		Database: "app",
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			BackupPath: "backup.sql",
		},
	}

	limiter := make(chan struct{}, 1)
	limiter <- struct{}{}

	result, err := scheduler.ExecuteScheduledRestore(context.Background(), cfg, "nightly-restore", job, nil, limiter)
	if err != nil {
		t.Fatalf("ExecuteScheduledRestore() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("ExecuteScheduledRestore() result = nil")
	}
	if result.Status != ports.StatusSkipped {
		t.Fatalf("result.Status = %q, want %q", result.Status, ports.StatusSkipped)
	}
	if result.Reason != "concurrency_limit_reached" {
		t.Fatalf("result.Reason = %q, want concurrency_limit_reached", result.Reason)
	}
}
