# Runbook — Stale lock recovery

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: ADR 0007 (lock file format), [scheduler-crash-recovery](./scheduler-crash-recovery.md), [troubleshooting](./troubleshooting.md)

## When to use

Sentinel reports `lock: held by another holder` or jobs persistently show `skipped` in `monitor list --status skipped`. Or you found leftover `*.lock` files after a host reboot / SIGKILL.

## Preconditions

- Read access to the operator-configured lock directory.
- `jq` available for inspection.

## Steps

### Inspect the lock file

```bash
ls -la <lock-dir>/
cat <lock-dir>/<job-name>.lock | jq .
```

Expected payload (v1 JSON, see ADR 0007):

```json
{
  "pid": 12345,
  "job_name": "postgres-prod",
  "start_time": "2026-05-17T03:21:08Z",
  "hostname": "sentinel-host-01"
}
```

### Decide stale (dual criterion)

The package marks a lock removable only when **both** hold:

1. The recorded PID is **not alive** on the current host (`signal(0)` returns `ESRCH`).
2. Lock **age > operator-configured threshold** (`now − start_time`).

Reference: `EvaluateLockState` in `internal/adapters/lock/state.go`.

Check the PID:

```bash
ps -p <pid> -o pid,etime,cmd       # exits non-zero if dead
date -u                             # compare against start_time
```

**Cross-host caveat (ADR 0007)**: if `hostname` in the payload differs from the current host, the PID check is meaningless. Only the age threshold applies. Size the threshold accordingly when sharing a lock directory across hosts.

### Let startup reap handle it

The CLI scans `<lock-dir>/*.lock` on every start and removes stale entries before normal operation. Simply re-running the next `sentinel` invocation is typically enough.

### Manual removal (only if startup reap is insufficient)

After confirming the criteria above:

```bash
rm <lock-dir>/<job-name>.lock
```

Empty or malformed lock files are also reaped at startup.

## Typed errors operators may see

From `internal/adapters/lock/errors.go` (and `internal/ports/lock.go` for `ErrLockHeld` / `ErrLockUnsupported` / `ErrLockIO` / `ErrLockExists`):

- `ErrLockHeld` — another holder owns the lock (live PID, non-stale recorded holder, or uncontended flock).
- `ErrLockUnsupported` — underlying filesystem does not support advisory file locks (some NFS configs).
- `ErrLockIO` — filesystem error not held/unsupported.
- `ErrUnsupportedPlatform` — non-POSIX build target (production should fail at compile time).
- `ErrLockExists` — deprecated alias of `ErrLockHeld`.

`HeldError` (wraps `ErrLockHeld`) prints `pid`, `host`, `age`, `live` for the holder.

## Verification

```bash
ls <lock-dir>/                                # empty after reap
sentinel monitor list --status skipped --last 1h   # should stop accumulating
```

Re-run the job that was blocked.

## Rollback / recovery

Deleting an actively-held lock corrupts running jobs. **Never** `rm` a lock without first confirming both stale criteria.

## References

- ADR 0007 — Lock file format v1 (`docs/adr/0007-lock-file-format.md`)
- `internal/adapters/lock/lock.go`, `internal/adapters/lock/state.go`, `internal/adapters/lock/errors.go`, `internal/ports/lock.go`
- Spec: `specs/010-lock-toctou/`
