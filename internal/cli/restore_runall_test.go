package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	internalrestore "github.com/denisakp/sentinel/internal/adapters/restore/runtime"
	"github.com/denisakp/sentinel/internal/ports"
)

// TestHandleRestoreRunAll_FailureIsolationAndConcurrency verifies that --all
// runs every enabled job, one failure does not abort siblings, concurrency is
// bounded, and the overall result is non-nil when any job fails (spec 045).
func TestHandleRestoreRunAll_FailureIsolationAndConcurrency(t *testing.T) {
	prevExecutor := runRestoreExecution
	prevParallel := restoreParallel
	t.Cleanup(func() {
		runRestoreExecution = prevExecutor
		restoreParallel = prevParallel
	})

	enabled := true
	cfg := &config.Configuration{
		HistoryDBPath:         filepath.Join(t.TempDir(), "history.db"),
		MaxConcurrentRestores: 2,
		Restores: map[string]config.RestoreJob{
			"job-a":    {Name: "job-a", Type: "postgres", Enabled: &enabled, Database: "a"},
			"job-b":    {Name: "job-b", Type: "postgres", Enabled: &enabled, Database: "b"},
			"job-fail": {Name: "job-fail", Type: "postgres", Enabled: &enabled, Database: "f"},
		},
	}
	restoreParallel = 2

	var (
		mu       sync.Mutex
		invoked  = map[string]bool{}
		inFlight int32
		maxSeen  int32
	)
	runRestoreExecution = func(_ context.Context, req *internalrestore.ExecutionRequest) (*internalrestore.ExecutionResult, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxSeen)
			if n <= old || atomic.CompareAndSwapInt32(&maxSeen, old, n) {
				break
			}
		}
		defer atomic.AddInt32(&inFlight, -1)

		mu.Lock()
		invoked[req.JobName] = true
		mu.Unlock()

		if req.JobName == "job-fail" {
			return nil, fmt.Errorf("boom")
		}
		return &internalrestore.ExecutionResult{Status: ports.StatusSuccess}, nil
	}

	err := handleRestoreRunAll(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected non-nil error when a job fails")
	}

	// all three jobs invoked (failure isolation: the failing one did not abort others)
	for _, name := range []string{"job-a", "job-b", "job-fail"} {
		if !invoked[name] {
			t.Errorf("job %q was not invoked", name)
		}
	}
	// concurrency never exceeded the limit
	if maxSeen > 2 {
		t.Errorf("observed concurrency %d exceeds limit 2", maxSeen)
	}
}

func TestEffectiveRestoreConcurrency(t *testing.T) {
	cfg := &config.Configuration{MaxConcurrentRestores: 3}
	if got := effectiveRestoreConcurrency(cfg, 5); got != 5 {
		t.Errorf("--parallel override: got %d want 5", got)
	}
	if got := effectiveRestoreConcurrency(cfg, 0); got != 3 {
		t.Errorf("config value: got %d want 3", got)
	}
	if got := effectiveRestoreConcurrency(&config.Configuration{}, 0); got != 1 {
		t.Errorf("default: got %d want 1", got)
	}
}
