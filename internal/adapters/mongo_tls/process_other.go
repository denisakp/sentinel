//go:build !(linux || darwin)

package mongo_tls

// processAlive has no liveness check on non-POSIX targets, so it
// conservatively reports every pid as alive: SweepOrphanMaterial then never
// removes material, which is safe (a missed cleanup, not a data-loss risk).
func processAlive(_ int) bool {
	return true
}
