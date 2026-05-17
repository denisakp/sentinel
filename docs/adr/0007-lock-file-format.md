# ADR 0007 — Lock file format v1 (per-job JSON, O_EXCL, PID-based stale detection)

- **Status**: Accepted (TOCTOU window closed by PRD 10 / `specs/010-lock-toctou/`; v1 stale-detection contract now matches the implementation)
- **Date**: 2026-05-17
- **Deciders**: Denis AKPAGNONITE
- **Tags**: lock, concurrency, format

## Context

Sentinel prevents concurrent execution of the same job by acquiring a per-job file lock before backup or restore. The implementation at `internal/lock/lock.go` works as follows:

- One lock file per job: `<lock-dir>/<job-name>.lock`.
- Lock acquisition opens the file with `O_CREATE|O_EXCL|O_WRONLY` and mode `0600`. The atomicity of `O_EXCL` is the mutual-exclusion mechanism.
- Payload is a JSON `JobLock` carrying `PID`, `JobName`, `StartTime` (UTC), `Hostname`.
- Release deletes the file.
- Stale detection: `CheckStale(jl, threshold)` returns true when the recorded PID is not alive (`os.FindProcess` + `proc.Signal(syscall.Signal(0))`) **and** the lock age exceeds `threshold`. Both conditions must hold.
- On startup, the CLI scans all `*.lock` files in the lock directory; stale ones are reaped before normal operation.

The lock directory is operator-configured. The format has been stable since v1.1.

This ADR freezes that contract and places the package under the hexagonal layout (ADR 0001).

## Decision drivers

- Atomic mutual exclusion without a process supervisor; Sentinel may run from cron or interactively.
- Crash-resilient: a SIGKILL'd backup must not leave a permanent lock that requires manual intervention.
- Cross-host safety: lock files may live on shared storage (NFS, SMB) where PID checks across hosts are meaningless.
- The format must be readable by tooling (`cat foo.lock | jq`) for incident response.
- The package becomes an adapter under ADR 0001; the port lives in `internal/ports/lock.go`.

## Options considered

### Option A — `flock(2)`-based advisory locks

POSIX advisory file locks, released automatically on process exit.

**Pros**
- Auto-release on crash.

**Cons**
- Behaviour on NFS varies and historically has been unreliable.
- No payload: a third party cannot see who holds the lock without consulting `lsof`.
- Operators cannot inspect with standard tools.

### Option B — Distributed lock service (etcd, Consul, Redis)

Push lock state to an external coordinator.

**Cons**
- Adds an operational dependency Sentinel explicitly avoids (single-binary, no daemon).
- Overkill for a single-node operator tool.

### Option C — File-based JSON lock with `O_EXCL` + PID-based stale reap

The current implementation.

**Pros**
- Atomic creation via `O_EXCL` on every filesystem that supports `open(2)` semantics, including most network filesystems for the create case.
- Payload is inspectable by `cat` and `jq`; operators can answer "who is holding this lock and since when".
- Stale reaping handles crashes without manual cleanup.
- No external dependency.

**Cons**
- PID-based liveness check is host-local. A lock created on host A cannot be reliably probed from host B. Mitigated by `Hostname` in the payload and by the dual condition (dead PID + age > threshold).
- `O_EXCL` on some network filesystems (older NFSv2/v3) is not atomic. Operators using such filesystems must place the lock directory on local disk.
- The two-step write (create then write payload) leaves a window where a crash produces an empty lock. Acceptable: the empty lock is detected as malformed and reaped.

## Decision

Sentinel freezes the lock file format as **v1**:

- **Filename**: `<lock-dir>/<job-name>.lock`. `job-name` is the operator-defined name from the YAML schedule entry.
- **Creation**: `os.OpenFile(path, O_CREATE|O_EXCL|O_WRONLY, 0600)`. The atomicity of `O_EXCL` is the only mutual-exclusion mechanism.
- **Payload**: JSON object with the fields `pid` (int, OS PID), `job_name` (string), `start_time` (RFC 3339 UTC), `hostname` (string). Implemented by `JobLock` in `internal/lock/types.go`.
- **Release**: `os.Remove(path)`. A missing file on release is not an error.
- **Stale criteria**: a lock is stale if **both** of the following hold:
  - The PID is not alive on the current host (`os.FindProcess(pid).Signal(syscall.Signal(0))` returns an error, typically `ESRCH`), **and**
  - The lock age (now − `start_time`) exceeds an operator-configured threshold.
- **Startup reap**: on process start, the CLI lists `<lock-dir>/*.lock`, evaluates each against the stale criteria, and removes stale entries before any new acquisition attempt.
- **Cross-host caveat**: when `hostname` in the payload differs from the current host, the PID check is skipped and the age threshold alone determines staleness. Operators sharing a lock directory across hosts must size the threshold accordingly.

The package moves to `internal/adapters/lock/`. The interface (port) lives at `internal/ports/lock.go` and exposes `RunWithLock(jobName, fn) error` and `RunWithTimeout(jobName, timeout, fn) error`. Adapters of `Locker` may, in future, implement non-file mechanisms (e.g. Redis) without touching the domain.

## Consequences

### Positive
- Operators can inspect lock state with `cat`, `jq`, and `ls -la`.
- Crash recovery is automatic on the next invocation.
- The format is now ADR-governed; any future change is deliberate.

### Negative
- The two-step create-then-write leaves a window for an empty lock file on a crash between `OpenFile` and `Write`. Mitigation: any unparsable lock is treated as malformed and reaped on startup. *(follow-up: add the malformed-lock branch to `CheckStale` or to the startup reaper if not already present.)*
- Lock files on certain network filesystems may not honour `O_EXCL` atomically. Documented as an operator constraint in the runbook.
- Cross-host PID checks are meaningless; cross-host operators rely on the age threshold alone.

### Neutral / to watch
- Whether to write the payload atomically (temp file + rename) instead of create-then-write. Adds a step; eliminates the empty-lock window. Tracked as a follow-up.
- Whether `JobLock` should include a `version` field. Today it does not; future fields can be added since unknown JSON fields are tolerated on read.

## Compatibility & migration

- **On-disk format**: this ADR documents the existing v1.1+ implementation. No migration.
- **Locks left from older runs**: continue to work; missing fields parse as zero values and are reaped by the threshold check.
- **Sentinel.yaml**: unaffected.
- **CLI**: no flag changes.

## Implementation checklist

- [ ] Move `internal/lock/` to `internal/adapters/lock/`.
- [ ] Create `internal/ports/lock.go` exposing `Locker`, `RunWithLock`, `RunWithTimeout`.
- [ ] Add the empty-lock / malformed-lock branch to the startup reaper if not already covered, so a crashed empty lock is removed automatically.
- [ ] Add a package doc comment in `internal/adapters/lock/lock.go` summarising the v1 contract.
- [ ] Cross-reference this ADR from `docs/runbooks/` once an incident-response runbook exists.

## Implementation (PRD 10)

Promoted to Accepted on completion of `specs/010-lock-toctou/`. Key changes that close the gap between the contract above and the code:

- Mutual exclusion is now backed by `syscall.Flock(LOCK_EX|LOCK_NB)` on the lock file fd (kernel-enforced); `O_CREATE|O_EXCL` is no longer the sole gate.
- The dual stale criterion (PID-dead AND age > threshold) is enforced inside `internal/lock` itself via `EvaluateLockState`; callers no longer re-implement it.
- Body writes go through a single `Write([]byte)` under the held flock (< 4 KiB / PIPE_BUF), so no observer sees a half-written file.
- Typed errors `ErrLockHeld`, `ErrLockUnsupported`, `ErrLockIO`, `ErrUnsupportedPlatform` (plus `ErrLockExists` as a deprecated alias of `ErrLockHeld`) make failure modes distinguishable via `errors.Is`.
- Three acquisition modes (`TryAcquire`, `AcquireContext`, `AcquireWithTimeout`) share one core path.
- Build target restricted to `linux || darwin`; non-POSIX targets return `ErrUnsupportedPlatform` at first call.

The on-disk v1 JSON shape (`PID`, `JobName`, `StartTime`, `Hostname`) is unchanged and pinned by a golden test (`TestJobLock_JSONShape_v1Stable`).

## References

- ADR 0001 — Adopt hexagonal architecture (port location).
- `internal/lock/lock.go`, `internal/lock/types.go` — current implementation.
- `.prds/10-lock-toctou.md`, `specs/010-lock-toctou/` — the fix that promoted this ADR to Accepted.
