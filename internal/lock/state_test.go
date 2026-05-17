package lock

import (
	"testing"
	"time"
)

func TestEvaluateLockState_DualCriterion(t *testing.T) {
	prev := isProcessAlive
	t.Cleanup(func() { isProcessAlive = prev })

	cases := []struct {
		name      string
		alive     bool
		age       time.Duration
		threshold time.Duration
		want      bool
	}{
		{"alive, age below threshold", true, time.Minute, time.Hour, false},
		{"alive, age above threshold", true, 2 * time.Hour, time.Hour, false},
		{"dead, age below threshold", false, time.Minute, time.Hour, false},
		{"dead, age above threshold", false, 2 * time.Hour, time.Hour, true},
		{"threshold zero disables eval", false, 24 * time.Hour, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isProcessAlive = func(int) bool { return tc.alive }
			jl := &JobLock{
				PID:       1234,
				StartTime: time.Now().Add(-tc.age),
			}
			got := EvaluateLockState(jl, tc.threshold).Removable
			if got != tc.want {
				t.Errorf("Removable=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateLockState_NegativeAgeNotStale(t *testing.T) {
	prev := isProcessAlive
	t.Cleanup(func() { isProcessAlive = prev })
	isProcessAlive = func(int) bool { return false }

	jl := &JobLock{PID: 1, StartTime: time.Now().Add(1 * time.Hour)}
	state := EvaluateLockState(jl, time.Minute)
	if state.Age != 0 {
		t.Errorf("Age = %s, want 0", state.Age)
	}
	if state.Removable {
		t.Error("Removable = true for future-dated lock, want false")
	}
}
