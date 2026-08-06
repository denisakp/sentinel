---
title: Backup
description: What a Sentinel backup job does; the run sequence, full versus incremental selection, and how each engine differs.
sidebar_position: 2
---

A backup job is a named entry in your configuration describing one database, where its artifact
should be written, and under what policy. Running the job produces a dump file, a manifest recording
its SHA-256 fingerprint and its place in an incremental chain, and a row in the local execution
history. Sentinel does not implement its own dump format; it drives your engine's own tool and takes
responsibility for everything around it.

## Why it exists

The hard part of backing up a database is rarely the dump command. It is everything the dump command
does not do: making sure two runs of the same job cannot overwrite each other, keeping credentials
out of `ps` output, proving months later that the file on disk is the file that was written,
encrypting it before it leaves the host, and recording enough lineage that a restore can be planned
rather than guessed.

Without that scaffolding you get a cron entry piping `pg_dump` into a bucket; which works until the
day the pipe truncates, the disk fills mid-write, or two overlapping runs interleave. Nothing in that
arrangement notices. A Sentinel backup job is that same dump with the surrounding guarantees made
explicit and checkable.

## How it works

A backup run is a single process that starts, does one job, and exits. It moves through the same
sequence every time.

**Validate.** The engine type is checked, the dump options are confirmed present, and the
configuration is parsed. Credentials named by `*_env` fields are resolved at this point, so a missing
environment variable fails the run immediately rather than halfway through a dump.

**Lock.** A per-job file lock is taken. If the lock is already held; the previous run of this job is
still going; the run is skipped rather than queued. There is no backlog that drains later; a skipped
run is simply a run that did not happen.

**Ping.** Connectivity to the database is confirmed before any work begins. A failure here is marked
retriable, which is what lets the scheduler retry it instead of treating it as a permanent failure.

**Dump.** Sentinel assembles an argument list and runs the engine's own tool: `pg_dump`, `mysqldump`,
`mariadb-dump`, or `mongodump`. The password is passed through the subprocess environment, never as a
command-line argument. When the job targets remote storage, the dump is redirected to a local staging
directory first, so the artifact can be secured before it leaves the host.

**Pipeline.** This is where the artifact stops being a bare file. In order: optional compression
(gzip or zstd, applied in place), the full-versus-incremental decision and any engine side artifacts
it requires, optional AES-256-GCM encryption, and finally a manifest written alongside the artifact as
`<artifact>.manifest.json`. The manifest records the SHA-256 of the stored bytes, the SHA-256 of the
plaintext, the compression and encryption parameters, and the incremental lineage. For remote storage
the encrypted artifact and its manifest are then uploaded. If an encryption key was configured but
the artifact was not encrypted, the run fails rather than uploading plaintext.

**Record.** The outcome; success or failure, duration, artifact path, size, backup type, chain
position; is written to the SQLite execution history. Failures are recorded as fully as successes;
this is the table `sentinel monitor` and the chain planner read from.

**Notify.** Configured channels are told what happened. A notification failure is reported but does
not fail the backup.

### Full versus incremental

Every job runs full backups unless `incremental_backup.enabled` is set. When it is, the planner
decides the mode for each run by reading the job's own execution history rather than looking at the
database.

It finds the most recent successful execution that carries a chain ID, and produces a **full** backup
starting a new chain; in any of these cases: there is no such execution, the chain has no baseline
to build on, the chain has reached `max_chain_depth` (default 6), or a full run was explicitly forced.
Otherwise it produces an **incremental** backup, extending the existing chain by one index and
recording the baseline artifact it depends on.

The consequence worth internalising is that chain depth is bounded by policy, not by time. A job with
`max_chain_depth: 6` never produces a chain longer than one full plus six incrementals, so a restore
never has to walk an unbounded lineage. Incremental artifacts additionally have their hash verified
immediately after the pipeline runs, because a corrupt link invalidates every backup after it.

## Per-engine behaviour

| Engine | Dump tool | Auto-discovery (`database: "*"`) | Incremental mechanism | Requirements |
|---|---|---|---|---|
| PostgreSQL | `pg_dump` | `pg_dumpall` for the `single` strategy | Chain lineage over WAL summarisation; the chain is reassembled at restore time with `pg_combinebackup` | PostgreSQL 17+ and WAL summarisation enabled |
| MySQL | `mysqldump` | `mysqldump` per database, or one combined dump | Binary logs archived next to the artifact as `<artifact>.binlogs.tar` | `mysql.binlog_path` must point at a readable, locally mounted directory; binary logging on |
| MariaDB | `mariadb-dump` | `mariadb-dump` per database, or one combined dump | Identical to MySQL: binary logs archived as `<artifact>.binlogs.tar` | Same as MySQL |
| MongoDB | `mongodump` | Enumerated through the MongoDB driver | Oplog captured by a second `mongodump` into `<artifact>.oplog.archive` | A replica set: a standalone `mongod` has no oplog. See the caution below about `oplog_window_warn_hours` |

The MySQL and MariaDB paths share one argument builder and differ only in the binary they invoke, so
their behaviour is deliberately identical. PostgreSQL is the outlier: it archives no side artifact at
backup time, because the change data already lives in the write-ahead log and is combined during
restore instead.

:::caution The incremental prerequisite checks do not run
Three configuration keys read like guards against a misconfigured server. None of them checks
anything today.

- `oplog_window_warn_hours` is defaulted and bounds-checked, but the function that would compare it
  against the server's actual oplog window is never called. A short oplog window silently breaks
  incremental coverage, which is the exact failure this key appears to guard against.
- `wal_summary_check` is stored and never probes the server for `summarize_wal`
  ([#155](https://github.com/denisakp/sentinel/issues/155)).
- `binlog_check` parses and is read by nothing.

The MySQL and MongoDB prerequisite validators are also invoked with their "feature enabled" argument
hard-coded to `true`, so `log_bin_off` and `oplog_unavailable_standalone` can never fire. A standalone
`mongod`, or a MySQL server with binary logging off, passes validation and fails later at backup time.

Verify these server-side settings yourself. Sentinel will not.
:::

Configuration validation rejects `incremental_backup.enabled` for MySQL or MariaDB without
`mysql.binlog_path`, and for any engine outside these four.

## Configuration

Backup jobs live under the top-level `databases:` key, keyed by job name. The key becomes the job's
name everywhere else; in `--job` flags, lock files, history rows, and notifications.

```yaml
version: "1.0"
history_db_path: ~/.sentinel/history.db
encryption_key_env: SENTINEL_MASTER_KEY

integrity:
  verify_after_upload: true

databases:
  prod-postgres:
    type: postgres
    host_env: PROD_DB_HOST
    port: 5432
    username_env: PROD_DB_USER
    password_env: PG_PASSWORD
    database: myapp_prod
    schedule: "0 2 * * *"
    output: prod-postgres.sql
    storage:
      type: local
      local_path: ./backups
    database_options:
      pg_out_format: c
    compression:
      enabled: true
      algorithm: zstd
      level: 3
    incremental_backup:
      enabled: true
      max_chain_depth: 6
      wal_summary_check: true
    retention:
      keep_last: 14
      keep_days: 30
    notifications:
      - type: slack
        webhook_url_env: SLACK_WEBHOOK_URL
```

A few keys carry more weight than their size suggests:

- `database: "*"` turns the job into auto-discovery. `exclude:` then removes databases from the
  discovered list, and `strategy:` chooses between `individual` (one artifact per database) and
  `single` (one combined artifact).
- `output:` is optional. Without it the artifact is named `SENTINEL_<timestamp>` with the extension
  appropriate to the engine and output format.
- `verify_after_upload:` may be set per job, overriding the `integrity.verify_after_upload` default.
  When on, the artifact is re-downloaded from its storage backend and re-hashed against the manifest
  immediately after writing.
- `password_env:` names an environment variable. There is no key that holds a password inline. For
  MySQL and MariaDB, `defaults_file:` can point at a `my.cnf` instead; for MongoDB,
  `mongo_secrets_file:` serves the same purpose.

The complete key list, with types and defaults, is in the
[configuration reference](../reference/configuration.md).

## Example

Run every enabled job in the configuration:

```bash
sentinel backup --config sentinel.yaml
```

For the job above you should see the dump complete and the artifact path confirmed:

```
Backup complete !
Backup successfully written to ./backups/prod-postgres.sql
```

On disk you now have two files; `./backups/prod-postgres.sql` and
`./backups/prod-postgres.sql.manifest.json`: plus a new row in the history database. The manifest is
what makes the artifact checkable later:

```bash
sentinel backup verify --all --config sentinel.yaml
```

This re-computes the SHA-256 of every recorded artifact, compares it against the stored manifest
value, and returns a non-zero exit code if any of them disagree.

A single job can also be run without a configuration file, entirely from flags:

```bash
sentinel backup --type postgres --host db --port 5432 --user backup \
  --database app --password-env PG_PASSWORD --local-path ./backups
```

This flag-driven form performs the dump and writes the artifact, but it has no job name, so it takes
no lock and records no history. Use it for one-off dumps, not for scheduled operation.

## Failure modes

**A run reports that the job is already running.** The file lock for this job is held. If no such
process exists, the lock is stale; this happens when a previous run was killed rather than allowed
to exit. Locks are scanned for staleness at startup, and `scheduler.stale_lock_threshold` controls
how old a lock with a dead PID must be before it is treated as abandoned.

**The connectivity check fails.** The database was unreachable before any dump began. This is
recorded as a failure and marked retriable, so a scheduled job retries with 1s, 2s, and 4s backoffs
rather than giving up. A run that fails all three attempts almost always means credentials or network
reachability, not a Sentinel problem.

**An incremental backup fails with `binlog_path_missing` or a similar code.** MySQL and MariaDB
incrementals need to read the binary logs from a locally mounted directory. If Sentinel runs in a
container and the logs live in the database container, the path exists for the database but not for
Sentinel. The prerequisite codes (`log_bin_off`, `binlog_path_not_found`,
`binlog_path_unreadable`, `oplog_unavailable_standalone`, `wal_summary_disabled`) name the specific
precondition that was not met.

**The backup fails after the upload, mentioning `verify_after_upload_failed`.** The artifact was
written, re-downloaded, and did not match its manifest hash. The stored object is deliberately left
in place rather than deleted, so it can be examined. Treat this as a storage-side integrity problem.

**A remote encrypted backup is refused for the `single` auto-discovery strategy.** That path uploads
directly and cannot be staged locally, so the artifact could not be encrypted before leaving the
host. Sentinel refuses rather than uploading plaintext. Use `strategy: individual`, or local storage.

**The manifest could not be written.** This surfaces as a warning, not a failure; the artifact
exists and is usable. It does mean the artifact cannot be verified later, so it is worth chasing.

## Related

- [How Sentinel fits together](../intro/architecture-overview.md); where the backup path sits among
  the other moving parts.
- [Restore](./restore.md): how an artifact and its chain become a database again.
- [Schedule](./schedule.md): running backup jobs on a cron loop.
- [Your first PostgreSQL backup](../tutorials/postgres/first-backup.md); the sequence above, run
  end to end against a throwaway instance.
- [Incremental backup with WAL](../tutorials/postgres/incremental-wal.md); chains in practice.
- [`sentinel backup` reference](../reference/cli/backup.md): every flag and subcommand.
- [Configuration reference](../reference/configuration.md): every YAML key.
- [Run backup from config](../guides/run-backup-from-config.md): taking a real backup from a configuration file.
- [Backup compression](../guides/backup-compression.md): reducing artifact size and transfer cost.

{/* sources: internal/domain/backup/executor.go, internal/domain/backup/planner.go, internal/domain/backup/pipeline.go, internal/domain/backup/source.go, internal/domain/backup/incremental/, internal/adapters/dump/, internal/config/types.go, internal/config/validator.go, internal/cli/backup.go, internal/cli/backup_factory.go */}
