//go:build !(linux || darwin)

package runtime

// availableStagingBytes has no capacity-check support on non-POSIX targets;
// ok=false tells ensureStagingCapacity to skip the check rather than fail.
func availableStagingBytes(_ string) (available int64, ok bool, err error) {
	return 0, false, nil
}
