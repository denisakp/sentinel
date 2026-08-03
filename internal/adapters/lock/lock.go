package lock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

const maxBodyBytes = 4096 // POSIX guarantees atomic writes up to PIPE_BUF.

// Manager handles file-based job locking with kernel-enforced advisory locks.
type Manager struct {
	dir string

	mu              sync.Mutex
	held            map[string]*os.File // jobName -> fd holding the flock
	unsupportedOnce sync.Once
}

// NewManager returns a Manager that stores lock files in dir.
// The directory is created lazily on first acquire.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir, held: make(map[string]*os.File)}
}

// lockPath returns the full path for the named job's lock file.
func (m *Manager) lockPath(jobName string) string {
	return filepath.Join(m.dir, jobName+".lock")
}

// writeBody marshals jl and writes it to f atomically (single Write under
// flock). Swappable for tests.
var writeBody = func(_ *Manager, f *os.File, jl *ports.JobLock) error {
	body, err := json.Marshal(jl)
	if err != nil {
		return fmt.Errorf("lock: marshal: %w", err)
	}
	if len(body) >= maxBodyBytes {
		return fmt.Errorf("lock: body too large for atomic write: %d bytes", len(body))
	}
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("lock: truncate: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("lock: seek: %w", err)
	}
	if _, err := f.Write(body); err != nil {
		return fmt.Errorf("lock: write: %w", err)
	}
	return nil
}

// tryAcquireOnce makes a single non-blocking attempt to acquire the lock.
// On success returns (*ports.JobLock, nil) and the Manager retains the flock'd
// fd in m.held for later Release. On contention returns *ports.HeldError wrapping
// ports.ErrLockHeld. On unsupported FS returns an error wrapping ports.ErrLockUnsupported.
func (m *Manager) tryAcquireOnce(jobName string, staleThreshold time.Duration) (*ports.JobLock, error) {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, fmt.Errorf("%w - mkdir %q: %v", ports.ErrLockIO, m.dir, err)
	}

	path := m.lockPath(jobName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w - open %q: %v", ports.ErrLockIO, path, err)
	}

	if err := tryFlockEx(f); err != nil {
		_ = f.Close()
		if errors.Is(err, ports.ErrLockHeld) {
			state := m.peekState(path, staleThreshold)
			m.logContention(jobName, state)
			return nil, &ports.HeldError{State: state}
		}
		if errors.Is(err, ports.ErrLockUnsupported) {
			m.logUnsupported(err)
		}
		return nil, err
	}

	// Flock held. Inspect current content.
	existing, parseErr := readLockFile(f)
	if parseErr == nil && existing != nil {
		state := EvaluateLockState(existing, staleThreshold)
		if !state.Removable {
			_ = funlock(f)
			_ = f.Close()
			m.logContention(jobName, state)
			return nil, &ports.HeldError{State: state}
		}
		// Stale → replace below.
		slog.Info("stale lock removed",
			"event", "stale_lock_removed",
			"job", jobName,
			"stale_pid", state.PID,
			"age_seconds", int64(state.Age.Seconds()),
		)
	}

	jl := newJobLock(jobName)
	if err := writeBody(m, f, jl); err != nil {
		_ = funlock(f)
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}

	m.mu.Lock()
	m.held[jobName] = f
	m.mu.Unlock()
	return jl, nil
}

func (m *Manager) peekState(path string, staleThreshold time.Duration) ports.LockState {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ports.LockState{}
	}
	var jl ports.JobLock
	if err := json.Unmarshal(data, &jl); err != nil {
		return ports.LockState{}
	}
	return EvaluateLockState(&jl, staleThreshold)
}

func (m *Manager) logContention(jobName string, state ports.LockState) {
	slog.Info("lock contention rejected",
		"event", "lock_contention_rejected",
		"job", jobName,
		"holder_pid", state.PID,
		"holder_age_seconds", int64(state.Age.Seconds()),
	)
}

func (m *Manager) logUnsupported(err error) {
	m.unsupportedOnce.Do(func() {
		slog.Warn("advisory locks unsupported on this filesystem",
			"event", "lock_unsupported_filesystem",
			"lock_dir", m.dir,
			"error", err.Error(),
		)
	})
}

// TryAcquire is the non-blocking canonical entry point.
func (m *Manager) TryAcquire(jobName string, staleThreshold time.Duration) (*ports.JobLock, error) {
	return m.tryAcquireOnce(jobName, staleThreshold)
}

// AcquireContext blocks until the lock is acquired, ctx is cancelled, or its
// deadline fires. On ctx fire it returns ctx.Err() (NOT ports.ErrLockHeld). Polls
// with exponential backoff (50 ms → 1 s cap).
func (m *Manager) AcquireContext(ctx context.Context, jobName string, staleThreshold time.Duration) (*ports.JobLock, error) {
	const startDelay = 50 * time.Millisecond
	const maxDelay = 1 * time.Second
	delay := startDelay
	for {
		jl, err := m.tryAcquireOnce(jobName, staleThreshold)
		if err == nil {
			return jl, nil
		}
		if !errors.Is(err, ports.ErrLockHeld) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		if delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}

// AcquireWithTimeout is a bounded-wait wrapper around AcquireContext.
func (m *Manager) AcquireWithTimeout(ctx context.Context, jobName string, staleThreshold, timeout time.Duration) (*ports.JobLock, error) {
	if timeout <= 0 {
		return m.AcquireContext(ctx, jobName, staleThreshold)
	}
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return m.AcquireContext(ctx2, jobName, staleThreshold)
}

// Acquire is the legacy non-blocking entry point. It is equivalent to
// TryAcquire(jobName, 0) — no automatic stale replacement — and returns
// ports.ErrLockExists (== ports.ErrLockHeld) when contended, preserving the original
// errors.Is contract.
//
// Deprecated: use TryAcquire(jobName, staleThreshold) for new code.
func (m *Manager) Acquire(jobName string) (*ports.JobLock, error) {
	jl, err := m.tryAcquireOnce(jobName, 0)
	if err != nil {
		var he *ports.HeldError
		if errors.As(err, &he) {
			return nil, ports.ErrLockExists
		}
		return nil, err
	}
	return jl, nil
}

// Release releases the advisory lock and removes the lock file. It is
// idempotent: returns nil if the file is gone AND no fd is held for jobName.
//
// Order: remove the file first (so a racing acquirer that sees no file gets
// a brand-new inode with its own flock), then funlock+close our fd.
func (m *Manager) Release(jobName string) error {
	m.mu.Lock()
	f := m.held[jobName]
	delete(m.held, jobName)
	m.mu.Unlock()

	path := m.lockPath(jobName)
	removeErr := os.Remove(path)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		removeErr = fmt.Errorf("lock: remove %q: %w", path, removeErr)
	} else {
		removeErr = nil
	}

	if f != nil {
		_ = funlock(f)
		_ = f.Close()
	}
	return removeErr
}

// ReadLock reads the current lock file for jobName without taking any
// kernel lock. Returns (nil, nil) when absent. Diagnostic only.
func (m *Manager) ReadLock(jobName string) (*ports.JobLock, error) {
	path := m.lockPath(jobName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("lock: read %q: %w", path, err)
	}
	var jl ports.JobLock
	if err := json.Unmarshal(data, &jl); err != nil {
		return nil, fmt.Errorf("lock: parse %q: %w", path, err)
	}
	return &jl, nil
}

// Inspect takes a transient flock, evaluates ports.LockState, releases the flock,
// and returns. Returns (nil, nil) when the lock file is absent.
func (m *Manager) Inspect(jobName string, staleThreshold time.Duration) (*ports.LockState, error) {
	path := m.lockPath(jobName)
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("lock: open %q: %w", path, err)
	}
	defer f.Close()

	if err := tryFlockEx(f); err != nil {
		if errors.Is(err, ports.ErrLockHeld) {
			state := m.peekState(path, staleThreshold)
			return &state, nil
		}
		return nil, err
	}
	defer funlock(f)

	jl, parseErr := readLockFile(f)
	if parseErr != nil || jl == nil {
		return &ports.LockState{}, nil
	}
	state := EvaluateLockState(jl, staleThreshold)
	return &state, nil
}

// ListLockFiles returns all *.lock file paths in the lock directory.
func (m *Manager) ListLockFiles() ([]string, error) {
	pattern := filepath.Join(m.dir, "*.lock")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("lock: glob: %w", err)
	}
	return matches, nil
}

// ScanStale walks the lock directory and removes any lock file that is
// (a) parseable, (b) belongs to this host, (c) has a non-live PID, and
// (d) has age exceeding staleThreshold. Foreign-host locks and live
// holders are skipped. The same evaluator as TryAcquire is used (G6).
func (m *Manager) ScanStale(staleThreshold time.Duration) (removed []string, err error) {
	files, listErr := m.ListLockFiles()
	if listErr != nil {
		return nil, listErr
	}
	hostname, _ := os.Hostname()
	for _, path := range files {
		base := filepath.Base(path)
		jobName := strings.TrimSuffix(base, ".lock")

		jl, readErr := m.ReadLock(jobName)
		if readErr != nil || jl == nil {
			continue
		}
		if jl.Hostname != "" && hostname != "" && jl.Hostname != hostname {
			slog.Warn("foreign host lock detected — not cleaning",
				"event", "foreign_host_lock_detected",
				"job", jobName,
				"lock_host", jl.Hostname,
				"lock_pid", jl.PID,
			)
			continue
		}

		f, openErr := os.OpenFile(path, os.O_RDWR, 0o600)
		if openErr != nil {
			continue
		}
		if flockErr := tryFlockEx(f); flockErr != nil {
			_ = f.Close()
			continue // held by a live process; skip
		}
		state := EvaluateLockState(jl, staleThreshold)
		if !state.Removable {
			_ = funlock(f)
			_ = f.Close()
			continue
		}
		// Remove file first, then unlock — matches Release ordering.
		if rmErr := os.Remove(path); rmErr == nil {
			removed = append(removed, path)
			slog.Info("stale lock removed",
				"event", "stale_lock_removed",
				"job", jobName,
				"stale_pid", state.PID,
				"age_seconds", int64(state.Age.Seconds()),
			)
		}
		_ = funlock(f)
		_ = f.Close()
	}
	return removed, nil
}

// readLockFile parses the JSON body from f (does not change file position
// long-term; caller should expect position == EOF on return for empty files).
func readLockFile(f *os.File) (*ports.JobLock, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var jl ports.JobLock
	if err := json.Unmarshal(data, &jl); err != nil {
		return nil, err
	}
	return &jl, nil
}

func newJobLock(jobName string) *ports.JobLock {
	hostname, _ := os.Hostname()
	return &ports.JobLock{
		PID:       os.Getpid(),
		JobName:   jobName,
		StartTime: time.Now().UTC(),
		Hostname:  hostname,
	}
}
