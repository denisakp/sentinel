---
title: Schedule
description: How Sentinel's foreground cron loop registers backup and restore jobs, when they fire, and how concurrency and history work.
sidebar_position: 4
---

The schedule is Sentinel's cron loop: one process, started with `sentinel schedule start`, that
holds every job defined in your configuration and runs each one when its cron expression fires.
Backup jobs and restore jobs are registered by the same loop and treated the same way.

The loop runs in the foreground. It is not a daemon, it does not fork, and it does not install
anything into the host's crontab. **If `sentinel schedule start` is not running, nothing is
scheduled and nothing runs.** That is the single most important operational fact about it; the
process needs a supervisor (systemd, a container restart policy, a process manager) exactly like any
other long-lived service you own.

## Why it exists

A backup that depends on someone remembering to run it is not a backup. The obvious alternative is
one host crontab entry per job, each invoking `sentinel backup run`. That works, and Sentinel does
not stop you doing it; but independent crontab entries cannot coordinate with one another.

A single loop can. It knows how many jobs are in flight, so it can enforce a ceiling across all of
them rather than letting a 3 a.m. pile-up saturate the database host. It knows a job's previous run
has not finished, so it can decline to start a second one on top of it. And it can treat a scheduled
restore-verification job as a first-class citizen alongside the backups it verifies, so the two
share one concurrency budget, one execution history, and one configuration file.

That coordination is the whole value. It is also why the loop has to be a process you keep alive:
the state it coordinates with lives in that process's memory.

## How it works

`sentinel schedule start --config sentinel.yaml` performs a fixed sequence and then blocks:

1. **Load and validate.** The configuration is parsed, defaults are applied, and every cron
   expression is parsed. A malformed expression fails startup rather than failing silently at
   3 a.m.
2. **Open the history database.** The SQLite file at `history_db_path` is opened.
3. **Reconcile.** Executions left in a `running` state by a previous, uncleanly stopped process are
   reconciled, and the count is printed. This is why an abrupt kill does not leave phantom in-flight
   runs in your history forever.
4. **Register jobs.** Every eligible backup job, restore job, and the optional integrity sweep is
   added to the cron loop.
5. **Start and block.** The loop starts and the process waits on `SIGINT` or `SIGTERM`. On either
   signal it stops accepting new fires and waits for in-flight jobs to finish before exiting.

A job is registered only if it is enabled **and** has a non-empty `schedule`. A job with no
`schedule` is not an error; it simply never fires on its own, and remains available to
`sentinel backup run` or `sentinel restore run`.

### Cron expressions

Sentinel accepts **exactly five fields**: minute, hour, day-of-month, month, day-of-week.

```text
┌───────────── minute       (0–59)
│ ┌─────────── hour         (0–23)
│ │ ┌───────── day of month (1–31)
│ │ │ ┌─────── month        (1–12 or JAN–DEC)
│ │ │ │ ┌───── day of week  (0–6 or SUN–SAT)
│ │ │ │ │
0 2 * * *
```

Ranges (`1-5`), lists (`1,15`), steps (`*/30`), and three-letter names (`SUN`, `JAN`) all work.

Two forms that look plausible are rejected at validation time:

| Expression | Result |
|---|---|
| `0 0 2 * * *` | Rejected: `expected exactly 5 fields, found 6`. There is no seconds field. |
| `@daily`, `@hourly`, `@every 1h` | Rejected: `parser does not accept descriptors`. |

Times are interpreted in the **local timezone of the process**. A container running with `TZ`
unset is running in UTC; the same configuration on a laptop in Europe/Paris fires two hours
earlier in wall-clock UTC terms. Pin `TZ` explicitly if it matters.

### What happens when a job fires

Each fire is handed to a bounded worker pool rather than run inline, so a slow job cannot delay the
loop itself.

Before anything starts, the loop checks whether the *same* job is already running. If it is, this
fire is recorded as `skipped` and no second run begins. A backup that takes 40 minutes on a
`*/30 * * * *` schedule therefore produces one run per hour, not an ever-growing overlap.

Backup jobs are additionally wrapped in a retry: **three attempts, with 1 s, 2 s and 4 s backoffs**
between them. Errors that retrying cannot fix; certificate, TLS, authentication and configuration
errors; abort immediately without consuming the remaining attempts. If all attempts fail, a
structured `backup_failed` error record is logged with the attempt count.

Whatever the outcome, the run is written to the execution history. See
[Backup](./backup.md) and [Restore](./restore.md) for what happens inside a run.

### Concurrency

Two independent limits apply, and they behave differently on purpose.

**Backups** are bounded by `max_concurrent_backups` (default `3`). This is a *blocking* limit: a
fire that arrives when the pool is full waits for a slot. Nothing is lost, it just starts later.

**Scheduled restores** are bounded by `scheduler.max_concurrent_restores` (default `1`: serial;
parallelism is opt-in). This is a *non-blocking* limit: a restore that arrives when the limit is
reached is **skipped**, not queued. The skip is recorded in the history with the reason
`concurrency_limit_reached`, so it is visible rather than silent.

The asymmetry reflects what the two operations are for. A backup deferred by ten minutes is still a
useful backup. A restore-verification run deferred past its window is better dropped and recorded
than executed against a database that has since moved on.

:::note A separate key governs `restore run --all`
The **top-level** `max_concurrent_restores` (also default `1`) bounds the manual
`sentinel restore run --all` fan-out, overridable per invocation with `--parallel N`. The scheduler
reads `scheduler.max_concurrent_restores`. They are distinct keys with distinct scopes.
:::

### Enabling, disabling, and history

Whether a job is registered is decided entirely by the `enabled` key in YAML, read at startup:

- **Backup jobs default to enabled.** Omitting `enabled` registers the job.
- **Restore jobs default to disabled.** A restore job must set `enabled: true` explicitly. Restores
  write to a database; the safe default is to do nothing.

There is no runtime pause. Changing what the loop runs means editing the configuration and
restarting the process; the registered job set is fixed at startup.

Execution history is durable and lives in the SQLite database at `history_db_path` (default
`~/.sentinel/history.db`). Query it with `sentinel monitor list`, `sentinel monitor show`, and
`sentinel monitor stats`. Separately, the running process keeps the last ten executions per job in
memory for `sentinel schedule status`; that in-memory ring is discarded when the process exits.

### Inspecting the schedule

`sentinel schedule list` and `sentinel schedule status` do **not** connect to a running scheduler;
there is no socket or control channel to connect to. Each builds a throwaway scheduler from your
configuration purely to compute next-fire times, then exits. They answer "what is configured, and
when would it next run", not "what is happening right now". This is why `LAST STATUS` is always
blank in their output; for actual outcomes, use `sentinel monitor list`.

`sentinel schedule status` registers backup jobs only, so passing a restore job name reports
`job '<name>' not found`. Use `sentinel schedule list` to see restore jobs.

`sentinel schedule stop` always returns an error. There is no daemon to signal; stop the scheduler
with `Ctrl+C` or `SIGTERM` on the `schedule start` process.

## Configuration

| Key | Type | Default | Description |
|---|---|---|---|
| `databases.<name>.schedule` | string | none | Five-field cron expression for a backup job. Omit to exclude it from the loop. |
| `databases.<name>.enabled` | bool | `true` | Registers the backup job when true. |
| `restores.<name>.schedule` | string | none | Five-field cron expression for a restore job. |
| `restores.<name>.enabled` | bool | `false` | Must be `true` explicitly for a restore job to run. |
| `defaults.schedule` | string | none | Inherited by backup jobs that define no `schedule` of their own. |
| `max_concurrent_backups` | int | `3` | Blocking ceiling on simultaneous scheduled backups. |
| `max_concurrent_restores` | int | `1` | Ceiling for `restore run --all`, not for the scheduler. |
| `scheduler.max_concurrent_restores` | int | `1` | Non-blocking ceiling on simultaneous scheduled restores; excess fires are skipped. |
| `scheduler.job_timeout_minutes` | int | `180` | Per-job timeout. |
| `scheduler.lock_dir` | string | `/var/run/sentinel` | Directory for per-job lock files. |
| `scheduler.stale_lock_threshold` | int | `60` | Age in minutes after which a lock held by a dead PID is treated as stale. |
| `history_db_path` | string | `~/.sentinel/history.db` | SQLite execution history. |
| `integrity.scheduled_check.enabled` | bool | `false` | Registers the reserved repository integrity sweep. |
| `integrity.scheduled_check.cron` | string | none | Five-field cron expression for the sweep. Required when enabled. |

Full key documentation is in the [configuration reference](../reference/configuration.md).

## Example

A configuration with two backup jobs and one weekly restore-verification job. Credentials are named
by `*_env` keys and never appear inline:

```yaml
version: "1.0"

max_concurrent_backups: 2

scheduler:
  max_concurrent_restores: 1

history_db_path: ./history.db

databases:
  prod-postgres:
    type: postgres
    host: db.internal
    port: 5432
    username: sentinel
    password_env: PG_PASSWORD
    database: app
    schedule: "0 2 * * *"          # every day at 02:00
    storage:
      type: local
      local_path: ./backups

  reporting-mysql:
    type: mysql
    host: reporting.internal
    port: 3306
    username: sentinel
    password_env: MYSQL_PASSWORD
    database: reporting
    schedule: "*/30 * * * *"       # every 30 minutes
    storage:
      type: local
      local_path: ./backups

restores:
  postgres-restore-test:
    type: postgres
    enabled: true                  # restore jobs are disabled unless stated
    host: staging.internal
    port: 5432
    username: sentinel
    password_env: PG_STAGING_PASSWORD
    database: app_restore_check
    schedule: "0 4 * * 0"          # Sundays at 04:00
    backup_source:
      type: local
      local_path: ./backups
      backup_path: "prod-postgres-*.sql"
      use_latest_match: true
```

Confirm what would run, and when, without starting anything:

```bash
sentinel schedule list --config sentinel.yaml
```

```text
TYPE     NAME                   SCHEDULE      NEXT EXECUTION
backup   prod-postgres          0 2 * * *     2026-08-06T02:00:00Z
backup   reporting-mysql        */30 * * * *  2026-08-05T17:30:00Z
restore  postgres-restore-test  0 4 * * 0     2026-08-09T04:00:00Z
```

`--format json` emits the same rows for scripting. Then start the loop in the foreground:

```bash
sentinel schedule start --config sentinel.yaml
```

```text
Scheduler started with 2 backup job(s) and 1 restore job(s)
```

The process stays in the foreground until you stop it with `Ctrl+C`.

## Failure modes

**Nothing ran overnight.** The most common cause is that the process was not running; it exited,
the container restarted, or the terminal session ended. Confirm with your supervisor, then check
`sentinel monitor list` for whether runs were recorded at all. No records means the loop was not up;
records with a `failure` status mean it was.

**A job is missing from `schedule list`.** It is either disabled or has no `schedule`. Restore jobs
are the usual case here, because they default to disabled and need `enabled: true` written out.

**`invalid cron expression`.** Startup and `sentinel config validate` reject bad expressions with
the offending job name. A six-field expression or an `@daily` descriptor is the usual cause; see
[Cron expressions](#cron-expressions).

**Runs recorded as `skipped`.** Either the previous run of that job was still in flight when the
next fire arrived, or a scheduled restore hit `scheduler.max_concurrent_restores`. The second case
records the reason `concurrency_limit_reached`. Both mean the schedule is tighter than the work
takes; lengthen the interval or raise the ceiling.

**`schedule stop` returns an error.** Expected. There is no daemon; use `Ctrl+C` or `SIGTERM` on the
`schedule start` process.

**`schedule status` reports a restore job as not found.** Expected. That command registers backup
jobs only. Use `sentinel schedule list`.

**`restore enable`, `disable`, `pause`, and `resume` appear to succeed but change nothing.** These
subcommands print a confirmation and return without modifying your configuration or any running
scheduler. Treat the `enabled` key in YAML as the only control, and restart the scheduler after
editing it.

**`scheduler.max_concurrent_backups` seems to have no effect.** The running loop sizes its backup
pool from the **top-level** `max_concurrent_backups`. Set that key; the value under `scheduler:` is
accepted by the parser but is not what bounds the loop.

## Related

- **[Backup](./backup.md)**: what a scheduled backup job actually does when it fires.
- **[Restore](./restore.md)**: what a scheduled restore job does, and why they default to disabled.
- **[How Sentinel fits together](../intro/architecture-overview.md)**: where the scheduler sits
  relative to everything else.
- **[Quickstart](../intro/quickstart.md)**: run a backup manually before scheduling it.
- **[`sentinel schedule` reference](../reference/cli/schedule.md)**: every subcommand and flag.
- **[Configuration reference](../reference/configuration.md)**: every YAML key, including the full
  `scheduler:` block.
- [Run backup from config](../guides/run-backup-from-config.md): the single run a scheduled job repeats.
- [Alerting setup](../guides/alerting-setup.md): hearing about a failed run rather than discovering it later.

{/* sources: internal/domain/schedule/types.go, internal/domain/schedule/next_run.go, internal/scheduler/scheduler.go, internal/scheduler/executor.go, internal/scheduler/backup_job_helpers.go, internal/scheduler/restore_executor.go, internal/scheduler/restore_integration.go, internal/cli/schedule.go, internal/cli/restore.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go */}
