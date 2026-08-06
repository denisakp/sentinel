---
title: Recovering from a stale lock
description: "A lock file outlived the process that wrote it: how to tell, how to clear it safely, and why recovery is manual today."
sidebar_position: 3
---

A crashed process left its lock file behind, and the job that owns that lock now refuses to start.
Nothing removes it automatically, so this is a manual recovery.

## Symptoms

A restore declines to run and exits non-zero:

```text
Error: restore execution skipped: lock_conflict
```

The skip is recorded, and `sentinel restore history` is where you see it accumulate:

```text
RESTORE | DATABASE | MODE | PLAN | STATUS | DURATION | TIMESTAMP | REASON | FALLBACK
pg-demo-restore | sentinel | full | - | skipped | 0s | 2026-08-05T21:49:05Z | lock_conflict | -
pg-demo-restore | sentinel | full | - | skipped | 0s | 2026-08-05T21:48:52Z | lock_conflict | -
```

Or the logs carry the contention event rather than an error, because the holder was judged live:

```text
{"level":"INFO","msg":"lock contention rejected","event":"lock_contention_rejected","job":"pg-demo-restore","holder_pid":41,"holder_age_seconds":10800}
```

Or every command that opens the history database hangs for thirty seconds and then fails, which is
a different lock in a different directory:

```text
Error: failed to initialize monitor: failed to acquire migration lock at /var/lib/sentinel/monitor.migrate.lock: context deadline exceeded
```

Or `*.lock` files are simply still sitting in the lock directory after a reboot or a `SIGKILL`.

:::note This error is not a stale lock
```text
Error: restore execution failed: failed to acquire restore lock: lock: filesystem error - mkdir "/var/run/sentinel": mkdir /var/run/sentinel: permission denied
```

The default `scheduler.lock_dir` is `/var/run/sentinel`, which an unprivileged user cannot create.
Nothing is stale; the directory never existed. Set `scheduler.lock_dir` to a path the running user
owns and re-run. Tracked as [issue #154](https://github.com/denisakp/sentinel/issues/154).
:::

## Before you start

**Know which jobs can even hold a lock.** Only two things create lock files:

| Holder | Directory | Stale threshold |
|---|---|---|
| Restore jobs, from `sentinel restore run` or the scheduler | `scheduler.lock_dir`, default `/var/run/sentinel` | 1 hour, hard-coded in the acquire path |
| The history database schema migration, as job `monitor.migrate` | The directory containing `history_db_path`, **not** `lock_dir` | 5 minutes, with a 30 second bounded wait |

Backup jobs take no lock at all. The backup factory is wired with no lock manager and the scheduler
helper meant to supply one has no callers, so a `prod-postgres.lock` from a backup job cannot exist.
This is [issue #163](https://github.com/denisakp/sentinel/issues/163).

**Capture the evidence before touching anything.** A lock file is the only record of the process
that died:

```bash
ls -la /var/lib/sentinel/locks/
cp -a /var/lib/sentinel/locks /tmp/sentinel-locks-before
```

**Have `jq` available.** The body is JSON and you will be reading it.

## Resolution

### 1. Read the lock and identify the holder

```bash
cat /var/lib/sentinel/locks/pg-demo-restore.lock | jq .
```

```json
{
  "pid": 999999,
  "job_name": "pg-demo-restore",
  "start_time": "2026-08-05T21:46:20Z",
  "hostname": "sentinel-host-01"
}
```

Four fields, and all four matter. Answer two questions from them.

**Is the recorded host this host?**

```bash
hostname
```

If it differs, stop and read step 5. A PID from another machine means nothing here.

**Is the PID alive?**

```bash
ps -p 999999 -o pid,etime,args     # exits non-zero when the process is gone
date -u                             # compare against start_time
```

A live PID means the lock is doing its job. Let the run finish. Nothing on this page applies.

### 2. Clear it with `sentinel repair`

This is the recommended path, because it applies both stale criteria for you and refuses to touch
another host's lock.

```bash
sentinel repair --dry-run --config sentinel.yaml
```

```text
  stale_lock  locks/pg-demo-restore.lock  dead pid=999999 age=3h0m0s  → report (use --fix to remove)
```

:::danger `--fix` also rewrites history rows
`sentinel repair --fix` does more than remove locks. It also finalises any `running` history row that
has no live lock, and because backup jobs hold no lock, a backup that is genuinely in flight right
now is indistinguishable from a crashed one and will be marked `interrupted`
([issue #163](https://github.com/denisakp/sentinel/issues/163)). The artifact is unaffected; the
history row is not.

Confirm nothing is dumping before you run it:

```bash
pgrep -af 'pg_dump|mysqldump|mariadb-dump|mongodump'
```
:::

```bash
sentinel repair --fix --config sentinel.yaml
```

```text
  stale_lock  locks/pg-demo-restore.lock  dead pid=999999 age=3h0m0s  → removed

1 change(s) applied.
```

A lock is removed only when the recorded PID is dead on this host **and** the lock is older than
`scheduler.stale_lock_threshold`, which defaults to 60 minutes.

### 3. When `repair` reports nothing but the job still skips

This is the case that wastes the most time, so check for it explicitly. A dead-PID lock that is
younger than the stale threshold is **not reported at all**. The summary reads
`0 stale_lock`, the repository looks clean, and the job keeps skipping with `lock_conflict`:

```text
4 finding(s): 1 orphan_artifact · 0 untracked · 1 orphan_manifest · 1 artifact_missing · 0 stale_running · 0 stale_lock · 1 chain_broken
```

You have three options, in order of preference.

**Wait it out.** Once the lock passes one hour, the next `sentinel restore run` of that job removes
it during its own acquire and proceeds, logging:

```text
{"level":"INFO","msg":"stale lock removed","event":"stale_lock_removed","job":"pg-demo-restore","stale_pid":999999,"age_seconds":10800}
```

**Lower the threshold and re-run `repair`.** Note that `scheduler.stale_lock_threshold` changes what
`repair` will remove and nothing else; the restore acquire path uses a hard-coded one hour regardless
of this value.

```yaml
scheduler:
  lock_dir: /var/lib/sentinel/locks
  stale_lock_threshold: 10    # minutes; affects `sentinel repair` only
```

**Remove the file by hand**, if you cannot wait.

:::danger Deleting a live lock corrupts a running restore
Removing a lock that a live process holds lets a second restore start against a database the first
one is still writing into. The surviving database is a mixture of two restores and neither run can
tell. Confirm **both** criteria first, and confirm them in this order:

```bash
cat /var/lib/sentinel/locks/pg-demo-restore.lock | jq -r '.hostname, .pid'
hostname                                   # must match the first line
ps -p <pid> -o pid,etime,args              # must exit non-zero
```

Only when the hostname matches this host and the PID is gone:

```bash
rm /var/lib/sentinel/locks/pg-demo-restore.lock
```

If you cannot confirm the PID is dead, prefer `sentinel repair --fix`, which will refuse rather than
guess.
:::

### 4. Clear a stuck migration lock

The `monitor.migrate.lock` file lives beside `history_db_path`, not in `scheduler.lock_dir`, so
`sentinel repair` never scans it and can never clean it. A migration interrupted part-way leaves it
behind, and every command that opens the history database then waits thirty seconds and fails.

```bash
ls -la /var/lib/sentinel/
cat /var/lib/sentinel/monitor.migrate.lock | jq .
```

Its stale threshold is five minutes, so the simplest fix is to wait five minutes and re-run: the
next migration attempt replaces it. If you cannot wait, apply the same dual check as step 3 before
removing it by hand, then confirm the schema state without writing:

```bash
sentinel monitor doctor --config sentinel.yaml
```

### 5. Locks from another host

`sentinel repair` never removes a lock whose `hostname` differs from this host. It also does not
report it: the finding is skipped silently, so a foreign lock is invisible in the output and you
will only find it by listing the directory.

Removing one is a human decision and requires confirming on the other machine that the job is not
running. There is no safe local check.

:::danger A shared lock directory does not protect you
The stale evaluation used when a restore **acquires** a lock ignores the `hostname` field entirely.
On a lock directory shared between hosts, host B will delete host A's lock and start its own restore
as soon as that lock is older than one hour and the recorded PID number happens not to be in use on
B, even though A is still running. The cross-host protection exists only in the sweep that `repair`
uses, not in the acquire path.

Do not share a lock directory across hosts. Give each host its own `scheduler.lock_dir` on local
storage, and run `sentinel repair` on each host separately.
:::

### 6. Empty or malformed lock files

A zero-byte or unparseable `.lock` file is skipped by every automated path: `sentinel repair` does
not report it and does not remove it, in `--dry-run` or `--fix`. It also does not block anything,
because the next acquire of that job name overwrites it. It is debris.

```bash
find /var/lib/sentinel/locks -name '*.lock' -size -2c
rm /var/lib/sentinel/locks/<name>.lock
```

## Verify recovery

The lock directory should hold only locks whose PIDs are alive:

```bash
ls -la /var/lib/sentinel/locks/
```

Re-run the job that was blocked. It should get past the lock and reach the database:

```bash
sentinel restore run pg-demo-restore --config sentinel.yaml
```

Then confirm the history stopped accumulating skips. The newest row must not read `lock_conflict`:

```bash
sentinel restore history pg-demo-restore --config sentinel.yaml
```

:::note `monitor list --status skipped` cannot show this
`skipped` is only ever written to the restore execution table, and `sentinel monitor list` reads the
backup table exclusively. `sentinel monitor list --status skipped` returns nothing no matter how many
restores were skipped. Use `sentinel restore history`, which is the only command that reads restore
executions.
:::

Confirm `sentinel repair` now agrees the lock directory is clean:

```bash
sentinel repair --dry-run --config sentinel.yaml
```

The summary line should read `0 stale_lock`, and this time the directory listing should agree with
it.

## Prevent recurrence

**Do not expect a startup sweep to save you.** The scheduler contains one, and it is never
configured: `sentinel schedule start` does not set the scheduler's lock directory, so the field stays
empty and the sweep is skipped on every start. Crash debris survives every restart. This is
[issue #142](https://github.com/denisakp/sentinel/issues/142), and it is why older operator runbooks
describing an automatic reap are wrong. Treat `sentinel repair --dry-run` followed by
`sentinel repair --fix` as a required step after any hard termination.

**Set `scheduler.lock_dir` explicitly**, to a directory the running user owns:

```yaml
scheduler:
  lock_dir: /var/lib/sentinel/locks
  stale_lock_threshold: 60
```

```bash
mkdir -p /var/lib/sentinel/locks
```

**Keep the lock directory on local storage.** Some NFS configurations do not implement `flock`, in
which case Sentinel logs `lock_unsupported_filesystem` once and the acquire fails with
`lock: advisory locks not supported on this filesystem`. A shared directory is not a supported way to
coordinate hosts, and per step 5 it actively weakens the guarantee.

**Stop the scheduler with `SIGINT` or `SIGTERM`.** A graceful stop waits for in-flight jobs and their
locks are released normally. See
[Recovering after a scheduler crash](./scheduler-crash-recovery.md).

**Size `stale_lock_threshold` above your longest restore.** It only governs what `repair` removes,
but setting it below a real restore's duration means `repair --fix` can delete a live restore's lock.

## Related

- [Locking and concurrency](../concepts/locking.md): the lock file format, the dual stale criterion,
  and which code paths take a lock.
- [Repairing repository state drift](./state-repair.md): the full `sentinel repair` procedure, of
  which stale locks are one class.
- [Recovering after a scheduler crash](./scheduler-crash-recovery.md): the event that usually leaves
  a lock behind.
- [`sentinel repair` reference](../reference/cli/repair.md): flags, drift classes, and exit codes.
- [`sentinel restore` reference](../reference/cli/restore.md): `restore run` and `restore history`.
- [Restore](../concepts/restore.md): where the lock sits in the restore run sequence.
- [Configuration reference](../reference/configuration.md): the `scheduler:` block, `lock_dir`, and
  `stale_lock_threshold`.
- [Diagnosing and repairing the monitor schema](../guides/monitor-schema-migration.md): the migration
  that takes the `monitor.migrate` lock.
- [Running restore jobs in parallel](../guides/parallel-restore.md): the concurrency limits that sit
  alongside the lock.

{/* sources: internal/adapters/lock/lock.go, internal/adapters/lock/state.go, internal/adapters/lock/flock_unix.go, internal/ports/lock.go, internal/scheduler/lock_integration.go, internal/scheduler/scheduler.go, internal/cli/schedule.go, internal/cli/repair.go, internal/cli/restore.go, internal/cli/backup_factory.go, internal/cli/monitor.go, internal/domain/restore/executor.go, internal/adapters/restore/runtime/executor.go, internal/adapters/monitor/migrate.go, internal/adapters/monitor/queries.go, internal/config/types.go, internal/config/loader.go, docs/runbooks/stale-lock-recovery.md, docs/adr/0007-lock-file-format.md */}
