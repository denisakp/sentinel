---
title: sentinel monitor
description: Reference for sentinel monitor and its list, show, stats, export, and doctor subcommands, with every flag.
sidebar_position: 5
---

Queries the SQLite execution-history database that Sentinel writes after every backup and restore, and diagnoses that database's schema.

## Synopsis

```text
sentinel monitor [command]
```

`monitor` has no run behaviour of its own; invoked bare it prints help. Every subcommand reads `history_db_path` from the configuration file, so a valid `--config` is required in all cases. The configuration is fully validated first, meaning a malformed or incomplete `databases:` block makes every `monitor` subcommand fail even though none of them connects to a database.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `list` | Lists backup executions as a table, JSON, or CSV, with filters for job, status, engine, storage backend, and time range. |
| `show` | Prints the full recorded detail of one execution, selected by ID. |
| `stats` | Prints aggregate statistics for one backup job: success rate, duration percentiles, size totals, and trend. |
| `export` | Writes backup history to stdout or to a file as JSON or CSV, for offline or compliance use. |
| `doctor` | Reports the monitor schema version, pending migrations, and per-table row counts; with `--repair`, applies pending migrations. |

## Flags

### `sentinel monitor`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | n/a | Path to the YAML configuration file. Registered as a persistent flag, so every subcommand inherits it. |
| `-h`, `--help` | bool | `false` | Print help for `monitor`. |

### `sentinel monitor list`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--format` | string | `table` | Output format: `table`, `json`, or `csv`. Any other value silently falls back to `table`. |
| `-h`, `--help` | bool | `false` | Print help for `list`. |
| `--job` | string | n/a | Restrict results to one backup job name. Empty means all jobs. |
| `-l`, `--last` | string | `7d` | Time range measured back from now. Accepts `<n>d`, `<n>h`, `<n>m`, `<n>s`, and `<n>min`. An unrecognised suffix fails with `invalid duration`. |
| `--limit` | int | `50` | Maximum rows returned. |
| `--offset` | int | `0` | Rows to skip, for pagination. |
| `--status` | string | n/a | Restrict results to one status, `success` or `failure`. |
| `--storage` | string | n/a | Restrict results to one storage backend, for example `local` or `s3`. |
| `--type` | string | n/a | Restrict results to one database engine, for example `postgres`. |

The table columns are `ID`, `JOB`, `TYPE`, `CHAIN`, `STATUS`, `TIMESTAMP`, `DURATION`, `DELTA`, `ERROR`. `TYPE` shows `full` when no backup type was recorded, `CHAIN` renders as `<chain-id>#<index>` or `-`, and `ERROR` is truncated to a preview.

:::caution `--format csv` ignores `--limit` and `--offset`
The CSV path re-runs the query through the export code, which uses its own fixed cap of 100000 rows and no offset. Pagination flags therefore apply to `table` and `json` output only. Filters (`--job`, `--status`, `--type`, `--storage`, `--last`) are honoured in all three formats.
:::

### `sentinel monitor show`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `show`. |
| `--id` | string | n/a | Execution ID to display. Required unless a positional ID is given. |

`show` also accepts the execution ID as a bare positional argument, which `--help` does not mention; `--id` wins when both are supplied. With neither, the command fails with `execution id is required`. An ID that matches no row fails with `failed to scan full execution: sql: no rows in result set` and exit 1.

The output includes ID, job name, engine, start time, duration, status, and file path and size, plus backup type, chain ID and index, delta size, full-backup size, checksum, and error message when those fields are populated.

### `sentinel monitor stats`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `stats`. |
| `--job` | string | n/a | Backup job name. Help describes this as optional; it is in fact required. See the defect note below. |
| `-l`, `--last` | string | `30d` | Time range measured back from now. Parsed to whole days, so values under 24 hours resolve to zero. See the defect note below. |

:::caution `--job` is documented as optional but is mandatory
`--help` reads `Backup job name (optional; omit for all jobs)`, and the aggregate-statistics code path behind it exists, but the command handler rejects the run before reaching it. `sentinel monitor stats --config sentinel.yaml` exits 1 with `Error: --job is required`. Always pass `--job`. Tracked as issue #153.
:::

:::caution Sub-day `--last` values silently widen to all history
`--last` is converted to an integer number of days, so `12h` truncates to `0` and the query is then run with no lower time bound. `sentinel monitor stats --job app --last 12h` prints `Period: all time` and counts every execution ever recorded, including ones months old. Use whole-day values such as `1d` for a bounded window. `sentinel monitor list --last 12h` is unaffected and honours the hour.
:::

Output covers job name, period, execution count, success rate, failure count, average, median, minimum, and maximum duration, total and average size, a trend label, and a summary of the most recent execution. When the window contains no rows the command prints `no matching records` and exits 0.

### `sentinel monitor export`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-f`, `--format` | string | `json` | Export format: `json` or `csv`. Any other value fails with `unsupported export format`. |
| `-h`, `--help` | bool | `false` | Print help for `export`. |
| `--job` | string | n/a | Restrict the export to one backup job name. |
| `-l`, `--last` | string | n/a | Time range measured back from now. Unset means no lower bound, so the entire history is exported. This differs from `list`, which defaults to `7d`. |
| `-o`, `--output` | string | n/a | File to write. When unset the export goes to the terminal stream described below. |
| `--status` | string | n/a | Restrict the export to one status, `success` or `failure`. |
| `--storage` | string | n/a | Restrict the export to one storage backend. |
| `--type` | string | n/a | Restrict the export to one database engine. |

The CSV export has a fixed header: `id`, `backup_name`, `database_type`, `timestamp`, `duration_ms`, `status`, `error_message`, `storage_backend`, `file_path`, `file_size_bytes`, `checksum`, `backup_type`, `chain_id`, `chain_index`, `delta_size_bytes`, `full_backup_size_bytes`, `created_at`. Writing to `--output` uses mode `0644`.

:::caution `list`, `show`, `stats`, and `export` print to stderr, not stdout
These four subcommands emit their results through Cobra's default print stream, which is stderr. Redirecting stdout captures nothing: `sentinel monitor export --config sentinel.yaml > history.json` produces an empty file, and `sentinel monitor list --format json | jq` receives no input. Use `-o`/`--output` for `export`, or redirect stderr with `2>` for the others. `monitor doctor` and [`sentinel repair`](./repair.md) write to stdout correctly.
:::

### `sentinel monitor doctor`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `doctor`. |
| `--json` | bool | `false` | Emit the report as JSON instead of the aligned text layout. |
| `--repair` | bool | `false` | Apply pending schema migrations. Ignored unless the status is `stale-pending`. |

`doctor` compares the `schema_version` row in the history database against `BinarySchemaVersion`, the version compiled into the binary, which is `5` as of v1.4.0. A database with no `schema_version` table is treated as version 0.

| Status | Meaning | Exit code |
|---|---|---|
| `current` | On-disk version equals the version this binary requires. | 0 |
| `stale-pending` | On-disk version is behind; migrations are listed under `pending`. | 1 |
| `forward-incompatible` | On-disk version is ahead of this binary. Refuses to touch the database. | 2 |
| `missing` | `history_db_path` is unset or the file does not exist. | 3 |
| `corrupt` | The file is not readable as SQLite, or a schema query failed. | 4 |

`--repair` acts only on `stale-pending`; `forward-incompatible`, `missing`, and `corrupt` are returned unchanged. Migrations are embedded in the binary, applied in version order, and each step bumps `schema_version` inside the same transaction. The run is serialised across processes by a file lock named `<history-db-basename>.migrate.lock` in the database's own directory, acquired with a 30 second timeout, so two concurrent Sentinel processes cannot migrate the same database at once. A successful repair reports `current (repaired)` and lists the migrations under `applied this run`.

The same version gate runs on every ordinary open of the history database, not only under `doctor`: `NewMonitor` migrates forward automatically, and refuses a forward-incompatible database with `ErrForwardIncompatible`, which carries both the found and required versions.

## Examples

List the last week of executions for every job:

```bash
sentinel monitor list --config sentinel.yaml
```

Prints the table to stderr. Add `2>&1` before a pipe if you need to page or grep it.

Show only failures from the last 30 days for one job:

```bash
sentinel monitor list --config sentinel.yaml \
  --job prod-postgres --status failure --last 30d
```

Page through a long history 25 rows at a time:

```bash
sentinel monitor list --config sentinel.yaml --last 90d --limit 25 --offset 50
```

Inspect one execution in full, including its chain position and checksum:

```bash
sentinel monitor show --config sentinel.yaml --id 01HQ8Z3K4M5N6P7Q8R9S
```

Read a job's success rate and duration spread over the default 30 day window:

```bash
sentinel monitor stats --config sentinel.yaml --job prod-postgres
```

`--job` is mandatory here despite what `--help` says.

Write a compliance export to a file, which is the only reliable way to capture export output:

```bash
sentinel monitor export --config sentinel.yaml \
  --last 90d --format csv --output /var/log/sentinel/history-90d.csv
```

Check the schema state of the history database before an upgrade:

```bash
sentinel monitor doctor --config sentinel.yaml
```

Exit 0 means the schema matches this binary. Exit 1 means migrations are pending.

Apply pending migrations, and gate a deployment script on the result:

```bash
sentinel monitor doctor --config sentinel.yaml --repair --json
```

Emits the report as JSON with `applied_this_run` listing the migrations that ran.

## Related

- [`sentinel repair`](./repair.md)
- [`sentinel backup`](./backup.md)
- [`sentinel retention`](./retention.md)
- [`sentinel schedule`](./schedule.md)
- [Configuration reference](../configuration.md)
- [Backups: what Sentinel captures and how](../../concepts/backup.md)
- [CLI reference index](./index.md)

<!-- sources: internal/cli/monitor.go, internal/cli/monitor_doctor.go, internal/cli/exit_codes.go, internal/adapters/monitor/doctor.go, internal/adapters/monitor/version.go, internal/adapters/monitor/migrate.go, internal/adapters/monitor/stats.go, internal/adapters/monitor/export.go, internal/config/types.go, docs/runbooks/inspect-monitor-history.md, docs/runbooks/monitor-schema-migration.md -->
