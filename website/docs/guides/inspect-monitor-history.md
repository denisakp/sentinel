---
title: Inspecting execution history
description: Read the SQLite execution history with monitor doctor, list, show, stats, and export, and redirect or pipe the output reliably.
sidebar_position: 14
---

Read Sentinel's execution history to answer whether a job ran, when it last succeeded, how long it
took, and which execution ID to hand to `sentinel backup verify`.

## When to use this

Use this for a daily "did last night's backups run" check, for post-incident triage of a job that
started failing, and whenever you need an execution ID, because every integrity check is addressed
by ID rather than by filename.

Do not use it to decide whether an artifact is still intact. The history records that a backup
happened and what its recorded hash was; it never re-reads the bytes. That is
[Verifying one backup](./verify-backup-integrity.md) or
[Sweeping a repository for corruption](./integrity-sweep.md). If the history database itself refuses
to open, you are in [Migrating the monitor schema](./monitor-schema-migration.md) instead.

## Before you start

- `history_db_path` set in your configuration and readable by the user running the command. It is
  created by the first backup or restore run, not by `monitor`.
- At least one recorded execution. Every run that was given `--config` writes a row, including
  failures.
- A configuration file that passes full validation. Every `monitor` subcommand calls the validating
  loader, so an unrelated problem in a `databases:` block, an unset `password_env` variable, or an
  engine option that engine does not accept, will stop a history query that touches no database at
  all.

:::note `list`, `show`, `stats` and `export` write to stdout, since the fix for issue #165
All four send their results to stdout, so redirecting or piping works as expected:
`sentinel monitor export --format json > history.json` writes the export, and
`sentinel monitor list --format json | jq` receives it. Log lines and status messages go to stderr,
so a pipe carries only data.

**On v1.4.0 and earlier all four wrote to stderr**, through Cobra's default print stream. A redirect
left a zero-byte file and a pipe received nothing, with no error either way. On those versions use
`--output` for `export`, and `2>&1` before any pipe for the others. `monitor doctor` was always
correct. The same defect affected `schedule list`, `schedule status` and the `retention` output, all
fixed together.
:::

## Steps

### 1. Confirm the history database is healthy

```bash
sentinel monitor doctor --config sentinel.yaml
```

```
Monitor schema doctor
  database:          /home/sentinel/.sentinel/history.db
  status:            current
  current version:   5
  required version:  5
  tables:
       backup_executions42 rows
        integrity_checks0 rows
      restore_executions3 rows
```

`status: current` and a non-zero `backup_executions` count mean the rest of this guide will return
something. `missing` (exit 3) means no run has ever recorded to this path; check `history_db_path`
before assuming your backups vanished. `stale-pending` (exit 1) and `forward-incompatible` (exit 2)
are schema problems, not data problems, and are handled in
[Migrating the monitor schema](./monitor-schema-migration.md).

The missing space in `backup_executions42 rows` is a real rendering defect in the aligned table, not
a truncated name; read it as 42 rows. `--json` renders the same report with the count in its own
`row_count` field, and is the better choice for scripting.

### 2. List recent executions

```bash
sentinel monitor list --config sentinel.yaml --last 7d 2>&1
```

```
ID            JOB   TYPE  CHAIN  STATUS   TIMESTAMP            DURATION  DELTA  ERROR
019a5c3f-...  demo  full  -      success  2026-08-05 17:48:18  1s        -
019a5b21-...  demo  full  -      failure  2026-08-04 17:48:02  50ms      -      dial tcp: connect...
```

The `ID` column is the execution ID that `monitor show` and `backup verify` take. `--last` defaults
to `7d`, so a job that last ran nine days ago produces `no matching records` until you widen the
window. `--limit` defaults to 50.

Filters narrow the same query:

```bash
sentinel monitor list --config sentinel.yaml --last 30d --status failure 2>&1
sentinel monitor list --config sentinel.yaml --last 30d --job prod-postgres 2>&1
sentinel monitor list --config sentinel.yaml --last 30d --type postgres --storage s3 2>&1
```

`--status` matches the stored value literally, and backups record `success` or `failure`. No other
spelling matches, so `--status completed` silently returns nothing rather than reporting an invalid
value.

`--last` accepts a whole number of days (`7d`), Go durations (`12h`, `90m`, `30s`), and `min` for
minutes (`90min`). Anything else fails with `invalid duration`.

:::caution `--format csv` ignores `--limit` and `--offset`
The CSV path re-runs the query through the export code, which applies its own fixed cap and no
offset. Pagination works for `table` and `json` only. Every filter is honoured in all three formats.
:::

### 3. Inspect one execution

```bash
sentinel monitor show --config sentinel.yaml --id 019a5c3f-... 2>&1
```

```
Execution Details
=================

ID: 019a5c3f-...
Backup Job: demo
Database Type: postgres
Started: 2026-08-05 17:48:18
Duration: 1s

Status: success
Backup Type: full
File: /var/backups/demo.backup
Size: 41982119
```

The ID may also be passed as a bare positional argument, which `--help` does not mention. `File:`
is the value `backup verify` will try to hash. If it reads `unknown`, the job ran without an
`output:` key and no manifest was written; see
[Verifying one backup](./verify-backup-integrity.md).

### 4. Read aggregate statistics for a job

```bash
sentinel monitor stats --config sentinel.yaml --job prod-postgres --last 30d 2>&1
```

```
Backup Job: prod-postgres
Period: last 30 days

Executions: 30
Success Rate: 96.7% (29/30)
Failures: 1

Duration:
  Average: 41s
  Median: 39s
  Min: 33s
  Max: 2m14s
```

:::caution `--job` is mandatory despite the help text
`--help` describes `--job` as `optional; omit for all jobs`, and the all-jobs code path behind it is
implemented, but the handler rejects the run first: `sentinel monitor stats --config sentinel.yaml`
exits 1 with `Error: --job is required`. Always pass `--job`. Tracked as issue #153.
:::

:::caution Sub-day `--last` values silently mean all time
`--last` is converted to a whole number of days before it reaches the query, so `12h` and `90min`
truncate to zero, which disables the lower time bound entirely. `--last 12h` reports
`Period: all time` and counts every execution ever recorded. The `Period:` line is the tell. Use a
day value (`1d`), or an hour value that is a whole multiple of 24 (`720h` reads as 30 days).
`monitor list --last 12h` is unaffected. Tracked as issue #166.
:::

### 5. Export history for a dashboard or an auditor

Either redirect stdout or write to a file with `--output`. Both work; `--output` additionally reports
the path it wrote.

```bash
sentinel monitor export --config sentinel.yaml \
  --last 90d --format csv --output /var/log/sentinel/history-90d.csv
```

```
exported csv history to /var/log/sentinel/history-90d.csv
```

The confirmation line goes to stderr, deliberately: with `--output` the data is already in the file,
so stdout stays empty and a caller piping this command receives nothing but the export. With
`--output` omitted, the export itself goes to stdout and can be redirected. `--last` has no default
here, unlike `list`, so an unfiltered export covers the entire history.

## Verify

Run a job and confirm it appears:

```bash
sentinel backup --config sentinel.yaml
sentinel monitor list --config sentinel.yaml --last 1h 2>&1 | head -3
```

A row for the job should be present within seconds of the run finishing, with `STATUS` matching what
the backup reported. Cross-check the count against `monitor doctor`, whose `backup_executions` row
count should have gone up by one.

For an export, check the file rather than the terminal:

```bash
wc -l /var/log/sentinel/history-90d.csv
```

One header line plus one line per execution. A zero-byte file means `--output` was omitted.

## If it goes wrong

**Nothing appears on stdout, or a redirect produces an empty file.** On v1.4.0 and earlier this was
issue #165: the output went to stderr. Add `2>&1` before the pipe, or use `--output` for `export`. On
a current version, an empty result means there is genuinely no matching history; widen `--last` or
drop the filters.

**`Error: invalid config ...` from a command that only reads SQLite.** `monitor` validates the whole
configuration before opening the history database, so an unrelated job's problem blocks the query.
`sentinel config validate` will name it. Note that `backup verify` loads without full validation, so
it can still run against the same file while `monitor` cannot.

**`no matching records`.** Usually the `--last 7d` default on `list`, or the `--last 30d` default on
`stats`, rather than missing data. Widen the window before concluding anything, and check
`monitor doctor` for the true row count.

**`monitor doctor` reports `missing`.** No execution has ever been recorded at that path. Confirm
`history_db_path` resolves to the file you expect; a relative path resolves against the working
directory of whatever ran the job, which for a systemd unit is rarely your shell's.

**An ID that matches no row** fails with a raw SQL error, `sql: no rows in result set`, and exit 1
rather than a friendly message. Copy the ID from `list` rather than retyping it.

## Related

- [Verifying one backup](./verify-backup-integrity.md): what to do with an execution ID once you have it.
- [Sweeping a repository for corruption](./integrity-sweep.md): checking every recorded backup at once.
- [Migrating the monitor schema](./monitor-schema-migration.md): resolving `stale-pending` and `forward-incompatible`.
- [Checking database migration status](./db-migration-status.md): the equivalent check for the databases themselves.
- [`sentinel monitor` reference](../reference/cli/monitor.md): every subcommand and flag, with defaults.
- [Manifests and integrity](../concepts/manifest.md): what the recorded hash means.
- [Configuration reference](../reference/configuration.md): `history_db_path` and the rest of the schema.

{/* sources: internal/cli/monitor.go, internal/cli/monitor_doctor.go, internal/cli/exit_codes.go, internal/adapters/monitor/queries.go, internal/adapters/monitor/stats.go, internal/adapters/monitor/export.go, internal/adapters/monitor/doctor.go, internal/adapters/monitor/recorder.go, internal/domain/backup/executor.go, internal/config/types.go, internal/config/loader.go, docs/runbooks/inspect-monitor-history.md */}
