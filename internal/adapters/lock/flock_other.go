//go:build !(linux || darwin)

package lock

import "os"

// Production builds for non-POSIX targets must fail. The const below makes
// the failure obvious at compile time even if a future call path bypasses
// the stub functions.
const _ = "sentinel internal/lock requires linux or darwin"

func tryFlockEx(_ *os.File) error { return ErrUnsupportedPlatform }
func funlock(_ *os.File) error    { return ErrUnsupportedPlatform }
