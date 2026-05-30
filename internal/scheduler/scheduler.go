package scheduler

import (
	"fmt"
	"sync"
	"time"

	"github.com/denisakp/sentinel/internal/domain/schedule"
	"github.com/robfig/cron/v3"
)

// defaultStaleLockThreshold is the age after which locks with dead PIDs are cleaned up.
const defaultStaleLockThreshold = 60 * time.Minute

type jobState struct {
	id             cron.EntryID
	name           string
	schedule       string
	fn             func() error
	running        bool
	executionCount int
	lastExecution  time.Time
	lastStatus     string
	lastError      string
	history        []schedule.ExecutionRecord
}

// Scheduler manages cron-based execution with bounded concurrency.
type Scheduler struct {
	cron     *cron.Cron
	parser   cron.Parser
	executor *Executor
	clock    Clock
	lockDir  string // directory for job lock files; empty disables stale-lock scan

	mu      sync.Mutex
	running bool
	jobs    map[string]*jobState
}

// NewScheduler creates a scheduler with the provided concurrency limit.
func NewScheduler(maxConcurrent int) *Scheduler {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return &Scheduler{
		cron:     cron.New(cron.WithParser(parser)),
		parser:   parser,
		executor: NewExecutor(maxConcurrent),
		clock:    realClock{},
		jobs:     make(map[string]*jobState),
	}
}

// SetLockDir configures the directory used for job lock files.
// When set, stale locks are cleaned at scheduler startup.
func (s *Scheduler) SetLockDir(dir string) {
	s.lockDir = dir
}

// Start begins the scheduler loop.
// T046: on startup, scan and clean any stale lock files left by a previous crash.
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("scheduler already running")
	}
	if s.lockDir != "" {
		ScanAndCleanStaleLocks(s.lockDir, defaultStaleLockThreshold)
	}
	s.cron.Start()
	s.running = true
	return nil
}

// Stop halts the scheduler and waits for in-flight jobs.
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return fmt.Errorf("scheduler not running")
	}
	ctx := s.cron.Stop()
	s.running = false
	s.mu.Unlock()

	<-ctx.Done()
	s.executor.Wait()
	return nil
}

// AddJob registers a new backup job with cron schedule.
func (s *Scheduler) AddJob(name, schedule string, fn func() error) error {
	if name == "" {
		return fmt.Errorf("job name is required")
	}
	if schedule == "" {
		return fmt.Errorf("schedule is required for job '%s'", name)
	}
	if _, err := s.parser.Parse(schedule); err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", schedule, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[name]; exists {
		return fmt.Errorf("job '%s' already exists", name)
	}

	state := &jobState{
		name:     name,
		schedule: schedule,
		fn:       fn,
	}

	id, err := s.cron.AddFunc(schedule, func() {
		s.executor.Execute(func() {
			s.runJob(state)
		})
	})
	if err != nil {
		return fmt.Errorf("failed to add job '%s': %w", name, err)
	}

	state.id = id
	s.jobs[name] = state
	return nil
}

// RemoveJob unregisters a scheduled job.
func (s *Scheduler) RemoveJob(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.jobs[name]
	if !ok {
		return fmt.Errorf("job '%s' not found", name)
	}
	s.cron.Remove(state.id)
	delete(s.jobs, name)
	return nil
}

// ListJobs returns all registered jobs with next execution times.
func (s *Scheduler) ListJobs() []schedule.JobInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos := make([]schedule.JobInfo, 0, len(s.jobs))
	for name, state := range s.jobs {
		entry := s.cron.Entry(state.id)
		infos = append(infos, schedule.JobInfo{
			Name:           name,
			ScheduleExpr:   state.schedule,
			NextExecution:  entry.Next,
			LastExecution:  state.lastExecution,
			LastStatus:     state.lastStatus,
			ExecutionCount: state.executionCount,
		})
	}
	return infos
}

// JobStatus returns detailed status of a specific job.
func (s *Scheduler) JobStatus(name string) (*schedule.JobStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.jobs[name]
	if !ok {
		return nil, fmt.Errorf("job '%s' not found", name)
	}
	entry := s.cron.Entry(state.id)
	status := &schedule.JobStatus{
		Name:             state.name,
		Schedule:         state.schedule,
		Enabled:          true,
		NextExecution:    entry.Next,
		LastExecution:    state.lastExecution,
		LastStatus:       state.lastStatus,
		LastError:        state.lastError,
		ExecutionHistory: append([]schedule.ExecutionRecord(nil), state.history...),
	}
	return status, nil
}

// IsRunning returns whether the scheduler is active.
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scheduler) runJob(state *jobState) {
	s.mu.Lock()
	if state.running {
		state.lastStatus = "skipped"
		s.mu.Unlock()
		return
	}
	state.running = true
	s.mu.Unlock()

	start := s.clock.Now()
	var err error
	func() {
		defer func() {
			if pErr, _ := handlePanic(recover()); pErr != nil {
				err = pErr
			}
		}()
		err = state.fn()
	}()
	duration := s.clock.Now().Sub(start)

	s.mu.Lock()
	state.running = false
	state.lastExecution = s.clock.Now()
	state.executionCount++
	if err != nil {
		state.lastStatus = "failure"
		state.lastError = err.Error()
	} else {
		state.lastStatus = "success"
		state.lastError = ""
	}

	record := schedule.ExecutionRecord{
		Timestamp: state.lastExecution,
		Duration:  duration,
		Status:    state.lastStatus,
		Error:     state.lastError,
	}
	state.history = append([]schedule.ExecutionRecord{record}, state.history...)
	if len(state.history) > 10 {
		state.history = state.history[:10]
	}
	s.mu.Unlock()
}
