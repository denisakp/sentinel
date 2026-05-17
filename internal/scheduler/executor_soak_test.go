//go:build soak

package scheduler

import (
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// TestExecutor_OneHourSoak is the SC-002 non-blocking soak. It runs a mixed
// panic/normal workload for one hour and verifies the executor still reports
// full free capacity at the end. Build-tagged `soak` so it never blocks the
// standard `go test ./...` run.
func TestExecutor_OneHourSoak(t *testing.T) {
	const capacity = 4
	const duration = time.Hour

	e := NewExecutorWithLogger(capacity, slog.New(slog.NewTextHandler(testWriter{t}, nil)))
	var submitted, completed atomic.Int64

	deadline := time.Now().Add(duration)
	i := 0
	for time.Now().Before(deadline) {
		i++
		idx := i
		submitted.Add(1)
		e.Execute(func() {
			completed.Add(1)
			if idx%3 == 0 {
				panic(fmt.Sprintf("soak-panic-%d", idx))
			}
		})
		if i%256 == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	e.Wait()

	if got := submitted.Load(); got != completed.Load() {
		t.Errorf("submitted=%d completed=%d (mismatch)", got, completed.Load())
	}
	if got := len(e.limit); got != 0 {
		t.Errorf("expected 0 in-flight slots after Wait, got %d", got)
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
