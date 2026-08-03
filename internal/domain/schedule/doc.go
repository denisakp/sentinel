// Package schedule holds the pure cron-model + next-run calculation. The cron
// loop runtime (goroutines, ticker, lifecycle) stays in internal/scheduler/.
//
// Spec 037 — domain extraction. Allowed imports: stdlib, internal/ports,
// internal/sanitize, sibling internal/domain/*. No I/O.
package schedule
