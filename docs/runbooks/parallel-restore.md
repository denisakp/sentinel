# Parallel Multi-Job Restore

> **Superseded by the documentation site: [guides/parallel-restore](https://denisakp.github.io/sentinel/guides/parallel-restore).**
>
> This runbook its example omits `enabled` and `schedule`, so no job in it would run. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

`sentinel restore run --all` runs every **enabled** configured restore job at once, bounded by a concurrency limit. It turns a disaster-recovery drill across N services (one restore job per service) from a serial sum-of-times into a parallel run.

> Scope: this parallelises **jobs** (the job axis). It does not restore multiple databases within a single job — a restore job targets exactly one database.

## Quick start

```bash
# Serial (default — behaviour unchanged):
sentinel restore run --all --config sentinel.yaml

# Parallel — raise the config limit, or override per-invocation:
sentinel restore run --all --parallel 5 --config sentinel.yaml
```

Single-job runs are unchanged:

```bash
sentinel restore run svc-a-restore --config sentinel.yaml
```

## Configuration

```yaml
# Global concurrency limit for `restore run --all`. Default 1 (serial).
# Parallelism is opt-in: raise this and/or pass --parallel.
max_concurrent_restores: 4

restores:
  svc-a-restore:
    type: postgres
    database: svc_a
    backup_source: { ... }
    # ...
  svc-b-restore:
    type: mysql
    database: svc_b
    backup_source: { ... }
  # ...one job per service
```

- **Default `max_concurrent_restores: 1`** preserves today's serial behaviour for anyone who does not opt in.
- Valid range: `1`–`100` (rejected at config load otherwise).

## Concurrency precedence

```
--parallel N   (if > 0)   >   max_concurrent_restores (config)   >   1 (default)
```

## Failure isolation

A run-all is a **drill**: one failing service must not stop the others.

- A job that fails does **not** abort or cancel the sibling jobs — they run to completion.
- Each job's outcome is reported individually:

  ```
    svc-a-restore                  OK      Restore job "svc-a-restore" completed successfully
    svc-b-restore                  FAILED  restore source not found: ...
    svc-c-restore                  OK      Restore job "svc-c-restore" completed successfully
  Restore run-all: 2/3 succeeded (concurrency=4)
  ```

- The command exits **non-zero** if any job failed, and zero if all succeeded — so a drill in CI/cron fails loudly when a service can't be restored.

## DR drill workflow

1. Configure one restore job per service (each pointing at that service's backup source and target database).
2. Set `max_concurrent_restores` to a value your hardware can sustain (each concurrent PostgreSQL PITR / `pg_combinebackup`, `mysql`, or `mongorestore` is a separate process — size CPU/RAM accordingly).
3. Run `sentinel restore run --all` on a drill schedule (or in CI).
4. Read the per-job summary; investigate any `FAILED` service; the non-zero exit flags the drill as failed.

## Multi-tenant restore

Model each tenant as a restore job (same backup source, different target database) and run `--all --parallel N` to restore many tenants concurrently instead of a shell loop.

## Notes

- Execution history is recorded per job and is safe under concurrency.
- One notification per job (unchanged) — there is no aggregate "drill summary" notification.
