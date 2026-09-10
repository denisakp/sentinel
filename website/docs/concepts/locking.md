---
title: Locking and concurrency
description: How Sentinel serialises a named job with a per-job lock file, how stale locks are judged, and why concurrency limits are separate.
sidebar_position: 10
---

A lock is a small file, one per named job, in a directory Sentinel controls. Holding it means "this
job is running in this process right now". Sentinel combines two things in that one file: a
kernel-enforced advisory lock, which is what actually excludes a second process, and a JSON body
recording who took it, so an operator inspecting the directory later can tell a live holder from the
debris of a crash. The mechanism is engine-independent; it keys on the job name and knows nothing
about PostgreSQL or MongoDB. It is also, today, applied unevenly: restore paths take the lock and
backup paths do not, which is covered in detail below.

## Why it exists

Consider one backup job, `prod-postgres`, writing to `./backups/prod-postgres.sql`. A run takes
twenty minutes. Something fires it again after ten: a second cron entry, an operator running the
command by hand, a container restart that replays the schedule. Both processes now dump the same
database to the same path. The second one truncates the file the first is still streaming into. When
the first finishes it writes a manifest recording a SHA-256 for bytes that are no longer on disk, and
records a success row. You are left with one corrupt artifact, one manifest that describes it
incorrectly, and two history rows both claiming everything worked. Nothing in that sequence raises an
error.

Restore is worse, because the collision is not on a file but on a live database. Two restores of the
same job into the same target interleave `DROP` and load statements against each other. The
surviving database is a mixture of two restores, and neither run has any way of knowing.

A lock turns both of those into an observable non-event. The second process discovers the first is
still going and declines to start, loudly enough to appear in the history and in the logs. A run that
did not happen is recoverable. A run that half-happened twice is not.

## How it works

Each job gets one file at `<lock_dir>/<job-name>.lock`, created with mode `0600`; the directory is
created lazily on first acquire with mode `0755`. The body is a single JSON object, written in one
call so it is never observed half-written:

```json
{
  "pid": 12345,
  "job_name": "prod-postgres-restore",
  "start_time": "2026-08-05T03:21:08Z",
  "hostname": "sentinel-host-01"
}
```

Those four fields are the whole record: the process ID that took the lock, the job it belongs to, the
UTC instant it was taken, and the host it was taken on. An optional `metadata` map exists in the
format but is not populated by any current caller.

The file's contents are not what enforces exclusion. Acquiring takes an exclusive non-blocking
advisory lock (`flock` with `LOCK_EX | LOCK_NB`) on the open descriptor first. If the kernel says the
descriptor is already locked, the acquire fails immediately and Sentinel reads the JSON body only to
describe the holder in the error: `lock: held by pid=12345 host="sentinel-host-01" age=6m10s
(live=true)`. This ordering is what makes the check race-free; a stat-then-create scheme would have a
window between the two operations, and the advisory lock does not.

If the kernel grants the lock but the file still holds a body from a previous run, Sentinel evaluates
that body before deciding. A body that is not judged stale is treated as a live holder and the
acquire is refused anyway. A body that is judged stale is overwritten, and a `stale_lock_removed`
event is logged with the dead PID and the lock's age.

Releasing removes the file first and unlocks the descriptor second. That order matters: a process
racing to acquire that sees no file creates a fresh inode with its own independent advisory lock,
rather than attaching to one that is about to be unlinked.

### How a lock becomes stale

A lock file outlives the process that made it whenever that process does not get to run its cleanup:
`SIGKILL`, an OOM kill, a container stopped without a grace period, a host that loses power. The
advisory lock dies with the process, so the kernel no longer objects, but the file and its JSON body
remain on disk.

Sentinel judges such a file removable only when **both** conditions hold:

1. The recorded PID is not alive on this host, probed with `signal(0)`.
2. The lock's age, `now` minus `start_time`, exceeds the configured stale threshold.

Neither condition alone is sufficient. A dead PID with a young lock could be a PID that has not been
reaped yet or, worse, a PID number that has been recycled by an unrelated process. An old lock with a
live PID is just a long-running job. A threshold of zero disables stale evaluation entirely, so
nothing is ever auto-removed. Negative ages, which come from clock skew between the writer and the
reader, clamp to zero rather than reading as impossibly old.

Cross-host lock directories, on shared NFS for example, get one extra rule: if the `hostname` in the
body differs from the current host, the PID check is meaningless, and the sweep refuses to touch the
file at all, logging `foreign_host_lock_detected`. Only a human decides those.

### Which code paths take the lock

This is the part worth reading carefully, because it is not symmetric.

| Path | Takes a lock file? | Lock directory | Stale threshold applied |
|---|---|---|---|
| `sentinel restore run <job>` | Yes | `scheduler.lock_dir` | 1 hour, hard-coded |
| `sentinel restore run --all` | Yes, one per job | `scheduler.lock_dir` | 1 hour, hard-coded |
| Restore jobs under `sentinel schedule start` | Yes | `scheduler.lock_dir` | 1 hour, hard-coded |
| `sentinel backup` | No | Not used | Not applicable |
| Backup jobs under `sentinel schedule start` | No | Not used | Not applicable |
| History database schema migration | Yes, as job `monitor.migrate` | Directory of `history_db_path`, not `lock_dir` | 5 minutes, with a 30s bounded wait |
| `sentinel repair` | No: it reads and removes locks rather than holding one | `scheduler.lock_dir` | `scheduler.stale_lock_threshold` |

Both the backup and the restore executors are built to take a lock; each accepts a lock manager and
serialises on it when one is supplied. The restore construction site supplies one whenever
`scheduler.lock_dir` is non-empty, and it always is, because the loader defaults it. The backup
factory passes nothing, with the stated reasoning that job serialisation belongs to the scheduler
runtime. The scheduler does have a helper for exactly that, wrapping a job function in acquire and
release, but nothing calls it. So a backup job today has no filesystem lock at all. Its only
protection against overlap is an in-process flag on the scheduler's job state, which marks a fire
`skipped` while the previous fire of the same job is still running. That covers the double-cron case
inside one scheduler process; it does not cover a second `sentinel backup` invoked by hand, and it
does not survive a restart.

The practical consequence of the asymmetry surfaces as a permission error rather than as a
correctness problem, and is covered under [Failure modes](#failure-modes).

### Concurrency limits are a separate mechanism

The lock answers "may this specific job start?". Concurrency limits answer "how many jobs may be in
flight at once?". They are independent, they are enforced in different places, and they behave
differently from each other.

The scheduler's backup semaphore is a buffered channel sized from `max_concurrent_backups`. When it
is full, an additional job fire **blocks** in its own goroutine until a slot frees, then runs. It is
delayed, not dropped, and nothing is recorded as skipped.

The scheduler's restore limiter is a channel of the same shape sized from
`scheduler.max_concurrent_restores`, but it is consulted with a non-blocking send. When it is full,
an additional restore fire is **skipped immediately**, with status `skipped` and reason
`concurrency_limit_reached` written to the restore execution history. It does not run later.

That asymmetry is deliberate on the restore side, where a delayed restore into a live database is
rarely what an operator wants, but it means the two halves of the product respond to the same
pressure in opposite ways. Read `concurrency_limit_reached` rows as "the limit is too low or the jobs
are too slow", not as a failure.

Separately, `sentinel restore run --all` bounds its own fan-out with the top-level
`max_concurrent_restores` key, or with `--parallel N` when given. All of these limits are
process-local buffered channels; a crash discards them and there is no on-disk semaphore state to
clean up after one.

## Configuration

Everything that controls locking lives in the top-level `scheduler:` block.

```yaml
version: "1.0"
history_db_path: ~/.sentinel/history.db

max_concurrent_restores: 2   # bounds `restore run --all`

scheduler:
  lock_dir: /var/lib/sentinel/locks
  stale_lock_threshold: 60          # minutes
  max_concurrent_restores: 2
  job_timeout_minutes: 180
```

| Key | Default | What actually reads it |
|---|---|---|
| `scheduler.lock_dir` | per-user, see above | Every backup and restore path, `sentinel repair`, and the startup reconciliation |
| `scheduler.stale_lock_threshold` | `60` minutes | `sentinel repair` and the startup reconciliation |
| `scheduler.job_timeout_minutes` | `180` | The deadline on a scheduled backup, carried into the dump subprocess |
| `scheduler.max_concurrent_restores` | `1` | The restore limiter in `sentinel schedule start` |
| `max_concurrent_restores` | `1` | The `restore run --all` fan-out |
| `max_concurrent_backups` | `3` | The scheduler's backup semaphore |

Three cautions about that table, all verified against the code rather than the key names:

- `scheduler.stale_lock_threshold` does not reach the restore acquire path, which uses a hard-coded
  one hour. It now governs `sentinel repair` and the reconciliation `schedule start` performs, which
  share the same lock-aware decision ([#195](https://github.com/denisakp/sentinel/issues/195)).
- `scheduler.max_concurrent_backups` is accepted, defaulted, and never read. The scheduler sizes its
  backup semaphore from the top-level `max_concurrent_backups` key.
- `scheduler.lock_dir` is not the directory used for the history database's migration lock, which
  always sits next to `history_db_path`.

Every key, with types and defaults, is in the
[configuration reference](../reference/configuration.md).

## Example

The default lock directory is `/var/run/sentinel`, which on a normal Linux or macOS host is not
writable by an unprivileged user. Running a restore as that user, with no `scheduler.lock_dir` set:

```bash
sentinel restore run prod-postgres-restore --config sentinel.yaml
```

```
Error: restore execution failed: failed to acquire restore lock: lock: filesystem error - mkdir "/var/run/sentinel": mkdir /var/run/sentinel: permission denied
```

The restore never reached the database. Point `lock_dir` somewhere the running user owns:

```yaml
scheduler:
  lock_dir: /var/lib/sentinel/locks
```

```bash
mkdir -p /var/lib/sentinel/locks
sentinel restore run prod-postgres-restore --config sentinel.yaml
```

The run proceeds, and while it is in flight the lock is visible:

```bash
ls /var/lib/sentinel/locks/
```

```
prod-postgres-restore.lock
```

After a clean exit the directory is empty again. After a crash the file remains, and
`sentinel repair` is what reports and clears it. Report first, always:

```bash
sentinel repair --dry-run --config sentinel.yaml
```

Stale locks appear as their own finding class, with the dead PID and the age that made them
removable. Applying the fix removes only locks that pass both stale criteria and that belong to this
host:

```bash
sentinel repair --fix --config sentinel.yaml
```

:::note
`sentinel repair --fix` is not the same as `sentinel monitor doctor --repair`, which only touches the
history database schema. Neither one substitutes for the other.
:::

## Failure modes

**`mkdir /var/run/sentinel: permission denied`.** Seen on v1.4.0 and earlier, where `lock_dir`
defaulted to `/var/run/sentinel` for every user. `/var/run` is root-owned, so an unprivileged restore
failed having touched nothing (issue #154). Backups in the same configuration never hit it, because
no backup path took a lock at all (issue #163).

Both are fixed. The default is now resolved per user: `/var/run/sentinel` when running as root,
otherwise `$XDG_RUNTIME_DIR/sentinel`, or `$HOME/.local/state/sentinel/locks`, or a uid-suffixed
directory under the temporary directory. An explicit `scheduler.lock_dir` always wins, and on the
affected versions setting one is the workaround.

One consequence worth knowing: a per-user default means locks serialize **that user's** invocations.
Two different users backing up the same job to the same target will not see each other's locks. Set
`scheduler.lock_dir` to a shared directory if that is your deployment.

**A restore is skipped with reason `lock_conflict`.** Another holder owns the lock for that job. If a
process really is running, this is the mechanism doing its job. If nothing is running, the lock is
stale; see below.

**A stale lock survives a crash and is not cleaned up on restart.** The scheduler contains a startup
sweep that removes stale locks, and the scheduler's lock directory is set through a dedicated method,
but `sentinel schedule start` never calls that method. The field stays empty, the sweep is skipped,
and the crash debris is still there on the next boot. This is tracked as issue #142, and it is why
the older operator runbooks describing an automatic startup reap overstate what happens today. Until
it is fixed, treat `sentinel repair --dry-run` followed by `sentinel repair --fix` as the recovery
step after any hard scheduler termination.

**Locks accumulate and nothing removes them.** A corollary of the same defect. Check for skipped runs
and lingering files together:

```bash
sentinel monitor list --status skipped --last 24h --config sentinel.yaml
ls -la /var/lib/sentinel/locks/
```

**`lock: advisory locks not supported on this filesystem`.** Some NFS configurations do not implement
`flock`. Sentinel logs `lock_unsupported_filesystem` once and the acquire fails. Put the lock
directory on local storage; a shared lock directory is not a supported way to coordinate hosts.

**A lock from another host is never cleaned.** By design. The sweep logs
`foreign_host_lock_detected` and skips the file, because a PID from a different host means nothing
locally. Only remove such a file after confirming by hand that the other host is not running the job.

**Two backup runs of the same job overlap.** Expected today, outside the scheduler's in-process
guard. A manual `sentinel backup` invocation runs regardless of what a scheduler process is doing
with the same job name. Until backups take the lock, do not run them by hand against a job that is
also scheduled.

:::danger Destructive
Never `rm` a lock file without first confirming both stale criteria: the recorded PID is dead on this
host, and the lock is older than your threshold. Deleting a live lock lets a second process start
against a database that is already being restored. Prefer `sentinel repair --fix`, which applies both
checks and refuses foreign-host locks.
:::

## Related

- [How Sentinel fits together](../intro/architecture-overview.md): where the lock adapter sits
  relative to the backup and restore executors.
- [Restore](./restore.md): the run sequence that takes the lock, and what `lock_conflict` means in a
  restore result.
- [Backup](./backup.md): the run sequence that does not.
- [Schedule](./schedule.md): the cron loop, its concurrency limits, and the in-process skip guard.
- [`sentinel repair` reference](../reference/cli/repair.md): the stale-lock finding class and the
  `--dry-run` and `--fix` postures.
- [`sentinel schedule` reference](../reference/cli/schedule.md): every flag on `schedule start`.
- [`sentinel monitor` reference](../reference/cli/monitor.md): filtering history for skipped runs.
- [Configuration reference](../reference/configuration.md): the full `scheduler:` block.
- [Parallel restore](../guides/parallel-restore.md): the concurrency limits that sit alongside the lock.
- [Inspect monitor history](../guides/inspect-monitor-history.md): seeing which runs were skipped and why.

{/* sources: internal/adapters/lock/lock.go, internal/adapters/lock/state.go, internal/adapters/lock/flock_unix.go, internal/adapters/lock/errors.go, internal/ports/lock.go, internal/scheduler/lock_integration.go, internal/scheduler/scheduler.go, internal/scheduler/executor.go, internal/scheduler/restore_executor.go, internal/domain/backup/executor.go, internal/domain/restore/executor.go, internal/adapters/restore/runtime/executor.go, internal/adapters/monitor/migrate.go, internal/cli/schedule.go, internal/cli/restore.go, internal/cli/repair.go, internal/cli/backup_factory.go, internal/config/types.go, internal/config/loader.go, docs/runbooks/stale-lock-recovery.md, docs/runbooks/scheduler-crash-recovery.md, docs/adr/0007-lock-file-format.md, docs/adr/0008-scheduler-concurrency-model.md */}
