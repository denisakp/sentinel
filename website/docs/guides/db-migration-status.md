---
title: Reading the monitor migration status report
description: Run sentinel db migrate status to list the schema migrations applied to the history database, and avoid the ways it can surprise you.
sidebar_position: 20
---

Print the schema version of Sentinel's history database, the version the running binary requires,
and the log of migrations already applied to it.

## When to use this

Use this when you want the applied-migration log: which migrations ran, in what order, and when.
That log is what `db migrate status` gives you and nothing else does.

Do not use it as a health check, and do not put it in a monitoring probe. Despite the name it is not
read-only, it has a single failure exit code that cannot distinguish a forward-incompatible schema
from a missing environment variable, and by the time it prints anything it has already changed the
database it was asked to report on. For any of those purposes use
[`sentinel monitor doctor`](./monitor-schema-migration.md), which reads without writing and has a
distinct exit code per state.

## Before you start

- Sentinel installed at the exact version you intend to run from now on. The report describes the
  relationship between one binary and one database; running it with a different binary than the one
  that will do the work tells you about the wrong pair.
- **The resolved value of `history_db_path`.** Read on: this is the part that bites.
- A copy of that database, if it holds history you care about.

:::danger This command migrates the database it reports on
Opening the history database runs any pending migrations. `db migrate status` opens it, so on a
database behind the current schema version it **migrates the database and then reports the
post-migration state**. The report always says the schema is current, because the command just made
it so. Migration is one-way: once applied, an older Sentinel binary refuses to open that file at all.

Worse, the configuration loader defaults `history_db_path` to `~/.sentinel/history.db` before the
command's own emptiness check runs. Pointing it at a configuration that omits the key does not fail;
it silently migrates the database in your home directory instead. Confirm the key is set, and copy
the file, before you run this. See
[issue #173](https://github.com/denisakp/sentinel/issues/173).
:::

Confirm the path is explicit:

```bash
grep history_db_path /etc/sentinel/sentinel.yaml
```

If that prints nothing, add the key before continuing, or use `sentinel monitor doctor`, which
prints the absolute path it resolved and does not write.

## Steps

### 1. Snapshot the database

```bash
cp /var/lib/sentinel/history.db /var/lib/sentinel/history.db.bak
```

The file carries execution metadata only, so the copy is small and no backup artifact depends on it.
It is the only way back to the previous schema.

### 2. Run the report

```bash
sentinel db migrate status --config /etc/sentinel/sentinel.yaml
```

With `--config` omitted, Sentinel looks for `./sentinel-config.yaml` in the working directory and
errors if it is absent. Only `history_db_path` is consumed from the file, but the loader still
requires a `databases:` block and still resolves every `*_env` variable, so a missing environment
variable stops the command.

On a database that is already current, the report is all you see:

```text
Database Migration Status
=========================
Current Version:          5
Latest Available Version: 5
Status:                   Up-to-date ✓

Applied Migrations:
-------------------
  [001] baseline_schema (applied at 2026-08-05T18:46:48Z)
  [002] add_cleanup_columns (applied at 2026-08-05T18:46:48Z)
  [003] add_consolidated_status_values (applied at 2026-08-05T18:46:48Z)
  [004] add_security_columns (applied at 2026-08-05T18:46:48Z)
  [005] add_integrity_checks (applied at 2026-08-05T18:46:48Z)
```

On a database that was behind, the migration log comes first, on stderr, and is your only evidence
that the command changed anything:

```text
2026/08/05 18:46:48 INFO monitor schema migration starting event=monitor_schema_migration_starting db_path=/var/lib/sentinel/history.db current_version=0 required_version=5
2026/08/05 18:46:48 INFO monitor schema migration complete event=monitor_schema_migration_complete db_path=/var/lib/sentinel/history.db applied_version=5
```

### 3. Read the fields

| Field | Meaning |
|---|---|
| `Current Version` | The highest version recorded in the database's `schema_migrations` table, `0` when none. |
| `Latest Available Version` | The highest version among the migration files embedded in this binary. |
| `Status` | `Up-to-date ✓`, or `Pending migrations` when the two disagree. |
| `Pending Migrations` | Versions with no `schema_migrations` row. Omitted when empty, and normally empty here because the open already applied them. |
| `Applied Migrations` | One line per migration: zero-padded version, name, and an RFC 3339 timestamp. |

The five migrations above are the complete set shipped by v1.4.0. They are embedded in the binary,
so there is no migration directory to deploy alongside it and the set is fixed per release.

:::note The `checksum` column is never populated
`schema_migrations` has a `checksum` column, but nothing writes to it: every row is inserted with
only a version and a name. There is no tamper detection on applied migrations, whatever the column's
presence suggests.
:::

## Verify

```bash
sentinel db migrate status --config /etc/sentinel/sentinel.yaml | grep -E 'Current Version|Status'
```

`Status: Up-to-date ✓` and exit `0` mean the binary and the database agree. A second run prints the
same report with no migration lines on stderr, because nothing is pending and no lock is taken.

For an independent confirmation that does not write, and that names the file it looked at:

```bash
sentinel monitor doctor --config /etc/sentinel/sentinel.yaml
```

## If it goes wrong

**The report describes a database you did not mean to touch.** Compare `db_path` in the migration
log lines against your intended path. If `history_db_path` was unset, the file that was migrated is
`~/.sentinel/history.db`. Set the key, and restore the affected file from a snapshot if it mattered.

**`monitor schema is ahead of this binary`.** Exit `1`, with a what/why/how block naming both
versions:

```text
Error: monitor schema is ahead of this binary

  What: schema is at version 99, this binary requires version 5
  Why:  running an older binary against a newer monitor database can drop
        columns or corrupt rows the newer binary depends on.
  How:  upgrade Sentinel to a build that ships the required schema version,
        or restore a prior monitor database snapshot. No rows were written.
```

Nothing was written. A newer Sentinel migrated this database and an older binary, usually an
unrolled deployment or a stale container image, then reached the same file. Upgrade the binary or
restore the snapshot; see [Upgrading the Sentinel binary](./upgrade-sentinel-binary.md). Do not
delete the database.

**`failed to load config`.** Every failure of this command exits `1`, so the message is the only
thing that distinguishes causes. A missing environment variable reads like this, and is a
configuration problem rather than a schema one:

```text
Error: failed to load config: failed to load config "sentinel.yaml": backup 'demo': environment variable 'DEMO_PG_PASSWORD' is not set
```

**A migration failed part-way.** Each migration file is applied in its own transaction, so an
interrupted run leaves the version at the last fully applied migration rather than somewhere in
between, and a re-run resumes. Read the `monitor schema migration failed` line on stderr; it names
the failing file. If the database is unrecoverable, restore your snapshot. Recreating it from
nothing loses your execution history but no backup artifacts.

## Related

- [Diagnosing and repairing the monitor schema](./monitor-schema-migration.md): the non-mutating
  inspector, the version gate, and the repair path.
- [Upgrading the Sentinel binary](./upgrade-sentinel-binary.md): where this check belongs in an
  upgrade.
- [`sentinel db`](../reference/cli/db.md): the full flag and exit-code reference.
- [`sentinel monitor`](../reference/cli/monitor.md): doctor and the history subcommands.
- [Inspecting execution history](./inspect-monitor-history.md): reading the records this database
  holds.
- [Configuration reference](../reference/configuration.md): `history_db_path` and its default.

{/* sources: internal/cli/db.go, internal/cli/config_resolver.go, internal/cli/forward_incompat.go, internal/adapters/monitor/init.go, internal/adapters/monitor/migrate.go, internal/adapters/monitor/version.go, internal/adapters/monitor/schema.go, internal/adapters/monitor/queries.go, internal/adapters/monitor/migrations/, internal/config/loader.go, docs/runbooks/db-migration-status.md */}
