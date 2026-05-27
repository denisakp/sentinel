package lock_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sentlock "github.com/denisakp/sentinel/internal/adapters/lock"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestAcquire_Atomicity(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)

	var successCount int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Acquire("test-job")
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if successCount != 1 {
		t.Errorf("Acquire() succeeded %d times, want exactly 1", successCount)
	}
}

func TestRelease_DeletesLockFile(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)

	if _, err := m.Acquire("job-x"); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := m.Release("job-x"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	// After release, acquiring again should succeed
	if _, err := m.Acquire("job-x"); err != nil {
		t.Errorf("Acquire() after Release() error = %v", err)
	}
}

func TestCheckStale_DeadPID(t *testing.T) {
	jl := &ports.JobLock{
		PID:       999999999,
		JobName:   "test",
		StartTime: time.Now().Add(-2 * time.Hour),
		Hostname:  "test-host",
	}

	// Dead PID + age > threshold → stale
	if !sentlock.CheckStale(jl, time.Hour) {
		t.Error("CheckStale() = false for dead PID with old lock, want true")
	}
}

func TestCheckStale_LivePID(t *testing.T) {
	jl := &ports.JobLock{
		PID:       os.Getpid(),
		JobName:   "test",
		StartTime: time.Now().Add(-2 * time.Hour),
		Hostname:  "test-host",
	}

	// Live PID → not stale regardless of age
	if sentlock.CheckStale(jl, time.Hour) {
		t.Error("CheckStale() = true for live PID, want false")
	}
}

func TestCheckStale_Nil(t *testing.T) {
	if sentlock.CheckStale(nil, time.Hour) {
		t.Error("CheckStale(nil) = true, want false")
	}
}

func TestReadLock_NotFound(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)

	jl, err := m.ReadLock("nonexistent-job")
	if err != nil {
		t.Fatalf("ReadLock() unexpected error = %v", err)
	}
	if jl != nil {
		t.Errorf("ReadLock() = %v, want nil for missing lock", jl)
	}
}

func TestReadLock_HappyPath(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)

	jobName := "read-test-job"
	acquired, err := m.Acquire(jobName)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	jl, err := m.ReadLock(jobName)
	if err != nil {
		t.Fatalf("ReadLock() error = %v", err)
	}
	if jl == nil {
		t.Fatal("ReadLock() returned nil for existing lock")
	}
	if jl.PID != acquired.PID {
		t.Errorf("ReadLock() PID = %d, want %d", jl.PID, acquired.PID)
	}
	if jl.JobName != jobName {
		t.Errorf("ReadLock() JobName = %q, want %q", jl.JobName, jobName)
	}
}

func TestReadLock_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)

	// Write a corrupt lock file manually
	lockPath := dir + "/corrupt-job.lock"
	if err := os.WriteFile(lockPath, []byte("not valid json"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := m.ReadLock("corrupt-job")
	if err == nil {
		t.Error("ReadLock() expected error for invalid JSON lock file")
	}
}

func TestListLockFiles_Empty(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)

	files, err := m.ListLockFiles()
	if err != nil {
		t.Fatalf("ListLockFiles() error = %v", err)
	}
	if len(files) != 0 {
		t.Errorf("ListLockFiles() = %v, want empty", files)
	}
}

func TestListLockFiles_WithFiles(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)

	jobs := []string{"job-alpha", "job-beta", "job-gamma"}
	for _, name := range jobs {
		if _, err := m.Acquire(name); err != nil {
			t.Fatalf("Acquire(%q) error = %v", name, err)
		}
	}

	files, err := m.ListLockFiles()
	if err != nil {
		t.Fatalf("ListLockFiles() error = %v", err)
	}
	if len(files) != len(jobs) {
		t.Errorf("ListLockFiles() count = %d, want %d", len(files), len(jobs))
	}
}

func TestAcquireRelease_FullCycle(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)

	// Acquire → ReadLock → Release → ReadLock (nil)
	if _, err := m.Acquire("full-cycle"); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	jl, err := m.ReadLock("full-cycle")
	if err != nil || jl == nil {
		t.Fatalf("ReadLock() after Acquire: err=%v jl=%v", err, jl)
	}
	if err := m.Release("full-cycle"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	jl, err = m.ReadLock("full-cycle")
	if err != nil {
		t.Fatalf("ReadLock() after Release: error = %v", err)
	}
	if jl != nil {
		t.Error("ReadLock() after Release should return nil")
	}
}

// --- US1: concurrent acquirers must produce exactly one winner ---

func TestAcquire_ConcurrentSingleWinner(t *testing.T) {
	const trials = 100
	const goroutines = 50
	for trial := 0; trial < trials; trial++ {
		dir := t.TempDir()
		m := sentlock.NewManager(dir)
		var wins int64
		var wg sync.WaitGroup
		var heldCount int64
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := m.TryAcquire("race", 0)
				if err == nil {
					atomic.AddInt64(&wins, 1)
					return
				}
				if errors.Is(err, ports.ErrLockHeld) {
					atomic.AddInt64(&heldCount, 1)
				} else {
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("trial %d: wins=%d, want 1", trial, wins)
		}
		if wins+heldCount != int64(goroutines) {
			t.Fatalf("trial %d: wins+held=%d, want %d", trial, wins+heldCount, goroutines)
		}
		_ = m.Release("race")
	}
}

// --- US1: release idempotent + safe vs racing acquirer ---

func TestRelease_Idempotent(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	if _, err := m.TryAcquire("idem", 0); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if err := m.Release("idem"); err != nil {
		t.Fatalf("Release #1: %v", err)
	}
	if err := m.Release("idem"); err != nil {
		t.Errorf("Release #2 (idempotent): %v", err)
	}
}

func TestRelease_OrderingSafeAgainstRacingAcquirer(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	if _, err := m.TryAcquire("ord", 0); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}

	var racerErr error
	var jl *ports.JobLock
	done := make(chan struct{})
	m2 := sentlock.NewManager(dir)
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		jl, racerErr = m2.AcquireContext(ctx, "ord", 0)
	}()

	time.Sleep(50 * time.Millisecond)
	if err := m.Release("ord"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	<-done
	if racerErr != nil {
		t.Fatalf("racer error: %v", racerErr)
	}
	if jl == nil {
		t.Fatal("racer got nil ports.JobLock")
	}
	_ = m2.Release("ord")
}

// --- US1: typed errors distinguishable via errors.Is/As ---

func TestErrorsAreDistinguishable(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	if _, err := m.TryAcquire("typed", 0); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer m.Release("typed")

	_, err := m.TryAcquire("typed", 0)
	if err == nil {
		t.Fatal("second acquire: expected error")
	}
	if !errors.Is(err, ports.ErrLockHeld) {
		t.Errorf("errors.Is(err, ports.ErrLockHeld) = false")
	}
	if !errors.Is(err, ports.ErrLockExists) {
		t.Errorf("errors.Is(err, ports.ErrLockExists) = false (alias broken)")
	}
	var he *ports.HeldError
	if !errors.As(err, &he) {
		t.Errorf("errors.As(err, *ports.HeldError) = false")
	}
}

// --- US2: PID reuse does not cause lock theft ---

func TestAcquire_DoesNotStealOnRecycledPID(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	host, _ := os.Hostname()
	jl := ports.JobLock{
		PID:       os.Getpid(),
		JobName:   "recycled",
		StartTime: time.Now().Add(-10 * time.Second),
		Hostname:  host,
	}
	body, _ := json.Marshal(jl)
	path := filepath.Join(dir, "recycled.lock")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	original, _ := os.ReadFile(path)

	_, err := m.TryAcquire("recycled", time.Minute)
	if err == nil {
		t.Fatal("TryAcquire: expected ports.HeldError, got success")
	}
	var he *ports.HeldError
	if !errors.As(err, &he) {
		t.Errorf("not ports.HeldError: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Errorf("lock file mutated:\n before=%s\n after=%s", original, after)
	}
}

// --- US2: stale lock replaced atomically ---

func TestAcquire_StaleLockReplacedAtomically(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	host, _ := os.Hostname()
	jl := ports.JobLock{
		PID:       999999999,
		JobName:   "stale",
		StartTime: time.Now().Add(-2 * time.Hour),
		Hostname:  host,
	}
	body, _ := json.Marshal(jl)
	path := filepath.Join(dir, "stale.lock")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := m.TryAcquire("stale", time.Hour)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	defer m.Release("stale")
	if got.PID != os.Getpid() {
		t.Errorf("PID after replace = %d, want %d", got.PID, os.Getpid())
	}
}

// --- US1 cont. (T034): blocking Acquire waits for release ---

func TestAcquire_BlockingWaitsForRelease(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	if _, err := m.TryAcquire("blk", 0); err != nil {
		t.Fatalf("initial: %v", err)
	}

	m2 := sentlock.NewManager(dir)
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := m2.AcquireContext(ctx, "blk", 0)
		done <- err
	}()

	time.Sleep(150 * time.Millisecond)
	if err := m.Release("blk"); err != nil {
		t.Fatalf("release: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("blocked acquire: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked acquire did not return after release")
	}
	_ = m2.Release("blk")
}

// --- T035: context cancel returns ctx.Err, not ports.ErrLockHeld ---

func TestAcquire_ContextCancelReturnsCtxErr(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	if _, err := m.TryAcquire("ctxc", 0); err != nil {
		t.Fatalf("initial: %v", err)
	}
	defer m.Release("ctxc")

	m2 := sentlock.NewManager(dir)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := m2.AcquireContext(ctx, "ctxc", 0)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if errors.Is(err, ports.ErrLockHeld) {
		t.Errorf("err also matches ports.ErrLockHeld; want only context.Canceled")
	}
}

// --- T036: AcquireWithTimeout respects deadline ---

func TestAcquireWithTimeoutRespectsDeadline(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	if _, err := m.TryAcquire("dl", 0); err != nil {
		t.Fatalf("initial: %v", err)
	}
	defer m.Release("dl")

	m2 := sentlock.NewManager(dir)
	start := time.Now()
	_, err := m2.AcquireWithTimeout(context.Background(), "dl", 0, 150*time.Millisecond)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if elapsed < 150*time.Millisecond || elapsed > 600*time.Millisecond {
		t.Errorf("elapsed = %s, want roughly 150-600ms", elapsed)
	}
}

// --- T039: Inspect returns ports.LockState without holding ---

func TestInspect_ReturnsLockStateWithoutHolding(t *testing.T) {
	dir := t.TempDir()
	m := sentlock.NewManager(dir)
	if _, err := m.TryAcquire("insp", 0); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer m.Release("insp")

	m2 := sentlock.NewManager(dir)
	state, err := m2.Inspect("insp", time.Minute)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state == nil {
		t.Fatal("Inspect returned nil state for existing lock")
	}
	if state.PID != os.Getpid() {
		t.Errorf("state.PID = %d, want %d", state.PID, os.Getpid())
	}
}
