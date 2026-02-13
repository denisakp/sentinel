package scheduler_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/scheduler"
)

func TestExecutorConcurrencyLimit(t *testing.T) {
	executor := scheduler.NewExecutor(2)
	var current int32
	var maxSeen int32
	block := make(chan struct{})

	for i := 0; i < 5; i++ {
		executor.Execute(func() {
			value := atomic.AddInt32(&current, 1)
			for {
				prev := atomic.LoadInt32(&maxSeen)
				if value <= prev || atomic.CompareAndSwapInt32(&maxSeen, prev, value) {
					break
				}
			}
			<-block
			atomic.AddInt32(&current, -1)
		})
	}

	time.Sleep(100 * time.Millisecond)
	if maxSeen > 2 {
		t.Fatalf("expected max concurrency 2, got %d", maxSeen)
	}

	close(block)
	executor.Wait()
}
