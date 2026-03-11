package lock_test

import (
	"os"
	"sync"
	"testing"
	"time"

	sentlock "github.com/denisakp/sentinel/internal/lock"
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
	jl := &sentlock.JobLock{
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
	jl := &sentlock.JobLock{
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
