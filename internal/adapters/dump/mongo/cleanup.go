package mongo

import (
	"errors"
	"sync"
)

// cleanupRing tracks every currently-prepared *MongoTLSMaterial so the process
// can release them on a catchable signal (SIGINT/SIGTERM) where deferred Close
// calls would not run. The normal lifecycle remains defer m.Close() at the
// caller; the ring is a backstop, not the primary path.
var (
	cleanupMu  sync.Mutex
	cleanupSet = map[*MongoTLSMaterial]struct{}{}
)

// Register adds m to the process-wide cleanup ring. Safe for concurrent use.
// Idempotent.
func Register(m *MongoTLSMaterial) {
	if m == nil {
		return
	}
	cleanupMu.Lock()
	cleanupSet[m] = struct{}{}
	cleanupMu.Unlock()
}

// Unregister removes m from the ring. Idempotent.
func Unregister(m *MongoTLSMaterial) {
	if m == nil {
		return
	}
	cleanupMu.Lock()
	delete(cleanupSet, m)
	cleanupMu.Unlock()
}

// CloseAll closes every currently-registered material, aggregating errors via
// errors.Join. Intended only as a signal-handler backstop. Safe to call from a
// signal handler.
func CloseAll() error {
	cleanupMu.Lock()
	pending := make([]*MongoTLSMaterial, 0, len(cleanupSet))
	for m := range cleanupSet {
		pending = append(pending, m)
	}
	cleanupMu.Unlock()

	var errs []error
	for _, m := range pending {
		if err := m.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
