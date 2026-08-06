---
title: Diagnosing and repairing the monitor schema
description: Use sentinel monitor doctor to read the history database's schema state without writing to it, and to apply pending migrations on purpose.
sidebar_position: 21
---

Find out what state Sentinel's history database is in, and apply pending schema migrations
deliberately rather than as a side effect of the next backup.

## When to use this

Use this when a command has refused with a schema error, immediately after installing a new Sentinel
release, before running `sentinel repair` (which refuses against a schema that is not current), or
as a scheduled health probe. The exit codes are contractual per state, which makes it the only
schema command you can safely script against.

Use it in preference to `sentinel db migrate status` whenever you want to *look* rather than
*change*. `doctor` without `--repair` acquires no lock and writes nothing;
[`db migrate status`](./db-migration-status.md) migrates the database as a side effect of opening it.

Do not use it for repository state drift: orphan artifacts, missing artifacts, stale locks, and
broken incremental chains are the job of [`sentinel repair`](../reference/cli/repair.md).
`monitor doctor --repair` touches one file, and only its schema.

:::info Added in v1.3.0
`sentinel monitor doctor`, the gated `schema_version` row, and these exit codes arrived in v1.3.0.
Against an older binary, `sentinel db migrate status` is all you have.
:::

## Before you start

- **A configuration that passes full validation.** Unlike `db migrate status`, `monitor doctor` runs
  the complete validator, so a missing `version: "1.0"`, an unresolvable `*_env` variable, or any
  other schema error stops it before it reaches the database. This is worth knowing when you reach
  for doctor during an incident.
- The resolved `history_db_path`. Doctor prints the absolute path it opened, which is the quickest
  way to confirm you are looking at the file you think you are.
- Write access to the database and its directory, but only if you intend to use `--repair`.
- A copy of the database before any `--repair`. Migrations are one-way.

## Steps

### 1. Inspect

```bash
sentinel monitor doctor --config /etc/sentinel/sentinel.yaml
```

```text
Monitor schema doctor
  database:          /var/lib/sentinel/history.db
  status:            current
  current version:   5
  required version:  5
  tables:
       backup_executions0 rows
        integrity_checks0 rows
      restore_executions0 rows
       schema_migrations5 rows
          schema_version1 rows
```

`current version` is the `schema_version` row on disk. `required version` is `BinarySchemaVersion`,
the constant this binary is pinned to, currently `5`.

:::note Table counts render without a separator
The table listing runs each row count into the table name, as above: read
`backup_executions0 rows` as `backup_executions`, `0 rows`. Use `--json` when the numbers matter.
:::

### 2. Read the status and exit code

Five statuses exist, and each maps to a fixed exit code that is stable across releases.

| Status | Exit | Meaning | What to do |
|---|---|---|---|
| `current` | `0` | The database matches this binary. | Nothing. |
| `stale-pending` | `1` | The database is behind; migrations are listed under `pending:`. | Step 4. |
| `forward-incompatible` | `2` | The database is ahead of this binary. | See [If it goes wrong](#if-it-goes-wrong). |
| `missing` | `3` | No file at the configured path. | Nothing, if expected; the next backup or restore run creates it. |
| `corrupt` | `4` | SQLite cannot open or query the file. | See [If it goes wrong](#if-it-goes-wrong). |

`--repair` returns `0` once the schema is current, so `stale-pending` after a repair means the
migrations did not apply.

### 3. Use JSON for automation

```bash
sentinel monitor doctor --json --config /etc/sentinel/sentinel.yaml
```

```json
{
  "database_path": "/var/lib/sentinel/history.db",
  "status": "current",
  "current_version": 5,
  "required_version": 5,
  "pending_migrations": [],
  "tables": [
    { "name": "backup_executions", "row_count": 0 },
    { "name": "integrity_checks", "row_count": 0 },
    { "name": "restore_executions", "row_count": 0 },
    { "name": "schema_migrations", "row_count": 5 },
    { "name": "schema_version", "row_count": 1 }
  ]
}
```

`applied_this_run` appears after a successful `--repair`. `error` and `hint` appear on the failure
statuses. Prefer this shape over parsing the table.

### 4. Apply pending migrations on purpose

:::danger Migration is one-way
There is no downgrade. Once the schema is stamped at a newer version, the previous Sentinel binary
refuses to open the file at all, and your only route back is a copy taken beforehand.
:::

```bash
sentinel monitor doctor --repair --config /etc/sentinel/sentinel.yaml
```

```text
2026/08/05 18:48:14 INFO monitor schema migration starting event=monitor_schema_migration_starting db_path=/var/lib/sentinel/history.db current_version=3 required_version=5
2026/08/05 18:48:14 INFO monitor schema migration complete event=monitor_schema_migration_complete db_path=/var/lib/sentinel/history.db applied_version=5
Monitor schema doctor
  database:          /var/lib/sentinel/history.db
  status:            current (repaired)
  current version:   5
  required version:  5
  applied this run:
    - add_security_columns
    - add_integrity_checks
```

What `--repair` does is exactly what any Sentinel command does when it opens a stale database, run
at a moment you chose. Backup, restore, schedule, retention, repair, monitor, and `db migrate status`
all pass through the same gate on startup:

1. The `schema_version` table is ensured, and the version read.
2. Ahead of the binary: refused with `ErrForwardIncompatible`, before any read or write. A negative
   version is refused as an unsupported source version.
3. Equal: no-op, and no lock is taken.
4. Behind: a file lock named `monitor.migrate` is acquired in the same directory as the database,
   as `monitor.migrate.lock`. Acquisition waits up to 30 seconds, and a lock older than 5 minutes is
   treated as stale and replaced. The version is re-read under the lock, in case a peer process won
   the race, then each pending migration file is applied in its own transaction, and finally
   `schema_version` is stamped in a separate committed transaction of its own.

That last detail matters for interpreting a partial failure. `schema_migrations` rows and the
`schema_version` stamp are **not** advanced together: the per-file rows commit as they go, and the
version is stamped only after every file has succeeded. An interrupted run therefore leaves
`schema_version` at its old value with some newer `schema_migrations` rows already present, and the
re-run resumes from the version rather than corrupting anything.

`--repair` refuses outright on forward-incompatible, missing, and corrupt databases, returning their
exit codes unchanged. It never deletes or recreates a file.

## Verify

```bash
sentinel monitor doctor --config /etc/sentinel/sentinel.yaml
echo $?
```

`status: current` and exit `0`. In a probe, treat only `0` as healthy and branch on `1` through `4`;
`1` is actionable by you, while `2` and `4` need a decision about which binary or which file is the
right one.

To see the applied-migration log with names and timestamps, which doctor does not print, use
[`sentinel db migrate status`](./db-migration-status.md), remembering that it writes.

## If it goes wrong

**`status: forward-incompatible`, exit `2`.** The database was written by a newer Sentinel build
than the one you are running. Nothing was written. Either upgrade the binary to a release that ships
the required schema version, or restore a prior copy of the history database and run against that.
Never delete the file to clear this: it holds the record of whether your backups have been working,
and deleting it destroys that while fixing nothing about the version mismatch. If you must reset,
archive the file first.

**`status: corrupt`, exit `4`.** SQLite cannot open or query the file; the `error` field carries the
underlying message, such as `file is not a database (26)`. Restore the most recent copy. Failing
that, archive the file and remove it, and the next backup or restore run recreates an empty database
with the current schema.

**`status: missing`, exit `3`.** No file at the configured path. On a host that has never run a
backup this is expected. On a host that has, check that `history_db_path` resolves to the same place
it did before; the loader defaults it to `~/.sentinel/history.db` when the key is absent, so a
configuration that lost the key will report `missing` while the real database sits elsewhere.

**Repair ran but the status is still `stale-pending`.** A migration failed. The human-readable table
does not print the `error` field for this status, so re-run with `--json` and read it there, or read
the `monitor schema migration failed` line on stderr, which names the failing migration file.

**`failed to acquire migration lock`.** Another Sentinel process is migrating the same database and
did not finish within 30 seconds, or a lock file is present in the database's directory with no live
owner. Check for other Sentinel processes on the host first; the error message includes the full path
of the lock file.

## Related

- [Reading the monitor migration status report](./db-migration-status.md): the applied-migration
  log, and why that command writes.
- [Upgrading the Sentinel binary](./upgrade-sentinel-binary.md): where this check belongs in a
  release rollout.
- [`sentinel monitor`](../reference/cli/monitor.md): every doctor flag and the other history
  subcommands.
- [`sentinel db`](../reference/cli/db.md): the version gate and the shipped migration list.
- [`sentinel repair`](../reference/cli/repair.md): repository-wide state drift, as distinct from
  schema state.
- [Inspecting execution history](./inspect-monitor-history.md): reading the records this database
  holds.
- [Locking](../concepts/locking.md): the file-lock mechanism the migration path reuses.

{/* sources: internal/adapters/monitor/doctor.go, internal/adapters/monitor/migrate.go, internal/adapters/monitor/version.go, internal/adapters/monitor/init.go, internal/adapters/monitor/schema.go, internal/cli/monitor_doctor.go, internal/cli/monitor.go, internal/cli/exit_codes.go, internal/cli/config_resolver.go, internal/config/loader.go, docs/runbooks/monitor-schema-migration.md */}
