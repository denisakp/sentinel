//go:build linux || darwin

package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// tryFlockEx takes an exclusive non-blocking advisory lock on f's fd.
// Returns ErrLockHeld (wrapped) if another holder has the lock; returns
// ErrLockUnsupported when the filesystem does not implement flock.
func tryFlockEx(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, syscall.EWOULDBLOCK):
		return ErrLockHeld
	case errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP),
		errors.Is(err, syscall.EINVAL):
		return fmt.Errorf("%w - flock: %v", ErrLockUnsupported, err)
	}
	return fmt.Errorf("%w - flock: %v", ErrLockIO, err)
}

// funlock releases the advisory lock on f's fd.
func funlock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("lock: funlock: %w", err)
	}
	return nil
}
