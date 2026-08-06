---
title: Running restore jobs in parallel
description: Run every enabled restore job at once as a recovery drill, bound the concurrency, and read the per-job summary.
sidebar_position: 18
---

Run every enabled restore job concurrently with one command, so a disaster-recovery drill across N
services takes as long as the slowest service rather than the sum of all of them.

## When to use this

Use this when you have one restore job per service, or per tenant, and you want to exercise all of
them together: a scheduled recovery drill, a CI gate that proves last night's backups are
restorable, or a real recovery where several services must come back at once.

Do not use it to restore several databases inside one job. A restore job targets exactly one
database, and there is no multi-database auto-discovery on the restore side. Parallelism here is on
the job axis only.

Do not reach for it when a single job is what you want. `sentinel restore run <job-name>` is
unchanged, and `--parallel` has no effect without `--all`.

## Before you start

- A configuration containing at least two restore jobs. Each is covered in
  [Restore](../concepts/restore.md); this guide only adds the fan-out.
- **`enabled: true` on every job you want included.** Restore jobs default to disabled, and `--all`
  runs only the enabled ones. A job with no `enabled` key is skipped silently.
- **A `schedule` on every enabled job.** Configuration validation requires a cron expression once a
  restore job is enabled, so enabling a job for `--all` also registers it with the scheduler. There
  is no way to mark a job manual-only.
- A writable lock directory. `scheduler.lock_dir` defaults to `/var/run/sentinel`, which is usually
  not writable by a non-root user; point it somewhere your operator account owns.
- Enough free space in `staging_dir` for **all** concurrent artifacts at once. See
  [If it goes wrong](#if-it-goes-wrong).
- Target databases you are willing to lose.

## Steps

### 1. Enable the jobs and set the limit

`max_concurrent_restores` is a top-level key. It defaults to `1`, so parallelism is opt-in, and
configuration validation rejects any value outside `1` to `100`.

```yaml
version: "1.0"
max_concurrent_restores: 4

restore:
  staging_dir: /var/lib/sentinel/staging

scheduler:
  lock_dir: /var/lib/sentinel/locks

restores:
  svc-a-verify:
    enabled: true
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: SENTINEL_PGPASSWORD
    database: svc_a_restore_check
    schedule: "0 4 * * *"
    backup_source:
      type: local
      local_path: /var/backups/sentinel
      backup_path: "svc-a_*.sql"
      use_latest_match: true

  svc-b-verify:
    enabled: true
    type: mysql
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: SENTINEL_MYSQL_PASSWORD
    database: svc_b_restore_check
    schedule: "0 4 * * *"
    backup_source:
      type: local
      local_path: /var/backups/sentinel
      backup_path: "svc-b_*.sql"
      use_latest_match: true
```

Validate before running anything:

```bash
sentinel config validate --config sentinel.yaml
```

### 2. Confirm each job resolves the artifact you expect

```bash
sentinel restore dry-run svc-a-verify --config sentinel.yaml
sentinel restore dry-run svc-b-verify --config sentinel.yaml
```

Dry-run reads the configuration only. It does not contact the storage backend or the database, so
treat it as a check on job wiring, not on artifact availability.

### 3. Run the fan-out

:::danger Destructive
Every job in the fan-out writes to the database named by its `database:` key, and with
`conflict_strategy: replace` it can drop objects there. Confirm each target with `restore dry-run`
above, verify the artifacts you are about to restore with
[`sentinel backup verify`](../reference/cli/backup.md), and point every job at a database you can
afford to lose. A drill that overwrites production is not a drill.
:::

```bash
sentinel restore run --all --config sentinel.yaml
```

Override the configured limit for one invocation with `--parallel`:

```bash
sentinel restore run --all --parallel 5 --config sentinel.yaml
```

The effective limit is resolved in this order:

| Source | Applies when |
|---|---|
| `--parallel N` | `N` is greater than zero |
| `max_concurrent_restores` | the flag is absent or zero, and the key is set |
| `1` | neither of the above |

`--parallel` is not range-checked the way `max_concurrent_restores` is; a value of `500` is accepted
and used. Zero and negative values fall through to the configured limit rather than erroring.

:::warning Two different keys set restore concurrency
`restore run --all` reads the **top-level** `max_concurrent_restores`. The scheduler reads
`scheduler.max_concurrent_restores`, a separate key that does **not** inherit the top-level value
and independently defaults to `1`. Raising the top-level key alone leaves scheduled restores serial.
Set both if you want the same behaviour from both entry points.
:::

## Verify

Each job prints one line, then a summary:

```text
  svc-a-verify                   OK      Restore job "svc-a-verify" completed successfully
  svc-b-verify                   FAILED  backup "svc-b_2026-08-05.sql" not found in local source: restore source object not found: svc-b_2026-08-05.sql
Restore run-all: 1/2 succeeded (concurrency=4)
```

One job's failure never aborts its siblings; every job runs to completion, and a panic in one job is
contained rather than taking the batch down. The command exits non-zero if any job failed, so a
drill in cron or CI fails loudly.

With no enabled jobs the command prints `No enabled restore jobs to run.` and exits `0`. If you
expected work to happen, that message means `enabled: true` is missing, not that everything passed.

Every run is recorded individually in the execution history, which is safe to write concurrently:

```bash
sentinel restore history svc-a-verify --config sentinel.yaml
```

Notifications are also per job. There is no aggregate drill summary notification.

## If it goes wrong

**Nothing ran.** `No enabled restore jobs to run.` Add `enabled: true` and a `schedule` to each job.

**A named job you thought was disabled ran anyway.** `sentinel restore run <job-name>` executes the
job whether or not it is enabled; the `enabled` flag governs only `--all` and the scheduler
([issue #139](https://github.com/denisakp/sentinel/issues/139)). Do not rely on `enabled: false` as a
safety catch on an explicit invocation.

**You disabled a job with the CLI and it still runs.** `restore enable`, `restore disable`,
`restore pause`, and `restore resume` print a success message and change nothing. They do not write
to the configuration file ([issue #137](https://github.com/denisakp/sentinel/issues/137)). Only
`enabled:` in the YAML has any effect, which is why `restore status` still reports `disabled`
straight after `restore enable` claims success.

**Every job failed after the data landed.** If the jobs set `verify_after_restore: true`, each one
fails with `verification handler is required for restore mode "full"` **after** the restore has
already written to the target. No verification handler is wired into the execution path, so the
option cannot succeed ([issue #149](https://github.com/denisakp/sentinel/issues/149)). Leave it unset
and verify the restored database yourself. The same applies implicitly to `pitr` and `incremental`
modes, which require verification.

**`failed to acquire restore lock`.** The lock directory is not writable:
`mkdir "/var/run/sentinel": permission denied`. Set `scheduler.lock_dir` to a path your account owns.

**The disk filled part-way through.** The staging capacity preflight is evaluated per job, against
free space at the moment that job starts. Four concurrent jobs each see the same free space and each
passes, then collectively exhaust it. Size `staging_dir` for the sum of the concurrent artifacts, not
the largest one.

**A job reports `lock_conflict`.** Another run of that same job is still in progress. The fan-out
does not queue it; the job is recorded as skipped.

## Related

- [Restore](../concepts/restore.md): the sequence one job goes through, and every reason code.
- [`sentinel restore`](../reference/cli/restore.md): every subcommand and flag.
- [Configuration reference](../reference/configuration.md): `max_concurrent_restores`,
  `scheduler.max_concurrent_restores`, and the restore job keys.
- [Restoring from Google Cloud Storage](./restore-from-gcs.md): a single-job restore end to end.
- [Locking](../concepts/locking.md): what the per-job lock protects.
- [Inspecting execution history](./inspect-monitor-history.md): reading what a drill recorded.
- [Verifying backup integrity](./verify-backup-integrity.md): confirming an artifact before you
  restore it.

<!-- sources: internal/cli/restore.go, internal/config/loader.go, internal/config/validator.go, internal/config/types.go, internal/config/restore_types.go, internal/cli/schedule.go, internal/adapters/restore/runtime/staging.go, internal/domain/restore/executor.go, docs/runbooks/parallel-restore.md -->
