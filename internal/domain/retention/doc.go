// Package retention holds pure policy evaluation (count/age/size) for backup
// retention. The DELETE execution path lives in the driving adapter
// (internal/cli/retention.go) and reaches storage via ports.StorageBackend.
//
// Spec 037 — domain extraction. Allowed imports: stdlib, internal/ports,
// internal/sanitize, sibling internal/domain/*. No I/O.
package retention
