---
title: Configuration reference
description: Every YAML key Sentinel accepts, with its type, whether it is required, its default, and its engine restrictions.
sidebar_position: 2
---

Sentinel reads a single YAML file, passed with `--config`. This page lists every key the parser
recognises, grouped by the block it belongs to. Keys not listed here do not exist.

Loading happens in a fixed order: parse YAML → apply defaults → resolve named storages → resolve
`*_env` overrides and secrets files → interpolate `${VAR}` references. Validation runs afterwards.
Anything that fails in that sequence fails at **configuration-load time**, never partway through a
running backup.

`n/a` in the Default column means the field has no default: it is left at the Go zero value
(`""`, `0`, `false`, or an empty collection) and nothing downstream substitutes a value.

## Top-level keys

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `version` | string | Yes | n/a | Schema version. Must be exactly `1.0`; any other value is rejected. |
| `defaults` | mapping | No | n/a | Values inherited by every backup job. See [`defaults`](#defaults). |
| `databases` | mapping | Yes | n/a | Backup job definitions keyed by job name. At least one job is required. See [`databases`](#databases). |
| `restores` | mapping | No | n/a | Restore job definitions keyed by job name. See [`restores`](#restores). |
| `restore` | mapping | No | n/a | Shared runtime settings for restore execution. See [`restore`](#restore). |
| `storages` | mapping | No | n/a | Named, reusable storage definitions referenced by `storage.name`. See [storage blocks](#storage-blocks). |
| `max_concurrent_backups` | int | No | `3` | Global backup concurrency limit. Must be between 1 and 100. |
| `max_concurrent_restores` | int | No | `1` | Concurrency limit for `restore run --all`. Must be between 1 and 100; parallelism is opt-in. |
| `scheduler` | mapping | No | n/a | Scheduler concurrency, timeout, and lock settings. See [`scheduler`](#scheduler). |
| `integrity` | mapping | No | n/a | Repository-wide integrity settings. See [`integrity`](#integrity). |
| `log_format` | string | No | `json` | Log output format: `json` or `text`. |
| `history_db_path` | string | No | `~/.sentinel/history.db` | Path to the SQLite execution-history database. Supports `${VAR}` interpolation. |
| `encryption_key_env` | string | No | n/a | Name of the environment variable holding the base64 master key for artifact encryption. Encryption is enabled only when this or `encryption_key_file` is set. |
| `encryption_key_file` | string | No | n/a | Path to a file containing the base64 master key. Supports `${VAR}` interpolation. |
| `secrets_key_env` | string | No | n/a | Name of the environment variable holding the key that decrypts an encrypted secrets file. Falls back to `encryption_key_env` when unset. |
| `secrets_key_file` | string | No | n/a | Path to a file containing the base64 secrets-file key. Falls back to `encryption_key_file` when unset. |

:::info Added in v1.4.0
`secrets_key_env` and `secrets_key_file` were introduced in Sentinel v1.4.0 alongside
at-rest encryption of secrets files.
:::

## `defaults`

Every key here supplies a fallback for the matching key on a backup job. Inheritance is
block-level: a job that declares its own `retention` block does not merge with
`defaults.retention`, it replaces it.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `schedule` | string | No | n/a | 5-field cron expression applied to jobs with no `schedule` of their own. |
| `storage` | mapping | No | n/a | Storage backend applied to jobs that declare neither `storage.type` nor `storage.name`. See [storage blocks](#storage-blocks). |
| `retention` | mapping | No | n/a | Retention policy applied to jobs with no effective retention rule. See [`retention`](#retention). |
| `compression` | mapping | No | n/a | Pipeline compression applied to jobs with no `compression` block. See [`compression`](#compression). |
| `notifications` | list | No | n/a | Notification channels applied to jobs with no `notifications` list. See [`notifications`](#notifications). |

## `databases`

Each entry under `databases` is a backup job; the map key is the job name. The name
`__integrity_check` is reserved and rejected.

### Job identity and connection

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `type` | string | Yes | n/a | One of `postgres`, `mysql`, `mariadb`, `mongodb`. |
| `enabled` | bool | No | `true` | Whether the scheduler runs this job. |
| `host` | string | Conditional | n/a | Server hostname. Required (or `host_env`) for postgres, mysql, mariadb. Not used by mongodb, which connects via `uri`. Supports `${VAR}` interpolation. |
| `host_env` | string | No | n/a | Name of an environment variable holding the host. When set, its value overwrites `host`; the variable must be set at config-load time or loading fails. |
| `port` | int | No | n/a | Server port. For mysql/mariadb, filled from `defaults_file` when left unset. |
| `username` | string | Conditional | n/a | Login user. Required (or `username_env`) for postgres, mysql, mariadb. Supports `${VAR}` interpolation. |
| `username_env` | string | No | n/a | Name of an environment variable holding the username. Overwrites `username`; the variable must be set at config-load time or loading fails. |
| `password_env` | string | Conditional | n/a | Name of an environment variable holding the password. Required for postgres, mysql, and mariadb unless a password is resolved from `defaults_file`. Passwords are never written inline. |
| `defaults_file` | string | No | n/a | Path to a MySQL/MariaDB option file. Its `[client]` section seeds host, user, password, and port for both the dump subprocess and Sentinel's own preflight and discovery. Explicit fields always win. **mysql/mariadb only**: rejected on any other type. |
| `defaults_file_env` | string | No | n/a | Name of an environment variable holding the path for `defaults_file`. Overwrites `defaults_file`; the variable must be set at config-load time or loading fails. |
| `uri` | string | Conditional | n/a | MongoDB connection URI. Required (or `uri_env`) for mongodb. Supports `${VAR}` interpolation. |
| `uri_env` | string | No | n/a | Name of an environment variable holding the MongoDB URI. Overwrites `uri`; the variable must be set at config-load time or loading fails. |
| `mongo_secrets_file` | string | No | n/a | Path to a Sentinel-native secrets file supplying a MongoDB password, URI, and/or TLS key passphrase. **mongodb only**: rejected on any other type. See [secrets files](#secrets-files). |
| `mongo_secrets_file_env` | string | No | n/a | Name of an environment variable holding the path for `mongo_secrets_file`. Overwrites it; the variable must be set at config-load time or loading fails. |
| `tls` | mapping | No | n/a | TLS settings for the database connection. Omitting it, or setting `enabled: false`, logs a `tls_not_configured` warning. Had no effect at all before the fix for [#189](https://github.com/denisakp/sentinel/issues/189). See [`tls`](#tls). |

:::info Added in v1.4.0
`defaults_file`, `mongo_secrets_file`, `defaults_file_env`, and `mongo_secrets_file_env` were
introduced in Sentinel v1.4.0.
:::

### Database selection

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `database` | string | Yes | n/a | Database name, or `*` for auto-discovery. Supports `${VAR}` interpolation. |
| `exclude` | list of string | No | n/a | Database names to skip. Only meaningful with `database: "*"`. |
| `strategy` | string | No | `individual` when `database: "*"` | Auto-discovery strategy: `individual` (one artifact per database) or `single` (one artifact for all). |

### Output and storage

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `output` | string | No | `SENTINEL_<timestamp>` plus the engine extension | Artifact filename. Supports `${VAR}` interpolation. **Used verbatim**, so a literal value such as `shop.sql` is rewritten on every run and truncates the previous artifact: retention then has nothing to prune and an incremental chain collapses onto one file. Use `{timestamp}` or `{date}` in the value, or omit the key, to get one artifact per run ([#193](https://github.com/denisakp/sentinel/issues/193)). |
| `storage` | mapping | Conditional | inherits `defaults.storage` | Storage backend for this job. An effective `storage.type` is required after inheritance. See [storage blocks](#storage-blocks). |
| `database_options` | mapping | No | n/a | Engine-specific dump options. See [`database_options`](#database_options). |
| `compression` | mapping | No | inherits `defaults.compression` | Pipeline compression. See [`compression`](#compression). |
| `verify_after_upload` | bool | No | inherits `integrity.verify_after_upload` | Re-download the artifact from its storage backend after write and re-hash it against the manifest, failing the backup on mismatch. |

:::info Added in v1.3.0
`verify_after_upload` and the `compression` block were introduced in Sentinel v1.3.0.
:::

### Incremental and point-in-time recovery

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `pitr_enabled` | bool | No | `false` | Capture PITR-related metadata for this job. |
| `wal_archive_prefix` | string | No | n/a | Location from which archived WAL segments can be retrieved. PostgreSQL-oriented. |
| `incremental_metadata_enabled` | bool | No | `false` | Capture lineage metadata for future incremental restores. |
| `incremental_backup` | mapping | No | n/a | Chain policy and engine pre-checks. See [`incremental_backup`](#incremental_backup). |
| `mysql` | mapping | No | n/a | MySQL/MariaDB engine options. See [`mysql` on a backup job](#mysql-on-a-backup-job). |

### Scheduling, retention, and notifications

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `schedule` | string | No | inherits `defaults.schedule` | 5-field cron expression. Rejected if unparseable or whitespace-only. |
| `retention` | mapping | No | inherits `defaults.retention` | Retention policy for this job's artifacts. See [`retention`](#retention). |
| `notifications` | list | No | inherits `defaults.notifications` | Notification channels for this job. See [`notifications`](#notifications). |

## `database_options`

A free-form mapping whose allowed keys depend on `type`. An unrecognised key for the job's engine
is a validation error.

### PostgreSQL

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `pg_out_format` | string | No | n/a | `pg_dump` output format: `p`, `c`, `t`, or `d`. |
| `compress` | int | No | n/a | `pg_dump` compression level, 0–9. Engine-native compression. |
| `pg_compression_algo` | string | No | n/a | `gzip`, `lz4`, `zstd`, or `none`. Engine-native compression. |
| `pg_compression_level` | int | No | n/a | Compression level, 1–9. Engine-native compression. |

### MySQL and MariaDB

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `single_transaction` | bool | No | n/a | Adds `--single-transaction` to the dump. |
| `routines` | bool | No | n/a | Adds `--routines`. |
| `triggers` | bool | No | n/a | Adds `--triggers`. |
| `events` | bool | No | n/a | Adds `--events`. |

### MongoDB

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `gzip` | bool | No | n/a | `mongodump --gzip`. Engine-native compression. |
| `oplog` | bool | No | n/a | Adds `--oplog`. |
| `archive` | bool | No | n/a | Adds `--archive`. Local MongoDB backups use `--archive` regardless since the fix for [#191](https://github.com/denisakp/sentinel/issues/191); on v1.4.0 and earlier this key was the only way to avoid a directory artifact. |

:::note Double compression is rejected
Pipeline `compression.enabled: true` may not coexist with engine-native compression; PostgreSQL
`compress` / `pg_compression_algo` / `pg_compression_level`, or MongoDB `gzip`. Configuration
validation fails rather than compressing twice.
:::

## Storage blocks

The same set of keys is used by `storages.<name>`, `defaults.storage`, and
`databases.<name>.storage`. A job may reference a named storage with `name` and override individual
fields on top of it.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `type` | string | Conditional | n/a | `local`, `s3`, `gcs`, `google-drive`, or `azure`. Required on the effective storage of every backup job. |
| `name` | string | No | n/a | Reference to a key in the top-level `storages` map. An unknown reference fails config load. |
| `local_path` | string | No | n/a | Filesystem directory for `local` storage. Supports `${VAR}` interpolation. |
| `s3_bucket` | string | Conditional | n/a | Bucket name. Required for `s3`. Supports `${VAR}` interpolation. |
| `s3_bucket_endpoint` | string | No | n/a | Custom endpoint for S3-compatible services. Supports `${VAR}` interpolation. |
| `s3_region` | string | No | n/a | Bucket region. Supports `${VAR}` interpolation. |
| `s3_access_key_id` | string | No | n/a | Inline access key ID. Logs a `plaintext_password_detected` warning; prefer `s3_access_key_id_env`. |
| `s3_access_key_id_env` | string | No | n/a | Name of an environment variable holding the access key ID. Overwrites `s3_access_key_id`; the variable must be set at config-load time or loading fails. |
| `s3_secret_access_key` | string | No | n/a | Inline secret access key. Logs a `plaintext_password_detected` warning; prefer `s3_secret_access_key_env`. |
| `s3_secret_access_key_env` | string | No | n/a | Name of an environment variable holding the secret access key. Overwrites the inline field; the variable must be set at config-load time or loading fails. |
| `gcs_bucket` | string | Conditional | n/a | Bucket name. Required for `gcs`. Supports `${VAR}` interpolation. |
| `gcs_project_id` | string | No | n/a | Google Cloud project ID. Supports `${VAR}` interpolation. |
| `gcs_credentials_file` | string | No | n/a | Path to a service-account JSON file. Supports `${VAR}` interpolation. |
| `gdrive_folder_id` | string | Conditional | n/a | Target folder ID. Required for `google-drive`. Supports `${VAR}` interpolation. |
| `gdrive_sa_file` | string | Conditional | n/a | Path to a service-account JSON file. Required for `google-drive`. Supports `${VAR}` interpolation. |
| `azure_storage_account` | string | Conditional | n/a | Storage account name. Required for `azure`. Supports `${VAR}` interpolation. |
| `azure_storage_account_env` | string | No | n/a | Name of an environment variable holding the account name. Overwrites the inline field; the variable must be set at config-load time or loading fails. |
| `azure_storage_key` | string | No | n/a | Inline account key. Logs a `plaintext_password_detected` warning; prefer `azure_storage_key_env`. |
| `azure_storage_key_env` | string | No | n/a | Name of an environment variable holding the account key. Overwrites the inline field; the variable must be set at config-load time or loading fails. |
| `azure_container` | string | Conditional | n/a | Blob container name. Required for `azure`. Supports `${VAR}` interpolation. |

## `retention`

Used by `defaults.retention` and `databases.<name>.retention`. A backup is kept if **any** rule
keeps it.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `keep_last` | int | No | n/a | Keep the N most recent backups. |
| `keep_days` | int | No | n/a | Keep backups from the last N days. |
| `dry_run` | bool | No | `false` | Preview deletions without executing them. Requires at least one of `keep_last`, `keep_days`, or `gfs`. |
| `gfs` | mapping | No | n/a | Grandfather-Father-Son calendar-tier rules. See below. |

### `retention.gfs`

Each tier keeps the newest backup of each of the N most-recent **occupied** calendar buckets,
computed in UTC. Empty periods are skipped, not backfilled. All values must be greater than or
equal to 0.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `keep_daily` | int | No | n/a | Keep the newest backup of each of the last N calendar days. |
| `keep_weekly` | int | No | n/a | Keep the newest backup of each of the last N ISO weeks (Monday–Sunday). |
| `keep_monthly` | int | No | n/a | Keep the newest backup of each of the last N calendar months. |
| `keep_yearly` | int | No | n/a | Keep the newest backup of each of the last N calendar years. |

:::info Added in v1.3.0
The `retention.gfs` block was introduced in Sentinel v1.3.0.
:::

## `compression`

Engine-agnostic streaming compression inserted between the dump and the hash/encrypt steps. Used by
`defaults.compression` and `databases.<name>.compression`.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | No | `false` | Turn pipeline compression on. |
| `algorithm` | string | No | `zstd` when enabled | `gzip`, `zstd`, or `none`. `enabled: true` with `none` is a validation error. |
| `level` | int | No | `6` for gzip, `3` for zstd | Codec level: 1–9 for gzip, 1–19 for zstd. |

## `tls`

Per-job TLS settings for the database connection. Applies to all four engines, on the **backup**
path.

:::note The block had no effect at all on v1.4.0 and earlier
Every engine's argument builder read a TLS field and nothing ever populated one. The validator built
the configuration from this block, checked it, and discarded it. So a job with `tls:` connected in
plaintext, for **every** engine, not only MongoDB as [issue #189](https://github.com/denisakp/sentinel/issues/189)
reported.

Worse, the `tls_not_configured` warning fired only when the block was **absent**, so adding it
silenced the one diagnostic that would have said the connection was unencrypted. Someone hardening a
deployment saw the warning stop and concluded it had worked.

The block now reaches all four engines, and the warning depends on whether the connection will
actually be encrypted rather than on whether the key exists: `tls: {enabled: false}` still warns.

**Restore jobs remain unaffected**, because `restores.<name>` has no `tls:` key at all. That is a
missing feature rather than broken wiring, and it is not covered by the fix.
:::

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | No | `false` | Whether TLS settings are applied. The rest of the block is validated only when this is `true`. |
| `mode` | string | No | `prefer` | `require`, `verify-ca`, `verify-full`, or `prefer`. |
| `ca_cert` | string | Conditional | n/a | Path to the CA certificate. Required when `mode` is `verify-ca` or `verify-full`. |
| `client_cert` | string | Conditional | n/a | Path to the client certificate for mutual TLS. Must be set together with `client_key`. |
| `client_key` | string | Conditional | n/a | Path to the client private key. Must be set together with `client_cert`. |
| `client_key_password_env` | string | No | n/a | Name of an environment variable holding the passphrase for an encrypted `client_key`. Requires `client_key` to be set. Takes precedence over a passphrase supplied by `mongo_secrets_file`. |

## `notifications`

A list of channels. Used by `defaults.notifications`, `databases.<name>.notifications`, and
`restores.<name>.notifications`.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `type` | string | Yes | n/a | `slack`, `discord`, `webhook`, or `email`. |
| `enabled` | bool | No | `true` | Whether this channel is dispatched to. |
| `events` | list of string | Yes | n/a | At least one of `success`, `failure`, `warning`. |
| `webhook_url_env` | string | Conditional | n/a | Name of an environment variable holding the webhook URL. Required for `slack`, `discord`, and `webhook`; the variable must be set at config-load time or loading fails. |
| `timeout_seconds` | int | No | `10` for slack/discord/webhook | HTTP timeout. Must be between 1 and 60. |
| `smtp_host` | string | Conditional | n/a | SMTP server hostname. Required for `email`. Supports `${VAR}` interpolation. |
| `smtp_port` | int | No | n/a | SMTP server port. Must be between 1 and 65535. |
| `smtp_username_env` | string | No | n/a | Name of an environment variable holding the SMTP username. |
| `smtp_password_env` | string | Conditional | n/a | Name of an environment variable holding the SMTP password. Required for `email`; the variable must be set at config-load time or loading fails. |
| `from_address_env` | string | Conditional | n/a | Name of an environment variable holding the sender address. Required for `email`; the variable must be set at config-load time or loading fails. |
| `to_addresses` | list of string | Conditional | n/a | Recipient addresses. Required for `email`. Supports `${VAR}` interpolation. |
| `use_tls` | bool | No | `true` for `email` | Use TLS for the SMTP connection. |

## `incremental_backup`

Per-job chain policy and engine pre-checks.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | No | `false` | Enable chain-based incremental backups. Supported for postgres, mysql, mariadb, and mongodb; any other type is a validation error. |
| `max_chain_depth` | int | No | `6` | Depth at which the chain resets by producing a new full backup. Must be greater than or equal to 0. |
| `wal_summary_check` | bool | No | `false` | Verify PostgreSQL `wal_summary=on` before an incremental backup. **postgres only.** |
| `binlog_check` | bool | No | `false` | Verify MySQL/MariaDB `log_bin=ON` before an incremental backup. **mysql/mariadb only.** |
| `oplog_window_warn_hours` | int | No | n/a | Warn when the MongoDB oplog window falls below this many hours. Must be greater than or equal to 0. **mongodb only.** |

## `mysql` on a backup job

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `binlog_path` | string | Conditional | n/a | Local or mounted path to the binary logs, readable by Sentinel. Required when `incremental_backup.enabled` is true for mysql/mariadb. **mysql/mariadb only.** |

## `scheduler`

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `max_concurrent_backups` | int | No | the top-level `max_concurrent_backups` | Scheduler-side backup concurrency limit. |
| `max_concurrent_restores` | int | No | `1` | Scheduler-side restore concurrency limit. |
| `job_timeout_minutes` | int | No | `180` | Deadline for a **scheduled** backup, in minutes. The deadline reaches the dump subprocess, so a wedged dump is killed rather than holding a concurrency slot. A one-shot `sentinel backup --config` is not bounded by it. Was read by nothing on v1.4.0 and earlier ([#194](https://github.com/denisakp/sentinel/issues/194)). |
| `stale_lock_threshold` | int | No | `60` | Age in minutes after which a lock held by a dead process is treated as stale. |
| `lock_dir` | string | No | `/var/run/sentinel` | Directory for per-job lock files. |

## `integrity`

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `algorithm` | string | No | `sha256` | Hash algorithm for integrity verification. Only `sha256` is accepted. |
| `verify_after_upload` | bool | No | `false` | Default for backup jobs: re-download and re-hash each artifact after upload. Overridden per job by `verify_after_upload`. |
| `scheduled_check` | mapping | No | n/a | Cron-driven repository integrity sweep. See below. |

### `integrity.scheduled_check`

When enabled, the scheduler registers a reserved `__integrity_check` job that runs the same sweep as
`backup verify --all`.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | No | `false` | Turn the scheduled sweep on. |
| `cron` | string | Conditional | n/a | 5-field cron expression. Required when `enabled` is true. |
| `since` | string | No | n/a | Recency window restricting the sweep to newer backups. Accepts an integer with a `d` or `w` suffix (`30d`, `4w`) or any Go duration (`720h`). Empty means all backups. |
| `notify_on` | string | No | `failure` | `failure`, `always`, or `never`. |
| `job` | string | No | n/a | Restrict the sweep to a single named backup job. |

:::info Added in v1.3.0
`integrity.scheduled_check` and `integrity.verify_after_upload` were introduced in Sentinel v1.3.0.
:::

## `restore`

Shared runtime settings for restore execution. Both keys supply defaults for every restore job.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `staging_dir` | string | No | `/tmp/sentinel` | Base directory for staged restore artifacts. Supports `${VAR}` interpolation. |
| `keep_file` | bool | No | `false` | Retain staged artifacts after execution, for debugging. |

## `restores`

Each entry under `restores` is a restore job; the map key is the job name. Restore jobs default to
**disabled**: unlike backup jobs, they must be enabled explicitly. The name `__integrity_check` is
reserved and rejected.

### Restore job identity and connection

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | No | `false` | Whether the scheduler runs this job. |
| `type` | string | Yes | n/a | One of `postgres`, `mysql`, `mariadb`, `mongodb`. |
| `host` | string | Conditional | n/a | Target hostname. Required (or `host_env`) for postgres, mysql, mariadb. Supports `${VAR}` interpolation. |
| `host_env` | string | No | n/a | Name of an environment variable holding the host. Overwrites `host`; the variable must be set at config-load time or loading fails. |
| `port` | int | No | n/a | Target port. |
| `username` | string | Conditional | n/a | Login user. Required (or `username_env`) for postgres, mysql, mariadb. Supports `${VAR}` interpolation. |
| `username_env` | string | No | n/a | Name of an environment variable holding the username. Overwrites `username`; the variable must be set at config-load time or loading fails. |
| `password_env` | string | No | n/a | Name of an environment variable holding the password. Resolved at restore time; an unset variable fails the restore. |
| `uri` | string | Conditional | n/a | MongoDB connection URI. Required (or `uri_env`) for mongodb. Supports `${VAR}` interpolation. |
| `uri_env` | string | No | n/a | Name of an environment variable holding the MongoDB URI. Overwrites `uri`; the variable must be set at config-load time or loading fails. |
| `database` | string | Conditional | n/a | Database to restore into. Required for postgres, mysql, mariadb. Supports `${VAR}` interpolation. |

### Restore source and scheduling

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `backup_source` | mapping | Yes | n/a | Where the artifact is read from. See [`backup_source`](#backup_source). |
| `schedule` | string | Conditional | n/a | 5-field cron expression. Required when the job is enabled. |
| `timeout_seconds` | int | No | n/a | Maximum duration for the restore. Must be non-negative. |
| `staging_dir` | string | No | inherits `restore.staging_dir` | Staging directory override for this job. Must be non-empty after inheritance. |
| `keep_file` | bool | No | inherits `restore.keep_file` | Retain the staged artifact after the restore attempt. |
| `notifications` | list | No | n/a | Notification channels for restore results. See [`notifications`](#notifications). |
| `retention` | mapping | No | n/a | Retention for restore backup files. See [restore retention](#restore-retention). |

### Restore mode and targets

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `restore_mode` | string | No | `full` | `full`, `pitr`, or `incremental`. `pitr` and `incremental` are accepted only for `postgres`; the validator rejects them for MySQL, MariaDB and MongoDB, because the planner cannot run them. `incremental` was accepted for all four before the fix for [issue #186](https://github.com/denisakp/sentinel/issues/186), and failed at `restore run` instead. |
| `pitr_timestamp` | string | Conditional | n/a | RFC3339 timestamp with timezone. Required when `restore_mode: pitr`; rejected in other modes. **postgres only**: `pitr` mode is rejected for other engines. |
| `pitr_target_timeline` | string | No | n/a | Recovery timeline for PITR. Valid only when `restore_mode: pitr`. |
| `incremental_from_backup` | string | Conditional | n/a | Baseline backup for incremental planning. Required when `restore_mode: incremental`; rejected in other modes. |
| `confirm_full_fallback` | bool | No | `false` | Authorise falling back to a full restore. Valid only when `restore_mode: incremental`. |
| `mysql` | mapping | No | n/a | Binlog replay selectors. See [restore engine blocks](#restore-engine-blocks). |
| `mongodb` | mapping | No | n/a | Oplog replay options. See [restore engine blocks](#restore-engine-blocks). |

### Restore behaviour

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `restore_options` | mapping | No | n/a | Engine restore flags. See [`restore_options`](#restore_options). |
| `verify_after_restore` | bool | No | `false` | Run post-restore verification. |
| `conflict_strategy` | string | No | `error` | What to do when data already exists: `ignore`, `replace`, or `error`. |
| `allow_cascade` | bool | No | `false` | Permit `DROP ... CASCADE` during a replace. **postgres only**: and required when `conflict_strategy: replace` on postgres. |

:::danger Destructive
`conflict_strategy: replace` overwrites the target database, and on PostgreSQL `allow_cascade: true`
additionally removes dependent objects. Confirm the artifact is sound with
`sentinel backup verify <id>` before enabling either.
:::

## `backup_source`

Where a restore job reads its artifact from.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `type` | string | Yes | n/a | `local`, `s3`, or `gcs`. Any other value is rejected. Supports `${VAR}` interpolation. |
| `backup_path` | string | Yes | n/a | Object path or filename. Supports `${VAR}` interpolation. |
| `use_latest_match` | bool | No | `false` | When `backup_path` is a pattern such as `*.sql`, select the most recent match. |
| `local_path` | string | Conditional | n/a | Filesystem directory. Required when `type: local`. Supports `${VAR}` interpolation. |
| `s3_bucket` | string | Conditional | n/a | Bucket name. Required when `type: s3`. Supports `${VAR}` interpolation. |
| `s3_bucket_endpoint` | string | No | n/a | Custom endpoint for S3-compatible services. Supports `${VAR}` interpolation. |
| `s3_region` | string | No | n/a | Bucket region. Supports `${VAR}` interpolation. |
| `s3_access_key_id` | string | No | n/a | Inline access key ID. Prefer `s3_access_key_id_env`. |
| `s3_access_key_id_env` | string | No | n/a | Name of an environment variable holding the access key ID. Overwrites the inline field; the variable must be set at config-load time or loading fails. |
| `s3_secret_access_key` | string | No | n/a | Inline secret access key. Prefer `s3_secret_access_key_env`. |
| `s3_secret_access_key_env` | string | No | n/a | Name of an environment variable holding the secret access key. Overwrites the inline field; the variable must be set at config-load time or loading fails. |
| `gcs_bucket` | string | Conditional | n/a | Bucket name. Required when `type: gcs`. Supports `${VAR}` interpolation. |
| `gcs_project_id` | string | No | n/a | Google Cloud project ID. Supports `${VAR}` interpolation. |
| `gcs_credentials_file` | string | No | n/a | Path to a service-account JSON file. Supports `${VAR}` interpolation. |
| `gdrive_folder_id` | string | No | n/a | Google Drive folder ID. Parsed and interpolated, but `type: google-drive` is not an accepted restore source today. |
| `gdrive_sa_file` | string | No | n/a | Path to a Google Drive service-account JSON file. Same restriction as `gdrive_folder_id`. |
| `azure_storage_account` | string | No | n/a | Azure storage account name. Parsed and interpolated, but `type: azure` is not an accepted restore source today. |
| `azure_storage_account_env` | string | No | n/a | Name of an environment variable holding the account name. Overwrites the inline field; the variable must be set at config-load time or loading fails. |
| `azure_storage_key` | string | No | n/a | Inline Azure account key. Prefer `azure_storage_key_env`. |
| `azure_storage_key_env` | string | No | n/a | Name of an environment variable holding the account key. Overwrites the inline field; the variable must be set at config-load time or loading fails. |
| `azure_container` | string | No | n/a | Azure blob container. Same restriction as `azure_storage_account`. |

## `restore_options`

A free-form mapping. These are the keys the restore argument builder acts on; unrecognised keys are
ignored rather than rejected.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `clean` | bool | No | n/a | Adds `--clean`. |
| `if_exists` | bool | No | n/a | Adds `--if-exists`. |
| `no_owner` | bool | No | n/a | Adds `--no-owner`. |
| `no_privileges` | bool | No | n/a | Adds `--no-privileges`. |
| `gzip` | bool | No | n/a | Adds `--gzip`. **mongodb only** in the additional-args path. |
| `archive` | bool | No | n/a | Restore from an archive file. **mongodb only.** |
| `additional_args` | string | No | n/a | Extra arguments, shell-quoted. Must be a string, and must parse at config-load time. |

## Restore retention

Used by `restores.<name>.retention`.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `keep_last` | int | No | n/a | Keep the last N restore backup files. `0` means no limit. Must be non-negative. |
| `keep_days` | int | No | n/a | Keep restore backup files from the last N days. `0` means no limit. Must be non-negative. |
| `dry_run` | bool | No | `false` | Preview deletions without removing files. |

## Restore engine blocks

### `mysql` on a restore job

Both selectors are mutually exclusive, and both are **mysql/mariadb only**.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `binlog_target_time` | string | No | n/a | RFC3339 timestamp with timezone at which binlog replay stops. |
| `binlog_target_position` | mapping | No | n/a | Explicit binlog stop position. Requires both sub-keys below. |

The `binlog_target_position` mapping:

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `file` | string | Yes | n/a | Binary log filename. Must be non-empty. |
| `pos` | int | Yes | n/a | Position within the file. Must be greater than 0. |

### `mongodb` on a restore job

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `oplog_target_timestamp` | string | No | n/a | RFC3339 timestamp with timezone at which oplog replay stops. **mongodb only.** |

## Secrets files

A secrets file is a separate file referenced from a backup job, not a block inside the main
configuration. Both kinds may be stored encrypted; encryption is auto-detected from the file's
content and the file is decrypted in memory using `secrets_key_env` / `secrets_key_file`, falling
back to `encryption_key_env` / `encryption_key_file`. A plaintext file that is group- or
world-readable produces a permission warning.

### MySQL and MariaDB `defaults_file`

A standard option file. Only the `[client]` section is read; no tool-specific sections, no
`!include` or `!includedir` directives. It can supply `host`, `user`, `password`, and `port`, each
used only when the corresponding job field is left unset.

### MongoDB `mongo_secrets_file`

A Sentinel-native YAML file. All three keys are optional.

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `uri` | string | No | n/a | Full connection URI. Used only when the job sets neither `uri` nor `uri_env`. |
| `password` | string | No | n/a | Password composed into the job's URI userinfo. Fails config load if the URI already carries a password, or if no username can be resolved. |
| `ssl_pem_key_password` | string | No | n/a | Passphrase for an encrypted TLS client key. Used only when `tls.client_key_password_env` is unset. |

:::info Added in v1.4.0
Both secrets-file mechanisms, their `*_env` path indirection, and at-rest encryption were introduced
in Sentinel v1.4.0.
:::

## Environment variables and interpolation

Sentinel resolves external values in two distinct ways.

**The `*_env` family**; `host_env`, `username_env`, `password_env`, `uri_env`, `defaults_file_env`,
`mongo_secrets_file_env`, `webhook_url_env`, `smtp_username_env`, `smtp_password_env`,
`from_address_env`, `client_key_password_env`, `s3_access_key_id_env`, `s3_secret_access_key_env`,
`azure_storage_account_env`, `azure_storage_key_env`, plus the top-level `encryption_key_env` and
`secrets_key_env`. Each names an environment **variable**, never a value. The variable name must
match `^[A-Z_][A-Z0-9_]*$`. When the field is set, the resolved value overwrites its inline
counterpart, and an unset or empty variable fails configuration load with an error naming the job
and the variable; never a silent fallback.

**`${VAR}` interpolation**: a subset of string fields (paths, hostnames, bucket names, usernames,
URIs, output names, SMTP hosts, recipient addresses) expand `${VAR}` references inline. The same
name pattern applies, and an unset variable is likewise a load-time error.

## Not part of the loaded schema

Three structures carry YAML tags in the source but are not reachable from the top-level
configuration that Sentinel parses, so their keys have no effect in a configuration file:

| Structure | Keys | Status |
|---|---|---|
| `AzureConfig` / `AzureAuthConfig` | `account_name`, `container`, `tier`, `auth.type`, `auth.connection_string`, `auth.connection_string_env`, `auth.sas_token`, `auth.sas_token_env` | Not referenced by any parsed block. Configure Azure through the `azure_*` keys in a [storage block](#storage-blocks). |
| `RestoreDefaults` | `restore_defaults` and its sub-keys | Defined on a `RestoreConfiguration` type that the loader never parses. Set the equivalent fields directly on each restore job. |

## A complete example

```yaml
version: "1.0"

log_format: json
history_db_path: ~/.sentinel/history.db
max_concurrent_backups: 3
max_concurrent_restores: 1

encryption_key_env: SENTINEL_MASTER_KEY
secrets_key_env: SENTINEL_SECRETS_KEY

scheduler:
  job_timeout_minutes: 180
  stale_lock_threshold: 60
  lock_dir: /var/run/sentinel

integrity:
  algorithm: sha256
  verify_after_upload: true
  scheduled_check:
    enabled: true
    cron: "30 4 * * 0"
    since: 30d
    notify_on: failure

storages:
  primary-s3:
    type: s3
    s3_bucket: example-backups
    s3_region: eu-west-3
    s3_access_key_id_env: AWS_ACCESS_KEY_ID
    s3_secret_access_key_env: AWS_SECRET_ACCESS_KEY

defaults:
  schedule: "0 2 * * *"
  storage:
    name: primary-s3
  retention:
    keep_last: 14
    gfs:
      keep_daily: 7
      keep_weekly: 4
      keep_monthly: 12
  compression:
    enabled: true
    algorithm: zstd
    level: 3
  notifications:
    - type: slack
      webhook_url_env: SLACK_WEBHOOK_URL
      events: [failure, warning]

databases:
  app-postgres:
    type: postgres
    host: pg.internal.example
    port: 5432
    username: sentinel
    password_env: PG_BACKUP_PASSWORD
    database: appdb
    tls:
      enabled: true
      mode: verify-full
      ca_cert: /etc/sentinel/certs/pg-ca.pem
    pitr_enabled: true
    wal_archive_prefix: s3://example-backups/wal/app-postgres
    incremental_backup:
      enabled: true
      max_chain_depth: 6
      wal_summary_check: true
    database_options:
      pg_out_format: c

  reporting-mysql:
    type: mysql
    defaults_file_env: MYSQL_DEFAULTS_FILE
    database: "*"
    strategy: individual
    exclude: [information_schema, performance_schema]
    schedule: "0 3 * * *"
    mysql:
      binlog_path: /var/lib/mysql
    incremental_backup:
      enabled: true
      binlog_check: true

  events-mongo:
    type: mongodb
    uri_env: MONGO_URI
    mongo_secrets_file: /etc/sentinel/mongo-secrets.enc.yaml
    database: events
    database_options:
      oplog: true
    storage:
      type: local
      local_path: /var/backups/sentinel

restore:
  staging_dir: /var/tmp/sentinel-restore
  keep_file: false

restores:
  weekly-restore-test:
    enabled: true
    type: postgres
    host: pg-verify.internal.example
    port: 5432
    username: sentinel
    password_env: PG_RESTORE_PASSWORD
    database: appdb_verify
    schedule: "0 5 * * 6"
    restore_mode: full
    conflict_strategy: error
    verify_after_restore: true
    timeout_seconds: 3600
    backup_source:
      type: s3
      s3_bucket: example-backups
      s3_region: eu-west-3
      s3_access_key_id_env: AWS_ACCESS_KEY_ID
      s3_secret_access_key_env: AWS_SECRET_ACCESS_KEY
      backup_path: "app-postgres/*.dump"
      use_latest_match: true
    restore_options:
      no_owner: true
      no_privileges: true
    retention:
      keep_last: 4
```

## Related

- [Backup concepts](../concepts/backup.md): what a backup job does with these keys.
- [Restore concepts](../concepts/restore.md): how a restore job plans and stages an artifact.
- [Schedule concepts](../concepts/schedule.md): how cron expressions and concurrency limits interact.
- [`sentinel backup` command reference](./cli/backup.md)
- [`sentinel restore` command reference](./cli/restore.md)
- [`sentinel schedule` command reference](./cli/schedule.md)

{/* sources: internal/config/types.go, internal/config/restore_types.go, internal/config/loader.go, internal/config/validator.go, internal/config/env.go, internal/config/since.go, internal/config/marshal.go, internal/config/defaults_file_resolve.go, internal/config/mongo_secrets_file.go, internal/config/secrets_file_crypto.go, internal/adapters/storage/validation.go, internal/ports/tls.go, internal/domain/backup/validator.go, release-notes.md */}
