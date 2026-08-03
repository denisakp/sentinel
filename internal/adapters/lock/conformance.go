package lock

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertion: a signature drift between *Manager and
// ports.LockManager fails the build here rather than at distant call sites.
var _ ports.LockManager = (*Manager)(nil)
