---
title: Recovering after a scheduler crash
description: "Bring sentinel schedule start back after a kill, panic, or reboot: clear the debris first, restart, then find the runs you missed."
sidebar_position: 4
---

The scheduler process is gone and nothing is firing. Clear the crash debris, restart it, and then
work out which runs were lost.

## Symptoms

No process is running, and no new history rows are appearing:

```bash
pgrep -af 'sentinel schedule start'
```

Backup rows are stuck at `running` with no error, from the moment the process died:

```text
ID        JOB      TYPE  CHAIN  STATUS   TIMESTAMP            DURATION  DELTA  ERROR
run-aa11  pg-demo  full  -      running  2026-08-05 09:00:00  0ms       -
```

Trying to stop it cleanly fails, whether it is running or not:

```text
Error: schedule stop is not supported without a running daemon; use Ctrl+C on 'schedule start'
```

Lock files remain in the lock directory from restore jobs that were in flight, and restores skip
after the restart:

```text
Error: restore execution skipped: lock_conflict
```

On restart, Sentinel tells you it found unfinished work:

```text
Reconciled 1 stale execution(s) from previous shutdown
```

## Before you start

**Confirm the process really is down.** Restarting alongside a live scheduler doubles every job:

```bash
pgrep -af 'sentinel schedule start'
```

**Capture the state before you change it.** Both of these become unreadable the moment you restart:

```bash
sentinel monitor list --last 24h --config sentinel.yaml > /tmp/history-before.txt
sentinel repair --dry-run --format json --config sentinel.yaml > /tmp/repair-before.json
ls -la /var/lib/sentinel/locks/
```

**Have the same environment the previous run had.** `schedule start` fully validates the
configuration and resolves every `*_env` variable before it registers a single job. A missing
`password_env` or `encryption_key_env` stops the restart at load time.

:::danger Restarting rewrites every `running` row, including live ones
Before the cron loop starts, `schedule start` marks **every** row still in `running` as
`interrupted`. There is no age check and no lock check: a backup that a separate process is running
right now looks identical to one that crashed, and its row is rewritten while the dump continues.
The artifact is unaffected; the history is what ends up wrong.

Check for live dumps first, and wait for them:

```bash
pgrep -af 'pg_dump|mysqldump|mariadb-dump|mongodump'
```
:::

:::danger Restarting also migrates the history database
`schedule start` opens the history database, and opening it applies any pending schema migrations.
Migration is one-way: after it, an older Sentinel binary refuses to open that file at all. If this
restart is part of a rollback to an older binary, copy the database first:

```bash
cp /var/lib/sentinel/history.db /var/lib/sentinel/history.db.bak
```

See [Reading the monitor migration status report](../guides/db-migration-status.md).
:::

## Resolution

### 1. Clear the crash debris, before restarting

Do this first. There is no automatic cleanup at startup, so anything left behind is still there when
the loop resumes, and a leftover lock turns the first restore of the new process into a
`lock_conflict` skip.

```bash
sentinel repair --dry-run --config sentinel.yaml
```

Read every line, then apply the recoverable fixes:

```bash
sentinel repair --fix --config sentinel.yaml
```

```text
  stale_running  run-aa11  no live lock                                → marked interrupted
  stale_lock     locks/pg-demo-restore.lock  dead pid=999999 age=3h0m0s  → removed

2 change(s) applied.
```

`repair` is the better tool than the restart for this, because it checks for a live lock before
finalising a row, and it refuses to touch another host's lock. The restart's own reconciliation does
neither. If `repair` reports a `stale_lock` it will not remove, or nothing at all while a job keeps
skipping, follow [Recovering from a stale lock](./stale-lock-recovery.md).

:::note There is no startup reap, despite what older runbooks say
The scheduler contains a stale-lock sweep that runs at startup only when its lock directory has been
configured, and `sentinel schedule start` never configures it. The field stays empty and the sweep is
skipped on every start. Recovery is manual today. This is
[issue #142](https://github.com/denisakp/sentinel/issues/142).
:::

### 2. Restart the scheduler

```bash
sentinel schedule start --config sentinel.yaml
```

```text
Reconciled 1 stale execution(s) from previous shutdown
Scheduler started with 1 backup job(s) and 0 restore job(s)
```

`schedule start` runs in the **foreground** and blocks. It is not a daemon and Sentinel ships no
background mode: if this shell exits, the scheduler stops. Run it under systemd, a container restart
policy, or a supervisor. Nothing else restarts it for you.

There is no on-disk concurrency state to clean up. The backup semaphore and the restore limiter are
in-process buffered channels, sized from `max_concurrent_backups` and
`scheduler.max_concurrent_restores`; a crash discards them and a restart rebuilds them.

### 3. Find the runs you lost

Three separate questions, three separate commands.

**Which backups failed or were cut off?**

```bash
sentinel monitor list --last 24h --status failure --config sentinel.yaml
sentinel monitor list --last 24h --config sentinel.yaml
```

Rows finalised by the crash recovery carry a fixed message, and it is the same text whether `repair`
or the restart wrote it:

```text
run-aa11  pg-demo  full  -  interrupted  2026-08-05 09:00:00  0ms  -  execution interrupted by process restart
```

An `interrupted` row means the artifact for that run is untrustworthy: the dump was cut off mid
stream. Re-run the job by hand rather than waiting for the next tick if the gap matters:

```bash
sentinel backup --config sentinel.yaml
```

**Which restores were skipped or lost?**

```bash
sentinel restore history --config sentinel.yaml
```

```text
RESTORE | DATABASE | MODE | PLAN | STATUS | DURATION | TIMESTAMP | REASON | FALLBACK
pg-demo-restore | sentinel | full | - | skipped | 0s | 2026-08-05T21:49:05Z | lock_conflict | -
```

`lock_conflict` means a leftover lock blocked it; go back to step 1. `concurrency_limit_reached`
means the restore limiter was full and that occurrence was dropped rather than delayed; it will not
run later.

A restore that was killed mid-run leaves **no row at all**. Restore executions are written once, at
completion, so the absence of an expected row is the only evidence that one was interrupted. Compare
against the schedule rather than against the history.

**Which windows were simply missed?** Nothing records a run that never started, because the process
was not there to record it. Compare the cron expressions against the gap in the history:

```bash
sentinel schedule list --config sentinel.yaml
```

## Verify recovery

Confirm the jobs you expect are actually registered. Do not trust the startup banner for this:

```bash
sentinel schedule list --config sentinel.yaml
```

```text
TYPE    NAME     SCHEDULE   NEXT EXECUTION
backup  pg-demo  0 2 * * *  2026-08-06T02:00:00Z
```

:::caution The startup count is not the number of registered jobs
`Scheduler started with N backup job(s) and M restore job(s)` counts every entry in `databases:` and
`restores:`, including jobs that are `enabled: false` and jobs with no `schedule:` at all. A
configuration with one unscheduled backup job still prints `1 backup job(s)` while registering
nothing. `schedule list` shows what was really registered; an empty table under a non-zero banner
means nothing will ever fire.
:::

Confirm the lock directory is clean:

```bash
ls -la /var/lib/sentinel/locks/
```

Wait for the next tick and confirm a fresh row appears:

```bash
sentinel monitor list --last 10m --config sentinel.yaml
```

Confirm no drift remains, and that the command exits `0`:

```bash
sentinel repair --dry-run --config sentinel.yaml; echo "exit=$?"
```

## Prevent recurrence

**Supervise the process.** `schedule start` is a foreground command with no daemon mode and no PID
file. Give it a systemd unit or a container restart policy so a crash is followed by a restart you
did not have to notice.

**Stop it with a signal, never a kill.** `SIGINT` or `SIGTERM` on the `schedule start` process stops
the loop, waits for in-flight jobs, and releases their locks:

```bash
kill -TERM "$(pgrep -f 'sentinel schedule start')"
```

```text
scheduler stopping...
scheduler stopped
```

`sentinel schedule stop` never works: it is an unconditional error and does not signal anything.
Configure your supervisor to send `SIGTERM` and to allow a stop timeout longer than your longest
job. This is [issue #138](https://github.com/denisakp/sentinel/issues/138).

**Do not rely on `scheduler.job_timeout_minutes` to bound a hung job.** The key is accepted and
defaults to 180, but nothing reads it: the helper that would apply a per-job deadline has no callers,
so a wedged backup runs until the process dies. Restore jobs are the exception; their own
`timeout_seconds` is applied. Bound backups externally if you need a ceiling.

**Run `sentinel repair --dry-run` after every hard termination**, and on a schedule between them.
Because there is no startup reap, drift accumulates silently until something skips. See
[Repairing repository state drift](./state-repair.md).

**Keep `scheduler.lock_dir` writable and host-local.** The default `/var/run/sentinel` cannot be
created by an unprivileged user, and a restore that cannot take its lock fails before touching the
database.

## Related

- [Recovering from a stale lock](./stale-lock-recovery.md): the debris a crash leaves behind, in
  detail.
- [Repairing repository state drift](./state-repair.md): reconciling history, manifests, and
  artifacts after the restart.
- [Schedule](../concepts/schedule.md): the cron loop, the in-process skip guard, and the concurrency
  limits.
- [Locking and concurrency](../concepts/locking.md): why a restart does not clear locks.
- [`sentinel schedule` reference](../reference/cli/schedule.md): every flag, the cron dialect, and
  the foreground contract.
- [`sentinel monitor` reference](../reference/cli/monitor.md): filtering history after an outage.
- [Inspecting execution history](../guides/inspect-monitor-history.md): reading what the crash left
  in the history database.
- [Reading the monitor migration status report](../guides/db-migration-status.md): what opening the
  history database can change.
- [Monitoring and execution history](../concepts/monitoring-history.md): the statuses a row can
  carry, including `interrupted`.

<!-- sources: internal/cli/schedule.go, internal/scheduler/scheduler.go, internal/scheduler/executor.go, internal/scheduler/restore_executor.go, internal/scheduler/lock_integration.go, internal/adapters/monitor/init.go, internal/adapters/monitor/queries.go, internal/adapters/monitor/recorder.go, internal/adapters/monitor/migrate.go, internal/cli/repair.go, internal/cli/restore.go, internal/domain/restore/executor.go, internal/config/types.go, internal/config/loader.go, docs/runbooks/scheduler-crash-recovery.md, docs/adr/0008-scheduler-concurrency-model.md -->
