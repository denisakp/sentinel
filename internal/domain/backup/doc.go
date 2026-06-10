// Package backup holds the domain Executor + Planner + Pipeline + Source for
// backup orchestration. Executor receives every dependency as a ports.* value
// via constructor injection: ports.DumpBuilder, ports.StorageBackend,
// ports.EncryptWriter, ports.Hasher, ports.Recorder, ports.Dispatcher,
// ports.LockManager, ports.DBProber.
//
// Spec 037 — domain extraction. Allowed imports: stdlib, internal/ports,
// internal/sanitize, sibling internal/domain/*. No I/O.
package backup
