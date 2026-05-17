package lock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestAcquire_AtomicNoHalfWrite swaps writeBody with a failing wrapper and
// asserts the lock file never contains partial JSON after a failed claim.
func TestAcquire_AtomicNoHalfWrite(t *testing.T) {
	prev := writeBody
	t.Cleanup(func() { writeBody = prev })
	writeBody = func(_ *Manager, _ *os.File, _ *JobLock) error {
		return errors.New("simulated write failure")
	}

	dir := t.TempDir()
	m := NewManager(dir)
	_, err := m.TryAcquire("half", 0)
	if err == nil {
		t.Fatal("TryAcquire: expected failure")
	}
	path := filepath.Join(dir, "half.lock")
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("lock file unexpectedly exists after failed claim: stat err=%v", statErr)
	}
}
