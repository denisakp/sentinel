# Runbook — Scheduler crash recovery

> **Superseded by the documentation site: [operations/scheduler-crash-recovery](https://denisakp.github.io/sentinel/operations/scheduler-crash-recovery).**
>
> This runbook same startup reap claim, and `monitor list --status skipped` can never return a row. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: ADR 0008 (scheduler concurrency), ADR 0007 (lock file format), [start-scheduler](./start-scheduler.md), [stale-lock-recovery](./stale-lock-recovery.md)

## When to use

Scheduler process was SIGKILL'd, panicked, or the host rebooted. You need to bring it back without losing or double-running jobs.

## Preconditions

- Same config and `history_db_path` as the previous run.
- Same `password_env` / `encryption_key_env` exported.

## Steps

### Restart the scheduler

```bash
sentinel schedule start --config /workspace/infra/dataset/local.yaml
```

On startup, the CLI scans `<lock-dir>/*.lock` and reaps stale entries per ADR 0007 (PID-dead AND age > threshold). The in-process semaphore is rebuilt from `max_concurrent`; there is no on-disk semaphore state to clean (ADR 0008).

### Reconcile last successful run

```bash
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 1h
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 24h --status failure
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 24h --status skipped
```

- A `failure` row at the time of the crash indicates an incomplete run. Re-run that job manually if needed.
- `skipped` rows after restart point at lingering lock conflicts — see [stale-lock-recovery](./stale-lock-recovery.md).

### Notification on missed window

If a notifier is configured for `failure` / `warning`, missed runs surface as `failed` (timeout) or `skipped` (`lock_conflict`, `concurrency_limit_reached`). See [alerting-setup](./alerting-setup.md) for event mapping.

## Why no semaphore cleanup is needed (ADR 0008)

- The bounded global semaphore is a buffered channel inside `Executor`. It is process-local; SIGKILL discards it.
- Per-job locks are filesystem-resident and survive a crash. They are handled by the startup reap.
- The semaphore-leak-under-panic regression is covered by `TestExecute_StressMixed` and `TestExecuteBackupWithCleanup_RecordsPanic` (feature `009-scheduler-semaphore-leak`).

## Verification

```bash
sentinel schedule list --config /workspace/infra/dataset/local.yaml
ls <lock-dir>/                                # no leftovers
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 10m
```

Next scheduled tick records a fresh row.

## Rollback / recovery

If the scheduler restart immediately produces `skipped` rows due to lock conflict with a phantom holder, follow [stale-lock-recovery](./stale-lock-recovery.md) before restarting again.

## References

- ADR 0008 — Scheduler concurrency model (`docs/adr/0008-scheduler-concurrency-model.md`)
- ADR 0007 — Lock file format v1 (`docs/adr/0007-lock-file-format.md`)
- `internal/scheduler/executor.go`, `internal/scheduler/scheduler.go`
- Feature: `specs/009-scheduler-semaphore-leak/`
