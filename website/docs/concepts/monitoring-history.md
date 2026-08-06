---
title: Monitoring and execution history
description: "The local SQLite record of every backup, restore, and integrity check: what it stores, how its schema is versioned, and how to inspect it."
sidebar_position: 12
---

Sentinel keeps a local SQLite database recording every run it performs. A backup writes a row when it
starts and finalises that row when it ends, a restore writes its own row with the plan it followed, and
an integrity sweep writes one row per artifact it checked. That file is the only durable memory Sentinel
has: `sentinel monitor` reads it, the incremental chain planner reads it, and retention reads it before
deciding what to delete.

## Why it exists

An artifact on disk tells you that a backup happened. It cannot tell you that a backup did not happen.
A job that silently stopped running six weeks ago leaves behind a directory of files that all look fine,
and the absence of last night's file is only visible to someone who knows what last night's file should
have been called.

Failures are worse, because a failed backup produces no artifact at all. Without a record, the failure
is a line in a log that scrolled away. The history database exists so that every outcome, including the
ones that produced nothing, is queryable afterwards. Three other subsystems then depend on it: the
incremental planner decides full versus incremental by reading the job's own past executions, retention
computes deletion candidates from recorded rows rather than from directory listings, and `sentinel repair`
reconciles rows against artifacts to find drift in either direction.

## How it works

The database is a single SQLite file named by `history_db_path`, defaulting to `~/.sentinel/history.db`.
It is opened on demand by any command that records or reads history; there is no daemon holding it. A
`busy_timeout` of 30 seconds is set on open, so concurrent openers serialise at the SQLite layer rather
than failing with "database is locked".

Four tables carry data. `backup_executions` holds one row per backup attempt, including its chain ID and
index, delta size, checksum, and error message. `restore_executions` holds the restore counterpart, with
the planning status, requested PITR timestamp, baseline backup, fallback decision and reason, and chain
depth. `integrity_checks` holds one row per artifact per sweep, grouped by a shared run ID.
`schema_migrations` and `schema_version` track the schema itself.

Nothing in the schema is engine-specific. A PostgreSQL backup and a MongoDB backup produce the same
columns; `database_type` is just a value in a row. The history is engine-independent by design, so a
single query answers "which jobs failed last night" across a mixed fleet.

### The lifecycle of a row

A backup job writes its row in `running` status before the dump begins, so an interrupted run is
distinguishable from a run that never started. When the run finishes, the same row is finalised to
`completed` or `failed`, with the duration, artifact path, size, and any error message. A separate
update then records the security metadata: hash algorithm and value, plaintext hash, manifest path,
whether the artifact was encrypted, and which key environment variable was used.

Rows left in `running` by a killed process are reconciled at the next startup and marked `interrupted`.
That is why the status vocabulary is wider than success and failure: `pending`, `running`, `completed`,
`failed`, `interrupted`, `timeout`, and `skipped` all appear, and legacy values (`success`, `failure`,
`in-progress`) are still accepted by the table constraint so that databases written by older binaries
keep opening.

### Schema versioning

The binary declares the schema version it requires as `BinarySchemaVersion`, currently 5, and ships the
five migrations that produce it embedded in the executable. On every open, Sentinel compares the version
stored in the database with the version the binary requires, and takes one of three paths.

If they match, nothing happens and no lock is taken. If the stored version is lower, Sentinel acquires a
file lock next to the database (`monitor.migrate.lock` in the same directory, with a 30 second acquisition
timeout and a 5 minute staleness threshold), re-reads the version under the lock in case a peer process
completed the migration while it waited, applies each pending migration in its own transaction, and stamps
the new version. If the stored version is *higher* than the binary requires, Sentinel refuses to open the
database at all and returns a forward-incompatible error, without mutating anything.

That last case is the one worth internalising. A database written by a newer Sentinel is not downgraded,
truncated, or "best effort" opened by an older one. Downgrading the binary while keeping the database is
therefore a supported, reversible operation: reinstall the newer binary and the database opens again.

### Inspecting it

`sentinel monitor doctor` is the non-mutating inspector. It reports the database path, one of five
statuses, the current and required schema versions, any pending migrations, and every table with its row
count. It never takes the migration lock unless you pass `--repair`.

| Status | Meaning | Exit code |
|---|---|---|
| `current` | Stored version equals the version this binary requires. | 0 |
| `stale-pending` | Stored version is lower; migrations are pending. | 1 |
| `forward-incompatible` | Stored version is higher; this binary refuses to open it. | 2 |
| `missing` | The file does not exist yet. | 3 |
| `corrupt` | The file exists but is not a readable SQLite database. | 4 |

`--repair` applies pending migrations, and only that. Forward-incompatible, missing, and corrupt databases
are reported unchanged; repair refuses rather than guessing. `--json` emits the same report as a document
on stdout, which is the reliable way to consume it from a script.

## Configuration

One key controls the whole subsystem.

```yaml
version: "1.0"
history_db_path: ~/.sentinel/history.db

databases:
  prod-postgres:
    type: postgres
    host_env: PROD_DB_HOST
    port: 5432
    username_env: PROD_DB_USER
    password_env: PG_PASSWORD
    database: myapp_prod
    schedule: "0 2 * * *"
    storage:
      type: local
      local_path: ./backups
```

`history_db_path` accepts `~` for the home directory and interpolates `${VAR}` environment references, so
a containerised deployment can point it at a mounted volume without templating the file. Omitting it
selects `~/.sentinel/history.db`. The parent directory is created if it does not exist.

Every YAML key, with types and defaults, is in the
[configuration reference](../reference/configuration.md).

## Example

Check the state of the history database, in the form a script can consume:

```bash
sentinel monitor doctor --config sentinel.yaml --json
```

On a healthy installation you should see a report on stdout and an exit code of 0:

```json
{
  "database_path": "/home/sentinel/.sentinel/history.db",
  "status": "current",
  "current_version": 5,
  "required_version": 5,
  "pending_migrations": [],
  "tables": [
    { "name": "backup_executions", "row_count": 14 },
    { "name": "integrity_checks", "row_count": 0 },
    { "name": "restore_executions", "row_count": 2 },
    { "name": "schema_migrations", "row_count": 5 },
    { "name": "schema_version", "row_count": 1 }
  ]
}
```

Dropping `--json` prints the same report as a human-readable block headed `Monitor schema doctor`. Note
that the row-count column in that block is not padded, so a table name and its count run together; the
JSON form is unambiguous.

List what ran recently:

```bash
sentinel monitor list --config sentinel.yaml --last 7d
```

With no rows in range this prints `no matching records`. With rows, it prints a table whose columns are
ID, job, backup type, chain position, status, timestamp, duration, delta size, and a truncated error.

To get history out of the tool and into a file, use the `--output` flag rather than a shell redirect:

```bash
sentinel monitor export --config sentinel.yaml --format json --output history.json
```

## Failure modes

**`monitor list`, `show`, `stats` and `export` print to stderr, not stdout.** This is a defect
([issue #165](https://github.com/denisakp/sentinel/issues/165)), and it means
`sentinel monitor export --format json > history.json` produces an empty file. The same applies to
`sentinel schedule list` and the `sentinel retention` output. Two workarounds are reliable:
`monitor export --output <file>`, which writes the file directly with no stream involved, or
`2>&1` on the redirect. `sentinel monitor doctor` is unaffected; it writes to stdout, including `--json`.

**`monitor stats --last 12h` silently reports on all time.** `stats` converts its window to whole days,
so any duration shorter than 24 hours truncates to zero, and a zero window is interpreted as unbounded.
The output labels itself `Period: all time`, which is the only signal that the flag did not take effect.
`monitor list` handles sub-day durations correctly, so use it when the window matters
([issue #166](https://github.com/denisakp/sentinel/issues/166)).

**`monitor stats` rejects a missing `--job` even though its help text says the flag is optional.** The
help reads `Backup job name (optional; omit for all jobs)`, but omitting it fails with `--job is required`.
The aggregate-across-all-jobs code path exists and is reachable from the library, just not from the flag
([issue #153](https://github.com/denisakp/sentinel/issues/153)).

**`db migrate status` migrates the database as a side effect of reporting on it.** It opens the database
through the same constructor every other command uses, and that constructor applies pending migrations
before returning. A command that reads as an inspection is therefore a mutation. Use
`sentinel monitor doctor` when you need to know the state without changing it
([issue #173](https://github.com/denisakp/sentinel/issues/173)).

**Doctor reports `forward-incompatible`.** The database was written by a newer Sentinel than the one you
are running. Nothing has been damaged. Upgrade the binary, or restore the database file from before the
newer binary touched it.

**Doctor reports `corrupt`.** The file exists but does not answer a trivial SQL query, which usually means
a truncated write or a non-SQLite file at that path. Restore the file from backup, or remove it and let
the next run recreate it. Recreating loses history but not artifacts; the artifacts and their manifests
are independent of this database, and `sentinel repair` can re-derive some state from them.

**A run is recorded as `interrupted`.** The process was killed while the row was still `running`. The
artifact may or may not exist. The row is finalised at the next startup rather than being left ambiguous.

## Related

- [How Sentinel fits together](../intro/architecture-overview.md): where the history database sits among
  the other moving parts.
- [Backup](./backup.md): the run sequence that writes these rows.
- [Restore](./restore.md): what a restore records about its plan.
- [Retention](./retention.md): how recorded rows become deletion candidates.
- [Manifests and integrity](./manifest.md): the per-artifact record that is independent of this database.
- [Inspecting execution history](../guides/inspect-monitor-history.md): querying the history in practice.
- [Diagnosing and repairing the monitor schema](../guides/monitor-schema-migration.md): the doctor workflow.
- [Reading the monitor migration status report](../guides/db-migration-status.md): interpreting
  `db migrate status`.
- [`sentinel monitor` reference](../reference/cli/monitor.md): every flag and subcommand.
- [`sentinel db` reference](../reference/cli/db.md): the migration status command.
- [`sentinel repair` reference](../reference/cli/repair.md): reconciling rows against artifacts.
- [Configuration reference](../reference/configuration.md): every YAML key.

<!-- sources: internal/adapters/monitor/init.go, internal/adapters/monitor/migrate.go, internal/adapters/monitor/version.go, internal/adapters/monitor/schema.go, internal/adapters/monitor/doctor.go, internal/adapters/monitor/recorder.go, internal/adapters/monitor/stats.go, internal/adapters/monitor/export.go, internal/adapters/monitor/migrations/, internal/ports/recorder.go, internal/cli/monitor.go, internal/cli/monitor_doctor.go, internal/cli/db.go, internal/cli/exit_codes.go, internal/config/types.go, internal/config/loader.go -->
