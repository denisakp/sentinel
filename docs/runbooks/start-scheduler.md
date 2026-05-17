# Runbook — Start the scheduler

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: ADR 0008 (scheduler concurrency), [scheduler-crash-recovery](./scheduler-crash-recovery.md), [stale-lock-recovery](./stale-lock-recovery.md)

## When to use

Bring up the cron-driven scheduler that executes both `databases:` (backup) and `restores:` (restore) jobs. For one-shot runs use [run-backup-from-config](./run-backup-from-config.md).

## Preconditions

- Validated config (`sentinel config validate`).
- `password_env` and any `encryption_key_env` exported in the shell that starts the scheduler.
- `history_db_path` writable.
- Lock directory writable; if running across hosts, see ADR 0007 cross-host caveat.

## Steps

### Start the scheduler in the foreground

```bash
sentinel schedule start --config /workspace/infra/dataset/local.yaml
```

The process stays in the foreground. Use `tmux`, `nohup`, systemd, or supervisor for background operation. `sentinel schedule stop` only works inside the same process session — for foreground runs, send `SIGINT` (Ctrl-C).

### List registered jobs

```bash
sentinel schedule list --config /workspace/infra/dataset/local.yaml
```

Default table (v1.0.2+):

```text
TYPE    NAME              SCHEDULE        NEXT EXECUTION
backup  postgres-sample   */10 * * * *    2026-03-13T02:00:00Z
backup  mysql-sample      */10 * * * *    2026-03-13T02:00:00Z
backup  mariadb-sample    */10 * * * *    2026-03-13T02:00:00Z
```

For full fields (including `last_status`):

```bash
sentinel schedule list --config /workspace/infra/dataset/local.yaml --format json
sentinel schedule status postgres-sample --config /workspace/infra/dataset/local.yaml
```

### Schedule inheritance

Set `defaults.schedule` once; jobs without their own `schedule` inherit it. Whitespace-only `defaults.schedule` fails `sentinel config validate` with job-scoped cron context.

## Concurrency knobs (per ADR 0008)

- **Bounded global semaphore** in `internal/scheduler/executor.go` caps parallel jobs across all schedules. Tune the operator-configured `max_concurrent`; min 1.
- **Per-job file lock** prevents two runs of the same job from overlapping (any trigger source — scheduler, CLI, ad-hoc).
- A lock conflict records a `skipped` outcome (visible in `monitor list --status skipped`); it does not queue.
- Order: semaphore acquired first, then file lock; releases happen via `defer` in reverse.

## Verification

```bash
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 1h
sentinel monitor list --config /workspace/infra/dataset/local.yaml --status skipped --last 24h
```

Expect a record per fired schedule tick. Persistent `skipped` rows indicate lock contention — see [stale-lock-recovery](./stale-lock-recovery.md).

## Rollback / recovery

Stop with Ctrl-C in the foreground session, or `kill -TERM <pid>` for a backgrounded process. Restart re-reaps stale locks per ADR 0007. See [scheduler-crash-recovery](./scheduler-crash-recovery.md).

## References

- ADR 0008 — Scheduler concurrency model (`docs/adr/0008-scheduler-concurrency-model.md`)
- `internal/scheduler/scheduler.go`, `internal/scheduler/executor.go`
