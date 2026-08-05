---
title: sentinel db
description: Reference for sentinel db migrate status, the monitor schema version gate, and the forward-incompatibility refusal.
sidebar_position: 8
---

Reports the schema version of the monitor history database and the migrations applied to it.

## Synopsis

```text
sentinel db [command]
sentinel db migrate [command]
sentinel db migrate status [flags]
```

`sentinel db` and `sentinel db migrate` are grouping commands. Each prints its help text and exits 0.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `migrate` | Grouping command for schema migration operations. |
| `migrate status` | Print the current schema version, the latest version this binary ships, any pending versions, and the applied-migration log. |

There is no `sentinel db migrate up`, `down`, or `apply`. Migration is not an operator-triggered step: it happens automatically whenever a Sentinel command opens the history database. To apply migrations deliberately, use [`sentinel monitor doctor --repair`](./monitor.md).

## Flags

### `sentinel db` and `sentinel db migrate`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for the command. |

### `sentinel db migrate status`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | `./sentinel-config.yaml` | Path to the YAML configuration file. Only `history_db_path` is consumed. |
| `-h`, `--help` | bool | `false` | Print help for `status`. |

## Behaviour

:::warning `status` is not read-only
Opening the history database runs pending migrations. `db migrate status` opens it, so on a database behind the current schema version the command **migrates it and then reports the post-migration state**. The status it prints is always up to date because it just made it so.

For a genuinely non-mutating inspection, use [`sentinel monitor doctor`](./monitor.md), which reads the schema version without acquiring the migration lock and without writing anything.
:::

### Sequence

1. Resolve the configuration path: `--config`, else `./sentinel-config.yaml`.
2. Load the configuration and read `history_db_path`.
3. Open the history database, which runs the version gate and any pending migrations.
4. Query `schema_migrations` and the embedded migration files, then print the report.

### The version gate

Sentinel pins the schema its binary requires to a constant, `BinarySchemaVersion`, currently `5`. Every open compares that constant against the `schema_version` row on disk:

| On-disk version | Outcome |
|---|---|
| Equal to `5` | No-op. No lock is acquired. |
| Below `5` | A file lock is taken next to the database file, the version is re-read under the lock in case a peer process won the race, pending migrations are applied, and `schema_version` is stamped to `5` in a committed transaction. |
| Above `5` | Refused with `ErrForwardIncompatible`. Nothing is written. |
| Negative | Refused as an unsupported source version. |

The migration lock is named `monitor.migrate`, lives in the same directory as the database as `monitor.migrate.lock`, waits up to 30 seconds, and treats a lock older than 5 minutes as stale. This is what makes concurrent Sentinel processes on the same history database safe.

Each migration file is applied in its own transaction. An interrupted run leaves `schema_version` at the last fully applied value rather than advancing it, so a re-run resumes rather than corrupting.

### Shipped migrations

| Version | Name |
|---|---|
| `001` | `baseline_schema` |
| `002` | `add_cleanup_columns` |
| `003` | `add_consolidated_status_values` |
| `004` | `add_security_columns` |
| `005` | `add_integrity_checks` |

Migration files are embedded in the binary, so the set is fixed per release and there is no directory to deploy alongside it.

### Output fields

| Field | Source |
|---|---|
| `Current Version` | Highest version present in the database's `schema_migrations` table, or `0` when none. |
| `Latest Available Version` | Highest version among the embedded migration files. |
| `Status` | `Up-to-date ✓` when current equals latest and nothing is pending; otherwise `Pending migrations`. |
| `Pending Migrations` | Every version from 1 to the latest available with no `schema_migrations` row. Omitted when empty. |
| `Applied Migrations` | One line per row: zero-padded version, name, and RFC 3339 application timestamp. |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Status printed. |
| `1` | Any failure: configuration could not be loaded, the database could not be opened or migrated, or the schema is forward-incompatible. |

Forward incompatibility is reported through the shared what/why/how formatter and is not given a distinct exit code here. If you need to distinguish it programmatically, use `sentinel monitor doctor`, whose exit codes are contractual per state.

## Errors

Configuration could not be loaded. `db migrate status` is documented internally as skipping full validation, but the load step it uses still enforces the presence of a `databases:` block and still resolves every `*_env` variable, so a missing environment variable stops it:

```text
Error: failed to load config: failed to load config "sentinel.yaml": backup 'demo': environment variable 'DEMO_PG_PASSWORD' is not set
```

The schema on disk is newer than the binary:

```text
Error: monitor schema is ahead of this binary

  What: schema is at version 99, this binary requires version 5
  Why:  running an older binary against a newer monitor database can drop
        columns or corrupt rows the newer binary depends on.
  How:  upgrade Sentinel to a build that ships the required schema version,
        or restore a prior monitor database snapshot. No rows were written.
```

This happens when a newer Sentinel migrated the history database and an older binary, an unrolled deployment or a stale container image, then reached the same file. Upgrade the binary; do not delete the database.

:::note `history_db_path` always has a value
The command checks that `history_db_path` is non-empty, but the configuration loader defaults it to `~/.sentinel/history.db` before that check runs, so the `history_db_path must be set in configuration` error is unreachable. Omitting the key silently migrates the default database in your home directory rather than failing.
:::

## Examples

Report the schema state of the configured history database:

```bash
sentinel db migrate status --config sentinel.yaml
```

On a database that did not exist yet, the migration log precedes the report:

```text
2026/08/05 18:26:26 INFO monitor schema migration starting event=monitor_schema_migration_starting db_path=/var/lib/sentinel/history.db current_version=0 required_version=5
2026/08/05 18:26:26 INFO monitor schema migration complete event=monitor_schema_migration_complete db_path=/var/lib/sentinel/history.db applied_version=5
Database Migration Status
=========================
Current Version:          5
Latest Available Version: 5
Status:                   Up-to-date ✓

Applied Migrations:
-------------------
  [001] baseline_schema (applied at 2026-08-05T18:26:26Z)
  [002] add_cleanup_columns (applied at 2026-08-05T18:26:26Z)
  [003] add_consolidated_status_values (applied at 2026-08-05T18:26:26Z)
  [004] add_security_columns (applied at 2026-08-05T18:26:26Z)
  [005] add_integrity_checks (applied at 2026-08-05T18:26:26Z)
```

A second run prints the same report with no migration lines, because the schema is already current and no lock is taken.

Confirm the schema after a Sentinel upgrade, before the first scheduled run:

```bash
sentinel db migrate status --config /etc/sentinel/sentinel.yaml
```

Exit 0 with `Up-to-date ✓` means the upgraded binary and the history database agree.

## Related

- [`sentinel monitor`](./monitor.md)
- [`sentinel config`](./config.md)
- [`sentinel repair`](./repair.md)
- [Configuration reference](../configuration.md)
- [CLI reference index](./index.md)
- [Architecture overview](../../intro/architecture-overview.md)

<!-- sources: internal/cli/db.go, internal/cli/config_resolver.go, internal/cli/forward_incompat.go, internal/cli/root.go, internal/adapters/monitor/version.go, internal/adapters/monitor/migrate.go, internal/adapters/monitor/doctor.go, internal/adapters/monitor/queries.go, internal/adapters/monitor/migrations/ -->
