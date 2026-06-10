// Package restore holds the domain Executor + Planner + Pipeline + Source for
// restore orchestration. The single Executor type replaces the CLI ↔ scheduler
// orchestration duplication called out in ADR 0001 Consequences. Executor
// receives every dependency as a ports.* value via constructor injection:
// ports.RestoreBuilder, ports.StorageBackend, ports.DecryptReader,
// ports.Hasher, ports.Recorder, ports.Dispatcher, ports.LockManager,
// ports.DBProber.
//
// Spec 037 — domain extraction. Allowed imports: stdlib, internal/ports,
// internal/sanitize, sibling internal/domain/*. No I/O.
package restore
