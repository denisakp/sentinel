package integration_test

import (
	"context"
	"sync"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	internalrestore "github.com/denisakp/sentinel/internal/restore"
	"github.com/denisakp/sentinel/internal/scheduler"
)

func TestScheduledRestoreOverlapReturnsSkipResult(t *testing.T) {
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

	const workers = 3
	results := make(chan *schedulerResult, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := scheduler.ExecuteScheduledRestore(context.Background(), cfg, "restore-overlap", job, nil, limiter)
			results <- &schedulerResult{res: res, err: err}
		}()
	}

	wg.Wait()
	close(results)

	for item := range results {
		if item.err != nil {
			t.Fatalf("ExecuteScheduledRestore() error = %v, want nil", item.err)
		}
		if item.res == nil {
			t.Fatal("ExecuteScheduledRestore() result = nil")
		}
		if item.res.Status != ports.StatusSkipped {
			t.Fatalf("result.Status = %q, want %q", item.res.Status, ports.StatusSkipped)
		}
		if item.res.Reason != "concurrency_limit_reached" {
			t.Fatalf("result.Reason = %q, want concurrency_limit_reached", item.res.Reason)
		}
	}
}

type schedulerResult struct {
	res *internalrestore.ExecutionResult
	err error
}
