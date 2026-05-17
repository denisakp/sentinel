# ADR 0008 — Scheduler concurrency model: bounded global semaphore + per-job file lock

- **Status**: Accepted (2026-05-17) — semaphore leak under panic resolved by feature `009-scheduler-semaphore-leak`; "leak-free under panic" assumption now holds, verified by `TestExecute_StressMixed`, `TestExecuteBackupWithCleanup_RecordsPanic`, and `go test -race ./internal/scheduler/...`. Per-job lock contract (ADR 0007) hardened by PRD 10 with no scheduler-side change required.
- **Date**: 2026-05-17
- **Deciders**: Denis AKPAGNONITE
- **Tags**: scheduler, concurrency, lock

## Context

The in-process scheduler at `internal/scheduler/` triggers backup and restore jobs on a cron-like schedule. Two distinct concurrency mechanisms cooperate:

1. **A bounded global semaphore** inside `Executor` (`internal/scheduler/executor.go`). Constructed by `NewExecutor(maxConcurrent)`; `Execute(job func())` blocks on a buffered channel of capacity `maxConcurrent`. This bounds the total number of jobs running in parallel across all schedules.
2. **A per-job file lock** acquired by every job before doing real work, via the package documented in ADR 0007. This prevents two runs of the **same** job from overlapping — whether they came from the scheduler, the CLI, or a separate process invocation.

Both mechanisms are required because they solve different problems:

- The semaphore protects shared host resources (CPU, memory, DB connections, network bandwidth) when many schedules fire near the same time.
- The per-job lock protects correctness: two concurrent backups of the same database produce ambiguous artefacts; two concurrent restores corrupt state.

This ADR records that choice and freezes it as the scheduler's concurrency contract.

## Decision drivers

- The scheduler is in-process. There is no daemon and no coordinator; concurrency control is host-local.
- The same job may be triggered from multiple places: the scheduler, an interactive CLI invocation, and a separate process started by an operator. The mutual-exclusion mechanism must work across all three.
- A long-running job must not silently block shorter ones forever: the semaphore must be bounded but not exotic (no priority queues, no preemption).
- Failures and cancellations must release both the semaphore slot and the file lock deterministically; `defer` is acceptable.

## Options considered

### Option A — Global semaphore only

Use only the `Executor` semaphore; rely on it to serialise everything.

**Cons**
- The semaphore does not know which job is which. Two concurrent runs of the same job both holding a semaphore slot is legal but incorrect.
- Does not protect against CLI invocations that bypass the scheduler.

### Option B — Per-job file lock only

Drop the semaphore; rely on locks to serialise per job.

**Cons**
- N independent jobs can all run in parallel and saturate host resources. A small VM with 4 GB RAM cannot run 8 simultaneous `pg_dump` against large databases.
- No backpressure mechanism: a misconfiguration with overlapping schedules causes a thundering herd.

### Option C — Per-job lock + global semaphore (current implementation)

The semaphore bounds total parallelism; the lock enforces per-job mutual exclusion. The lock is acquired **inside** the semaphore slot to avoid holding a slot while waiting for a lock.

**Pros**
- Two orthogonal invariants, each enforced by a single mechanism.
- The lock works across all triggers (scheduler, CLI, external process) because it is a filesystem artefact.
- The semaphore enforces a single tunable that operators understand (`max_concurrent`).
- Already implemented and tested (`internal/scheduler/executor_test.go`, `internal/scheduler/lock_integration.go`).

**Cons**
- Two mechanisms must be coordinated correctly. Acquire-order matters: semaphore first, then lock. Releasing in the opposite order is enforced by `defer`.
- If a job blocks on lock acquisition, it consumes a semaphore slot for that wait. Mitigated because the lock is non-blocking: `Acquire` returns `ErrLockExists` immediately if the lock is held, and the executor records that as a skipped run rather than queuing.

### Option D — Add a per-pool semaphore (e.g. one slot per DB engine)

Refine bounded parallelism by resource class.

**Pros**
- Finer control: PostgreSQL backups can be limited independently of MongoDB.

**Cons**
- Adds configuration surface (`max_concurrent_per_engine`) that no operator has requested.
- Out of scope today. May become a future ADR if real workloads demand it.

## Decision

Sentinel's scheduler uses the **per-job file lock + bounded global semaphore** model. Specifically:

- **Semaphore**: a buffered channel of capacity `max_concurrent` inside `Executor`. Default value is set by operator configuration; it is bounded to at least 1. `Execute(job)` blocks until a slot is available.
- **Per-job lock**: every job, before doing real work, acquires a file lock via the package frozen in ADR 0007. The lock name is the job name from the YAML schedule entry. A lock conflict (`ErrLockExists`) results in a recorded `skipped` outcome, not a queued wait.
- **Acquire order**: semaphore first, then file lock. Release order is the reverse, enforced by `defer`.
- **Cancellation**: jobs accept `context.Context`. Cancellation of the context releases the lock and the semaphore slot via `defer`. The scheduler does not preempt running jobs.
- **Stale locks**: handled per ADR 0007 (PID-dead + age > threshold). The scheduler does not maintain its own staleness state.
- **Skipped runs**: when a job is skipped due to a held lock, the monitor records it as a `skipped` execution. This is observable to the operator.

The scheduler package is a driving adapter under ADR 0001. It depends on `internal/ports/lock`, `internal/ports/recorder` (monitor), and the domain use cases.

## Consequences

### Positive
- Two orthogonal invariants, each with one mechanism: host-resource bound (semaphore), correctness bound (lock).
- The lock works across all triggers — scheduler, interactive CLI, ad-hoc process — because it is filesystem-resident.
- Cancellation semantics are simple: `defer` handles both releases.
- Skipped runs are visible in monitor history; operators can detect lock contention from the data.

### Negative
- Operators must reason about two knobs: `max_concurrent` and the per-job lock dir. Documented in `docs/runbooks/start-scheduler.md` and `README.md`.
- A misconfigured `max_concurrent = 1` serialises everything globally; operators must size it for the host.
- Skipped runs may hide an underlying scheduling problem. Mitigated by the monitor recording the outcome with a reason.

### Neutral / to watch
- Whether to add per-engine or per-database semaphores. Tracked as a possible follow-up ADR once a real workload demands it.
- Whether to expose semaphore-wait metrics. Today the monitor records start/end times; a future enhancement could record wait duration explicitly.

## Compatibility & migration

- **No on-disk format change**: this ADR documents the existing v1.1+ behaviour.
- **No `sentinel.yaml` change**: `max_concurrent` and lock-related fields stay where they are.
- **No CLI change**: scheduler invocation and output are unchanged.
- **Monitor schema**: the `skipped` outcome is already a recognised state in `internal/monitor/`; no migration required.

## Implementation checklist

- [x] Semaphore-leak-under-panic regression covered by `TestExecute_StressMixed` and `TestExecuteBackupWithCleanup_RecordsPanic` (`internal/scheduler/panic_*_test.go`); `-race` clean (feature `009-scheduler-semaphore-leak`).
- [x] Per-job lock acquisition now goes through the hardened `internal/lock` package (PRD 10): `ErrLockHeld` short-circuits to a `skipped` outcome without consuming a semaphore slot for any wait.
- [ ] Add a package doc comment on `internal/scheduler/executor.go` summarising the acquire/release order (semaphore-then-lock, reverse defer release) from this ADR.
- [ ] Once ADR 0001's migration completes, `internal/scheduler/executor.go` depends on `internal/ports/lock` and `internal/ports/recorder` rather than concrete packages.
- [x] Cross-reference this ADR from `docs/runbooks/start-scheduler.md` and `docs/runbooks/scheduler-crash-recovery.md`.

## References

- ADR 0001 — Adopt hexagonal architecture.
- ADR 0007 — Lock file format v1.
- `internal/scheduler/executor.go`, `internal/scheduler/lock_integration.go` — current implementation.
- `.prds/09-scheduler-executor-semaphore.md` — historical context that established the semaphore as it stands.
