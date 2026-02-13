package scheduler

import "sync"

// Executor runs jobs with bounded concurrency.
type Executor struct {
	limit chan struct{}
	wg    sync.WaitGroup
}

// NewExecutor constructs a bounded executor.
func NewExecutor(maxConcurrent int) *Executor {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Executor{
		limit: make(chan struct{}, maxConcurrent),
	}
}

// Execute runs a job with concurrency control.
func (e *Executor) Execute(job func()) {
	e.wg.Add(1)
	go func() {
		e.limit <- struct{}{}
		defer func() {
			<-e.limit
			e.wg.Done()
		}()
		job()
	}()
}

// Wait blocks until all jobs complete.
func (e *Executor) Wait() {
	e.wg.Wait()
}
