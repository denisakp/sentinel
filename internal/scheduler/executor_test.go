package scheduler

import (
	"errors"
	"testing"
	"time"
)

// T049: Table-driven tests for withRetry

func TestWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	calls := 0
	err := withRetry(func() error {
		calls++
		return nil
	}, 3, []time.Duration{time.Millisecond, time.Millisecond})

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestWithRetry_SuccessOnSecondAttempt(t *testing.T) {
	calls := 0
	err := withRetry(func() error {
		calls++
		if calls == 1 {
			return errors.New("network: transient failure")
		}
		return nil
	}, 3, []time.Duration{time.Millisecond, time.Millisecond})

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestWithRetry_AllAttemptsFail(t *testing.T) {
	calls := 0
	sentinel := errors.New("network: persistent failure")
	err := withRetry(func() error {
		calls++
		return sentinel
	}, 3, []time.Duration{time.Millisecond, time.Millisecond})

	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestWithRetry_NonRetriableSkipsRemaining(t *testing.T) {
	calls := 0
	err := withRetry(func() error {
		calls++
		return errors.New("invalid tls certificate: expired")
	}, 3, []time.Duration{time.Millisecond, time.Millisecond})

	if err == nil {
		t.Error("expected non-nil error")
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 call for non-retriable error, got %d", calls)
	}
}
