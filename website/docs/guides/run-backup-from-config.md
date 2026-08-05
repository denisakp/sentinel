---
title: Running a backup from a configuration file
description: "Run every job in a YAML configuration once, on demand: validation, execution order, and how naming differs from a scheduled run."
sidebar_position: 7
---

Run the backup jobs defined in a configuration file once, immediately, without involving the
scheduler.

## When to use this

Use this for an ad-hoc dump before a migration, for smoke-testing a configuration you have just
written, or for seeding a newly configured storage backend with a first artifact.

Do not use it for recurring backups. `sentinel backup --config` ignores every `schedule:` field in
the file; only the scheduler reads them, and only scheduled runs get timestamped filenames and an
automatic retention sweep. See [Schedule](../concepts/schedule.md) for the recurring path.

Do not reach for it to run a single job either. `sentinel backup` has no `--job` flag, and the
command backs the whole file or nothing. Step 4 covers what to do instead.

## Before you start

- The `sentinel` binary on `PATH`. Confirm with `sentinel version`.
- The client tool for each engine you have configured, installed on this host and on `PATH`:
  `pg_dump` for PostgreSQL, `mysqldump` for MySQL, `mariadb-dump` for MariaDB, `mongodump` for
  MongoDB. Sentinel builds an argument vector and executes these; it does not implement the dump
  protocols itself.
- Every environment variable named by a `*_env` key exported into this shell. Configuration loading
  resolves them eagerly, so one missing variable stops the run before any job starts. See
  [Supplying database credentials](./database-credentials.md).
- A writable path at `history_db_path`, if you want the runs recorded. Without a usable history
  database there is nothing for `sentinel monitor`, `backup verify`, or retention to read later.

## Steps

### 1. Export the credentials

Passwords reach Sentinel through the environment, a secrets file, or an engine option file, never on
the command line where they would land in shell history and in the process table.

```bash
export SENTINEL_PG_PASSWORD="$(vault kv get -field=password secret/sentinel/postgres)"
export SENTINEL_MYSQL_PASSWORD="$(vault kv get -field=password secret/sentinel/mysql)"
```

### 2. Validate the configuration

```bash
sentinel config validate --config sentinel.yaml
```

```
configuration is valid
```

This parses the file, applies defaults and inheritance, and resolves every `*_env` reference, so it
catches an unexported variable before you discover it mid-dump. It is structural only: it opens no
database connection and contacts no storage backend.

Expect a warning line per job that has no `tls:` block. It is informational and does not fail
validation.

### 3. Run every job

```bash
sentinel backup --config sentinel.yaml
```

Sentinel dispatches per job by `type`:

| `type` | Tool invoked |
|---|---|
| `postgres` | `pg_dump`, or `pg_dumpall` when auto-discovery writes one combined dump |
| `mysql` | `mysqldump` |
| `mariadb` | `mariadb-dump` |
| `mongodb` | `mongodump` |

Four behaviours of this mode are worth knowing before you run it against anything you care about.

Jobs run **one at a time**, and the run **stops at the first failure**: the remaining jobs are never
attempted and the command exits non-zero. A three-job file where the second job fails leaves you with
one artifact, not two.

The **order is not the order in the file**. Jobs are iterated from a map, so which job runs first
varies between invocations. Combined with the previous point, a failing job aborts a different subset
of its neighbours each time.

Jobs with `enabled: false` are skipped silently.

The artifact filename is exactly your `output:` value. There is no timestamp, so **a second run
overwrites the first**. Timestamped names such as `app-postgres_2026-06-11T02-00-00.sql` are produced
only by the scheduler. If you need to keep two ad-hoc dumps, change `output:` between runs or move
the first one aside.

A configuration to run against looks like this:

```yaml
version: "1.0"
log_format: json
history_db_path: ./.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: ./backups

databases:
  app-postgres:
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: sentinel
    password_env: SENTINEL_PG_PASSWORD
    database: app_production
    output: app-postgres.sql

  app-mysql:
    type: mysql
    host: 127.0.0.1
    port: 3306
    username: sentinel
    password_env: SENTINEL_MYSQL_PASSWORD
    database: app_production
    output: app-mysql.sql
```

An `output:` with no extension gains `.sql` for the SQL engines. A PostgreSQL job that sets
`pg_out_format: c` under `database_options:` gains `.backup` instead, and `t` gains `.tar`.

MongoDB is configured by URI rather than by host and port:

```yaml
  app-mongo:
    type: mongodb
    uri_env: SENTINEL_MONGO_URI
    database: app_production
    output: app-mongo
```

Prefer `uri_env` over an inline `uri:`, since a MongoDB URI usually embeds the password.

### 4. Scope the run when you do not want every job

There is no `--job` flag on `sentinel backup`. Passing one fails with `unknown flag: --job`. Three
things work instead:

- Set `enabled: false` on the jobs you want to sit out, and re-run.
- Keep a second configuration file containing only the job in question, which is also the tidiest way
  to run a one-off dump with different storage.
- Use the flag form, `sentinel backup --type postgres --host ... --database ...`, which ignores the
  configuration file entirely. Note that this path has no access to `defaults:`, retention,
  notifications, or per-job TLS.

:::caution `backup force-full --job` does not work today
`sentinel backup force-full` and `sentinel backup chain-status` accept `--job` but register no
`--config` flag, and their handlers require one. Both fail with `Error: --config is required`
whatever you pass, so neither is currently usable as a single-job entry point.
:::

## Verify

Confirm the artifacts exist and are the size you expect:

```bash
ls -lh ./backups/
```

Then confirm Sentinel agrees, which is the check that matters. A file on disk proves the process
wrote something; the history row proves the run was accepted end to end:

```bash
sentinel monitor list --config sentinel.yaml --last 1h
```

Every job you expected should appear with `STATUS=success`. A job that is absent never ran, which is
the signature of an earlier job having aborted the sequence.

For proof that the bytes are intact rather than merely present, recompute the hash against the
manifest:

```bash
sentinel backup verify <backup-id> --config sentinel.yaml
```

## If it goes wrong

**`environment variable '<NAME>' is not set`.** Loading resolves every `*_env` key before any work
starts, so this is a load failure rather than a job failure. Nothing ran.

**`exec: "pg_dump": executable file not found in $PATH`, or the equivalent for another engine.** The
client tool is missing on this host. Install the client package for the engine, and check that its
major version is at least that of the server you are dumping.

**Some jobs produced artifacts and the rest did not.** The run aborted at the first failure. Read the
error, fix that job, and re-run; jobs that already succeeded will simply be overwritten with a fresh
dump.

**A second run silently replaced the first artifact.** Expected in this mode: the filename is your
`output:` value with no timestamp. Only the scheduler makes names unique.

**Old backups were not cleaned up.** The automatic retention sweep runs only after a *scheduled*
backup. `sentinel backup --config` never triggers it. Apply the policy by hand with
[Applying a retention policy](./apply-retention.md).

## Related

- [Backup](../concepts/backup.md): the stages of a run and what each produces.
- [Supplying database credentials](./database-credentials.md): the supported ways to get a password
  to Sentinel.
- [Environment setup](./environment-setup.md): making variables visible to the process that runs
  Sentinel.
- [Schedule](../concepts/schedule.md): the recurring path, timestamped filenames, and the automatic
  retention sweep.
- [Verifying backup integrity](./verify-backup-integrity.md): what `backup verify` checks.
- [Your first PostgreSQL backup](../tutorials/postgres/first-backup.md): the same commands against a
  throwaway container.
- [`sentinel backup` reference](../reference/cli/backup.md): every flag and subcommand.
- [Configuration reference](../reference/configuration.md): every YAML key.

<!-- sources: internal/cli/backup.go, internal/cli/backup_factory.go, internal/cli/config.go, internal/config/loader.go, internal/config/env.go, internal/config/types.go, internal/utils/default.go, internal/utils/scheduled_output.go, internal/adapters/dump/pg/output.go, internal/domain/backup/executor.go, docs/runbooks/run-backup-from-config.md -->
