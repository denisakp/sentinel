//go:build linux || darwin

package runtime

import "syscall"

// availableStagingBytes reports free space on the filesystem holding dir.
// ok is always true on POSIX targets.
func availableStagingBytes(dir string) (available int64, ok bool, err error) {
	var stats syscall.Statfs_t
	if statErr := syscall.Statfs(dir, &stats); statErr != nil {
		return 0, false, statErr
	}
	return int64(stats.Bavail) * int64(stats.Bsize), true, nil
}
