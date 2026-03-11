package scheduler_test

import (
	"sync"
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
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		executor.Execute(func() {
			defer wg.Done()
			value := atomic.AddInt32(&current, 1)

			// Update maxSeen with proper synchronization
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
	observed := atomic.LoadInt32(&maxSeen)
	if observed > 2 {
		t.Fatalf("expected max concurrency 2, got %d", observed)
	}

	close(block)
	wg.Wait()
	executor.Wait()
}
