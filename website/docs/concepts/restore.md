---
title: Restore
description: How Sentinel resolves which artifact to restore, stages and verifies it, and hands it to your database's own restore tool.
sidebar_position: 3
---

A restore job turns a stored artifact back into a live database. Sentinel resolves which artifact to
use, downloads it into a staging directory, verifies its SHA-256 against the manifest written at
backup time, decrypts and decompresses it if needed, and only then runs your engine's own restore
tool. Restore jobs are configured in the same file as backup jobs, and they default to disabled.

## Why it exists

An artifact you have never restored is a hypothesis, not a backup. The failure modes that matter;
a truncated upload, a lost encryption key, a dump format your restore tool cannot read, a chain whose
baseline was deleted by a retention sweep; are all invisible until someone attempts a restore, which
is usually the worst possible moment to discover them.

Making restore a first-class configured job rather than an ad-hoc command has two consequences. The
same job can be run on a schedule against a throwaway target database, so recovery is exercised
continuously instead of theoretically. And the recovery path itself is written down in configuration
and reviewed like any other code, rather than reconstructed from memory during an incident.

The safety defaults follow from that. A restore job is disabled unless you write `enabled: true`, and
`conflict_strategy` defaults to `error`, so a job that is misconfigured refuses to run rather than
overwriting something.

## How it works

A restore run is a fixed sequence. Each step either succeeds or aborts the run; there is no partial
resumption.

**Lock and timeout.** A file lock is taken for the job name. If another run of the same job holds it,
this run is recorded as skipped with reason `lock_conflict` rather than queued. If
`timeout_seconds` is set, the whole run is bounded by it.

**Resolve the artifact.** The `backup_source` block says where to look. `type` is one of `local`,
`s3`, or `gcs`: these three, and no others, are supported as restore sources. For `local`,
`local_path` is the directory that holds artifacts and `backup_path` is the object inside it; for the
cloud types, `backup_path` is the object key in the configured bucket.

By default `backup_path` names one exact object. With `use_latest_match: true` it becomes a glob:
Sentinel lists the source, matches every object against the pattern, sorts the matches by last
modified time, and takes the newest. This is what lets a job say `SENTINEL_*.sql` and always pick up
last night's backup without knowing its timestamp.

**Stage.** The chosen object is downloaded into `staging_dir`, which is created mode `0700`; the
staged file is `0600`. Sentinel first checks that the filesystem has room for the artifact's reported
size and aborts with `insufficient_staging_space` if not. It then attempts to download the manifest
sidecar; the same object path with `.manifest.json` appended. A missing sidecar is not an error; it
means the backup predates manifests or was written without one, and the run continues without
integrity verification.

**Plan.** The requested `restore_mode` is checked against what the manifest actually supports. The
planner returns one of three statuses: ready, rejected, or confirmation-required, each with a reason
code that is recorded in the execution history and printed on failure. Planning is pure; it decides
before anything touches the target database. See [Restore modes](#restore-modes) below.

**Preflight: verify, decrypt, decompress.** If a manifest was staged, the artifact's SHA-256 is
computed and compared against the manifest's recorded hash. A mismatch aborts the run with
`integrity_check_failed`. If the manifest records encryption, the per-backup key is derived from the
master key named by `encryption_key_env` or `encryption_key_file` and the stream is decrypted with
AES-256-GCM. If the manifest records compression, the stream is decompressed. The result is written
beside the staged file with a `.plaintext` suffix, and that path is what the engine receives.

:::danger Destructive
Everything up to this point is read-only. The next step writes to your target database and, depending
on `conflict_strategy`, may drop objects in it. Confirm the artifact is the one you intend with
`sentinel restore dry-run <job-name>`, and point the job at a database you are willing to lose.
:::

**Restore.** Sentinel builds an argument list and executes your engine's own tool; it does not parse
or rewrite dump content. Credentials are passed through the environment, never as arguments. See
[Per-engine behaviour](#per-engine-behaviour).

**Replay, verify, record.** An incremental PostgreSQL restore has already combined its chain before
this point; the executor also carries binlog and oplog replay steps for the other engines, though see
the note below on their reachability. Verification runs if
`verify_after_restore: true`, and is mandatory, not optional, for `pitr` and `incremental` modes.
Finally the outcome is written to the execution history, whether it succeeded or failed. Staged files
are deleted unless `keep_file: true`.

### Restore modes

`restore_mode` selects how much more than the base artifact is applied.

| Mode | What it does | Engines |
|---|---|---|
| `full` (default) | Restores the single resolved artifact and nothing else. | All four |
| `pitr` | Recovers to `pitr_timestamp`, which must fall inside the manifest's recoverable window. | PostgreSQL only |
| `incremental` | Resolves the chain from `incremental_from_backup` to the target, stages every link, and combines them. | PostgreSQL only |

Requesting `pitr` or `incremental` on MySQL, MariaDB, or MongoDB is rejected at planning time with
reason code `unsupported_database_type`. The executor does implement binlog replay for MySQL and
MariaDB and oplog replay for MongoDB, and the backup side archives those artifacts, but because
planning rejects the mode first, a configured restore job on those engines reaches only `full` mode
today. Treat `pitr` and `incremental` as PostgreSQL capabilities when planning recovery.

Other reason codes you are likely to meet: `missing_pitr_timestamp`,
`pitr_outside_recoverable_window`, `missing_advanced_metadata` (the manifest does not declare the
capability), `missing_incremental_baseline`, and `incompatible_incremental_baseline` (the
`incremental_from_backup` you named is not the baseline this artifact descends from).

One status is not a failure. When an incremental chain cannot be executed, the planner offers a
fallback to a full restore from the baseline and returns `confirmation_required` with reason
`full_fallback_confirmation_required`. The run stops. Setting `confirm_full_fallback: true` on the
job authorises that substitution in advance; which is a decision about acceptable data loss, since a
full restore from the baseline discards everything the chain would have applied.

### Conflict strategy

`conflict_strategy` decides what happens when the target database already contains data. It defaults
to `error`, and the three values map onto native flags of each engine's restore tool rather than onto
anything Sentinel invents.

For PostgreSQL, `conflict_strategy: replace` additionally requires `allow_cascade: true`. The
configuration is rejected at load time otherwise, because `replace` becomes `pg_restore --clean`,
which can drop dependent objects that were never part of the backup.

## Per-engine behaviour

Sentinel orchestrates external tools. Each must be on `PATH` on the machine running the restore. The
incremental replay column describes the mechanism each engine uses; as noted under
[Restore modes](#restore-modes), only the PostgreSQL path is reachable from a configured restore job
today.

| Engine | Restore tool | Connectivity check | `conflict_strategy` mapping | Incremental replay |
|---|---|---|---|---|
| PostgreSQL | `pg_restore` for custom, tar, and directory archives; `psql` for plain-SQL dumps, detected from the file's magic bytes | `psql` | `replace` → `--clean` (plus `--if-exists` when `allow_cascade: true`); `ignore` → `--if-exists`; `error` → no flag | `pg_combinebackup` combines the staged chain before restore |
| MySQL | `mysql` | `mysql` | `replace` and `ignore` → `--force`; `error` → no flag | `mysqlbinlog` piped into `mysql` |
| MariaDB | `mariadb` | `mariadb` | `replace` and `ignore` → `--force`; `error` → no flag | `mysqlbinlog` piped into `mariadb` |
| MongoDB | `mongorestore` | `mongosh` | `replace` → `--drop`; `ignore` → `--stopOnError=false`; `error` → no flag | `mongorestore` applies the oplog archive |

Two asymmetries are worth noting. MySQL and MariaDB do not distinguish `replace` from `ignore`: both
become `--force`, which tells the client to continue past errors. And PostgreSQL is the only engine
where Sentinel inspects the artifact to choose a tool, because `pg_dump`'s default plain-text output
is not readable by `pg_restore` at all.

## Configuration

Restore jobs live under the top-level `restores:` map, keyed by job name. Shared runtime settings
live under `restore:`. The full key list is in the
[configuration reference](../reference/configuration.md).

| Key | Notes |
|---|---|
| `enabled` | Defaults to `false`. A job you do not explicitly enable will not be scheduled. |
| `type` | `postgres`, `mysql`, `mariadb`, or `mongodb`. |
| `host` / `host_env`, `port`, `username` / `username_env`, `password_env` | SQL engines. `password_env` names an environment variable; there is no inline password key and no `--password` flag. |
| `uri` / `uri_env` | MongoDB, in place of host and username. |
| `database` | The target database. Required for the SQL engines. |
| `schedule` | Five-field cron expression. Required when the job is enabled. |
| `backup_source` | Where to read from: see below. |
| `staging_dir` | Per-job override of `restore.staging_dir`. One of the two must be set. |
| `restore_mode`, `pitr_timestamp`, `pitr_target_timeline` | Mode selection and PITR target. |
| `incremental_from_backup`, `confirm_full_fallback` | Chain baseline, and pre-authorisation of the full-restore fallback. |
| `conflict_strategy`, `allow_cascade` | `ignore`, `replace`, or `error` (default). `allow_cascade` is PostgreSQL-only. |
| `verify_after_restore` | Runs post-restore verification. Implied by `pitr` and `incremental`. |
| `timeout_seconds`, `keep_file` | Run bound, and retention of staged files for debugging. |
| `restore_options` | Engine flag toggles: `clean`, `if_exists`, `no_owner`, `no_privileges`, `gzip`, `archive`, and `additional_args`. |
| `mysql.binlog_target_time`, `mysql.binlog_target_position` | Binlog replay stop point. |
| `mongodb.oplog_target_timestamp` | Oplog replay stop point. |
| `notifications`, `retention` | Per-job channels, and retention of restore artifacts. |

Inside `backup_source`: `type` (`local`, `s3`, or `gcs`), `backup_path` (required), and
`use_latest_match`. Then `local_path` for local sources; `s3_bucket`, `s3_region`,
`s3_bucket_endpoint`, and `s3_access_key_id_env` / `s3_secret_access_key_env` for S3; `gcs_bucket`,
`gcs_project_id`, and `gcs_credentials_file` for GCS.

## Example

A PostgreSQL restore job that picks up the most recent nightly dump and replays it into a dedicated
verification database:

```yaml
restore:
  staging_dir: /var/lib/sentinel/staging

restores:
  nightly-verify:
    enabled: true
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: SENTINEL_PGPASSWORD
    database: app_restore_check
    schedule: "0 4 * * *"
    restore_mode: full
    conflict_strategy: error
    verify_after_restore: true
    timeout_seconds: 3600
    backup_source:
      type: local
      local_path: /var/backups/sentinel
      backup_path: "SENTINEL_*.sql"
      use_latest_match: true
```

Check what the job would do before running it:

```bash
sentinel restore dry-run nightly-verify --config sentinel.yaml
```

```
Dry-run: Job "nightly-verify"
  Type: postgres
  Database: app_restore_check
  Backup Source Type: local
  Backup Path: SENTINEL_*.sql
  Restore Mode: full
  Timeout: 3600 seconds

NOTE: This is a dry-run. No data will be restored.
```

:::danger Destructive
`sentinel restore run` writes to the database named by the job. Verify the artifact first with
`sentinel backup verify` and confirm that `database:` points at a target you can afford to lose;
`nightly-verify` above deliberately restores into `app_restore_check`, not into the production
database it was dumped from.
:::

```bash
sentinel restore run nightly-verify --config sentinel.yaml
```

```
Restore job "nightly-verify" completed successfully
```

Every run, successful or not, is recorded:

```bash
sentinel restore history nightly-verify --config sentinel.yaml
```

`sentinel restore run --all` runs every *enabled* restore job concurrently, bounded by
`max_concurrent_restores` or by `--parallel N`. A named job runs whether or not it is enabled; the
`enabled` flag governs scheduling and `--all`, not an explicit invocation.

## Failure modes

**The artifact cannot be found.** `backup <path> not found in <type> source`. With
`use_latest_match: true` this usually means the glob matched nothing: `backup_path` is matched
against object paths as listed by the backend, so a pattern written for a different prefix silently
matches zero objects rather than erroring earlier.

**Hash mismatch.** `integrity_check_failed`: the staged bytes do not match the SHA-256 the manifest
recorded at backup time. Treat this as a corrupt or tampered artifact and restore from another copy.
The `--skip-hash-verify` flag on `restore run` downgrades this to a warning; it exists for last-copy
disaster recovery and prints an unmissable warning to stderr. For encrypted artifacts it only
silences the SHA-256 comparison; the AES-256-GCM authentication tag is an independent check that the
flag does not bypass, so genuinely corrupt ciphertext still fails to decrypt.

**Encrypted artifact, no key.** `backup is encrypted but no key provider was supplied`. The manifest
says the artifact is encrypted but neither `encryption_key_env` nor `encryption_key_file` is
configured for the run.

**Not enough staging space.** `insufficient_staging_space`, raised before any download begins, from
the artifact's reported size against free space in `staging_dir`.

**Lock conflict.** The run is recorded as skipped with reason `lock_conflict` because another run of
the same job is in progress. It is not retried.

**Planning rejected.** `restore planning rejected: <reason_code>`. The requested mode is not
supported by the engine or not backed by the manifest; see the reason codes under
[Restore modes](#restore-modes).

**Fallback needs confirmation.** `restore execution requires explicit fallback confirmation`. The
incremental chain is not executable and Sentinel will not silently substitute a full restore. Decide
whether the resulting data loss is acceptable, then set `confirm_full_fallback: true`.

**A required tool is missing.** `required_tool_missing: <tool>` from `pg_combinebackup`,
`mysqlbinlog`, or the engine client. Install the matching client package on the host running the
restore.

## Related

- **[Backup](./backup.md)**: how the artifact and its manifest were produced.
- **[Schedule](./schedule.md)**: running restore jobs on a cron loop rather than by hand.
- **[How Sentinel fits together](../intro/architecture-overview.md)**: where restore sits in the
  overall run model.
- **[Restore command reference](../reference/cli/restore.md)**: every subcommand and flag.
- **[Configuration reference](../reference/configuration.md)**: every YAML key.
- **[Restore a PostgreSQL backup](../tutorials/postgres/restore.md)**: a worked example end to end.
- **[PostgreSQL point-in-time recovery](../tutorials/postgres/pitr.md)**: `pitr` mode in practice.
- [Parallel restore](../guides/parallel-restore.md): restoring several jobs concurrently, and the disk budget that governs it.
- [Restore from gcs](../guides/restore-from-gcs.md): restoring when the artifacts live in Google Cloud Storage.

{/* sources: internal/domain/restore/executor.go, internal/domain/restore/planner.go, internal/domain/restore/plan_types.go, internal/domain/restore/job.go, internal/adapters/restore/runtime/executor.go, internal/adapters/restore/runtime/staging.go, internal/adapters/restore/runtime/preflight.go, internal/adapters/restore/pg/pg_restore.go, internal/adapters/restore/mysql/mysql_restore.go, internal/adapters/restore/mariadb/mariadb_restore.go, internal/adapters/restore/mongo/mongo_restore.go, internal/adapters/restore/mongo/oplog_replay.go, internal/adapters/restore/incremental/mysqlbinlog/replay.go, internal/adapters/restore/incremental/pgcombine/combinebackup.go, internal/config/restore_types.go, internal/config/types.go, internal/config/loader.go, internal/config/marshal.go, internal/cli/restore.go */}
