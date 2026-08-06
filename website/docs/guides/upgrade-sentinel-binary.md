---
title: Upgrading the Sentinel binary
description: Replace Sentinel on a host that runs the scheduler, without losing history or leaving a job wedged behind a lock.
sidebar_position: 19
---

Move a host from one Sentinel release to the next: drain the scheduler, swap the binary, confirm the
history database and the configuration still agree with it, and start again.

## When to use this

Use this when you are rolling a new release onto a host that runs `sentinel schedule start`, or onto
any host whose history database is shared with other Sentinel processes. The order below exists
because the first command the new binary runs against the history database migrates it, and that
migration is one-way.

You do not need this to install Sentinel for the first time; see
[Installation](../intro/installation.md). You also do not need it for a host that only ever runs
`sentinel` by hand against a throwaway history database, though the schema check in step 6 is still
worth a moment.

## Before you start

- The new binary downloaded and verified. Both the checksum and, for recent releases, the cosign
  signature and SLSA provenance are covered in [Installation](../intro/installation.md).
- Root or `sudo` on the host, and permission to restart whatever supervises the scheduler.
- The path to the history database. It is `history_db_path` in your configuration, and if the key is
  absent it is `~/.sentinel/history.db` for the user running Sentinel. Resolve it before you start,
  because every step below acts on that file.
- A maintenance window long enough for in-flight backups and restores to finish. Sentinel does not
  interrupt a running dump.

:::info Schema versioning added in v1.3.0
`sentinel monitor doctor` and the gated `schema_version` this guide relies on arrived in v1.3.0.
Upgrading from an older release, you have `sentinel db migrate status` and nothing else.
:::

## Steps

### 1. Record what you are running now

```bash
sentinel version
sentinel monitor doctor --config /etc/sentinel/sentinel.yaml
```

Keep both outputs. `version` is what you roll back to; `doctor` is proof the history database was
healthy before you touched anything, and it reads without writing.

:::note A `go install` build reports `dev`
`sentinel version` prints `dev / unknown / unknown` for a binary built with `go install` or a plain
`go build`, because the release build flags that stamp the version, commit, and build date are not
applied. If that is what you see, you cannot tell from the binary which code you are running. Use a
prebuilt release binary on any host you intend to upgrade in a controlled way.
:::

### 2. Snapshot the history database

```bash
cp ~/.sentinel/history.db ~/.sentinel/history.db.bak
```

Substitute your own `history_db_path`. The file holds execution metadata only; no backup artifact
lives in it. The copy exists so that a rollback has something the older binary can still read.

### 3. Stop the scheduler

Foreground: interrupt the `sentinel schedule start` process with Ctrl-C. Under a supervisor:

```bash
systemctl stop sentinel-scheduler
```

`sentinel schedule stop` is not the command for this. It always fails with
`schedule stop is not supported without a running daemon; use Ctrl+C on 'schedule start'`, because
there is no daemon control channel.

Wait for the process to exit rather than killing it. A graceful stop lets in-flight jobs release
their per-job locks; see [Locking](../concepts/locking.md).

### 4. Install the new binary

```bash
sudo install -m 0755 ./sentinel /usr/local/bin/sentinel
sentinel version
sentinel version --tools
```

`--tools` probes `pg_dump`, `mysqldump`, `mariadb-dump`, and `mongodump` on the current `PATH` and
reports each as `available`, `missing`, or `error`. A release that expects a newer client tool will
show it here rather than at the next scheduled run.

### 5. Validate the configuration against the new binary

```bash
sentinel config validate --config /etc/sentinel/sentinel.yaml
```

A release can add validation rules or tighten existing ones. Catch that now, not at the first tick.

### 6. Check the schema before anything writes to it

:::danger Migration is one-way
The first command that opens the history database migrates it to the new binary's schema version,
and there is no downgrade path. Once migrated, the previous binary **refuses** to open that file at
all. Confirm the snapshot from step 2 exists before you continue.
:::

Inspect first, without mutating:

```bash
sentinel monitor doctor --config /etc/sentinel/sentinel.yaml
```

Exit `0` with `status: current` means the schema already matches and no migration is pending. Exit
`1` with `status: stale-pending` lists the migrations the new binary will apply. Apply them
deliberately, while you are watching, rather than letting the next scheduled job do it:

```bash
sentinel monitor doctor --repair --config /etc/sentinel/sentinel.yaml
```

```text
Monitor schema doctor
  database:          /var/lib/sentinel/history.db
  status:            current (repaired)
  current version:   5
  required version:  5
  applied this run:
    - add_security_columns
    - add_integrity_checks
```

Do not use `sentinel db migrate status` for this check. Despite the name it is not read-only: it
opens the database and therefore migrates it as a side effect. See
[Reading the monitor migration status report](./db-migration-status.md).

### 7. Clear stale locks, then restart

A crash or a hard kill can leave a lock file behind for a job that is no longer running.

:::note Startup does not clear stale locks
The scheduler reconciles stale **execution rows** in the history database at startup, printing
`Reconciled N stale execution(s) from previous shutdown`. It does not sweep the lock directory: the
stale-lock scan exists in the scheduler but is never wired up, so its lock directory is always empty
and the scan never runs. Clear locks explicitly.
:::

Report first, then fix:

```bash
sentinel repair --dry-run --config /etc/sentinel/sentinel.yaml
sentinel repair --fix --config /etc/sentinel/sentinel.yaml
```

`--fix` finalises stale running rows, removes stale locks, and marks broken chains. It never deletes
an artifact; only `--purge-orphans` does that, and you do not need it here. `repair` refuses to run
against a history database whose schema is not current, which is why step 6 comes first.

Then start the scheduler:

```bash
systemctl start sentinel-scheduler
```

## Verify

```bash
sentinel version
sentinel monitor doctor --config /etc/sentinel/sentinel.yaml
sentinel schedule list --config /etc/sentinel/sentinel.yaml
sentinel monitor list --config /etc/sentinel/sentinel.yaml --last 1h
```

You are done when `version` reports the release you installed, `doctor` exits `0` with
`status: current`, `schedule list` shows the jobs you expect with their next run times, and, after
the next tick, `monitor list` shows a fresh `success` record.

## If it goes wrong

**`monitor schema is ahead of this binary`.** The database was migrated by a newer Sentinel than the
one now running it, which is what a rollback produces. `monitor doctor` reports
`status: forward-incompatible` and exits `2`; every other command refuses before writing anything.
Either move forward to the newer binary again, or restore the snapshot from step 2 and start the
older binary against that. Never delete the history database to clear this; you would lose the
execution history that tells you whether your backups have been working.

**The configuration no longer validates.** Read the error and fix the configuration rather than
downgrading. Validation failures are load-time and nothing has run yet, so there is no partial state
to unwind.

**A job will not start, reporting a lock conflict.** Run `sentinel repair --dry-run` to see which
locks are held and whether their owning processes are alive, then `--fix` to remove the ones that
are genuinely stale. Locks held by a live process on another host are skipped rather than stolen.

**Repair reports pending migrations it could not apply.** The human-readable table does not print
the underlying error for a failed repair; add `--json` to `monitor doctor --repair` and read the
`error` field, or read the `monitor schema migration failed` line on stderr, which names the failing
migration file.

## Related

- [Installation](../intro/installation.md): downloading, verifying, and the `go install` caveat.
- [Reading the monitor migration status report](./db-migration-status.md): what
  `db migrate status` reports, and why it writes.
- [Diagnosing and repairing the monitor schema](./monitor-schema-migration.md): the version gate,
  the migration lock, and every doctor exit code.
- [`sentinel version`](../reference/cli/version.md): flags and JSON output shape.
- [`sentinel repair`](../reference/cli/repair.md): every drift class and what `--fix` touches.
- [`sentinel schedule`](../reference/cli/schedule.md): starting and inspecting the scheduler.
- [Locking](../concepts/locking.md): what a per-job lock protects and when one goes stale.
- [Configuration reference](../reference/configuration.md): `history_db_path` and the scheduler keys.

<!-- sources: internal/cli/version.go, internal/version/metadata.go, internal/version/tools.go, internal/cli/schedule.go, internal/cli/monitor_doctor.go, internal/cli/db.go, internal/cli/repair.go, internal/cli/exit_codes.go, internal/adapters/monitor/version.go, internal/adapters/monitor/migrate.go, internal/adapters/monitor/doctor.go, internal/scheduler/scheduler.go, internal/config/loader.go, docs/runbooks/upgrade-sentinel-binary.md -->
