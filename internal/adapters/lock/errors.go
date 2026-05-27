package lock

import "errors"

// ErrUnsupportedPlatform is returned when the lock package is built
// for a non-POSIX target. Production builds should fail at compile time.
//
// This sentinel is platform-build-time only and never appears in port
// method signatures, so it stays in the adapter rather than relocating
// to internal/ports/.
var ErrUnsupportedPlatform = errors.New("lock: unsupported platform (linux or darwin required)")
