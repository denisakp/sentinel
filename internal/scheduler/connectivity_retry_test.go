package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestSchedulerSurvivesConnectivityCheckFailure asserts that a connectivity-style
// error on attempt 1 is consumed by RunBackupWithRetry and the closure succeeds
// on attempt 2 — covers the first clause of SC-001.
func TestSchedulerSurvivesConnectivityCheckFailure(t *testing.T) {
	prev := defaultBackoffs
	defaultBackoffs = []time.Duration{0, 0, 0}
	t.Cleanup(func() { defaultBackoffs = prev })

	var attempts atomic.Int32
	fn := func() error {
		n := attempts.Add(1)
		if n == 1 {
			return errors.New("ping failed: connection refused")
		}
		return nil
	}

	err := RunBackupWithRetry(context.Background(), "backup-1", "db1", fn)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("want 2 attempts, got %d", got)
	}
}

// TestSchedulerLoopContinuesAfterJobFailure asserts that an always-failing job
// does not prevent other jobs from continuing to execute on subsequent ticks —
// second clause of SC-001.
func TestSchedulerLoopContinuesAfterJobFailure(t *testing.T) {
	s := NewScheduler(2)

	var aCount, bCount atomic.Int32
	stateA := &jobState{
		name: "always-fail",
		fn: func() error {
			aCount.Add(1)
			return errors.New("ping failed: connection refused")
		},
	}
	stateB := &jobState{
		name: "always-ok",
		fn: func() error {
			bCount.Add(1)
			return nil
		},
	}

	// Register so JobStatus paths are exercised but drive ticks directly.
	s.mu.Lock()
	s.jobs[stateA.name] = stateA
	s.jobs[stateB.name] = stateB
	s.mu.Unlock()

	for i := 0; i < 3; i++ {
		s.runJob(stateA)
		s.runJob(stateB)
	}

	if got := aCount.Load(); got != 3 {
		t.Fatalf("job A should have run 3 times (loop kept ticking after each failure), got %d", got)
	}
	if got := bCount.Load(); got < 2 {
		t.Fatalf("job B should have run >=2 times after A's first failure, got %d", got)
	}
	if stateA.lastStatus != "failure" {
		t.Fatalf("expected A to remain in failure state, got %q", stateA.lastStatus)
	}
	if stateB.lastStatus != "success" {
		t.Fatalf("expected B to remain in success state, got %q", stateB.lastStatus)
	}
}
