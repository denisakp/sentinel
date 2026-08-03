//go:build linux || darwin

package mongo_tls

import (
	"errors"
	"syscall"
)

// processAlive returns true iff pid is a currently-running process.
// Uses syscall.Kill(pid, 0): a successful return or EPERM means alive;
// ESRCH means dead.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
