//go:build integration

package integration

import (
	"testing"

	"github.com/denisakp/sentinel/internal/scheduler"
)

// TestSchedulerDoubleStart verifies that calling Start() on an already-running scheduler returns an error.
func TestSchedulerDoubleStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	sched := scheduler.NewScheduler(3)

	if err := sched.Start(); err != nil {
		t.Fatalf("Expected first Start() to succeed, got: %v", err)
	}
	defer sched.Stop() //nolint:errcheck

	err := sched.Start()
	if err == nil {
		t.Fatal("Expected second Start() to return error, but it succeeded")
	}

	t.Logf("Double Start() correctly returned error: %v", err)
}

// TestSchedulerLifecycle verifies that a scheduler can be started and stopped cleanly.
func TestSchedulerLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	sched := scheduler.NewScheduler(3)

	if err := sched.Start(); err != nil {
		t.Fatalf("Expected scheduler.Start() to succeed, got: %v", err)
	}

	if err := sched.Stop(); err != nil {
		t.Fatalf("Expected scheduler.Stop() to succeed, got: %v", err)
	}

	t.Log("Scheduler lifecycle (Start/Stop) completed successfully")
}

// TestSchedulerStopWithoutStart verifies that stopping a scheduler that was never started returns an error.
func TestSchedulerStopWithoutStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	sched := scheduler.NewScheduler(3)

	err := sched.Stop()
	if err == nil {
		t.Fatal("Expected Stop() without Start() to return error")
	}

	t.Logf("Scheduler.Stop() correctly returned error without prior Start(): %v", err)
}
