---
title: sentinel backup
description: Reference for sentinel backup and its verify, chain-status, chain-list, force-full, and diff subcommands, with every flag.
sidebar_position: 2
---

Runs a database backup from a YAML configuration file or from CLI flags, and groups the subcommands that inspect, verify, and compare the resulting artifacts.

## Synopsis

```text
sentinel backup [flags]
sentinel backup [command]
```

When `--config` resolves to a valid configuration, every enabled job in `databases:` runs and all other CLI flags act as per-run overrides. When `--config` is omitted and the configuration cannot be loaded, `--type` selects a single ad-hoc job built entirely from flags.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `verify` | Re-computes the SHA-256 fingerprint of a stored artifact and compares it against the manifest value. Takes one `[backup-id]`, or `--all` for a repository-wide sweep. |
| `chain-status` | Prints the active chain ID, chain index, chain depth, latest backup type, and last success time for one configured job. |
| `chain-list` | Lists the successful backup executions belonging to one incremental chain ID, ordered by chain index. |
| `force-full` | Runs a full backup immediately for a configured job and resets its incremental chain state. |
| `diff` | Compares the recorded metadata of two backups and flags silent security regressions. |

## Flags

### `sentinel backup`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--args` | string | n/a | Extra arguments appended to the underlying dump command. Overrides `additional_args` from the config when set. |
| `--aws-access-key-id` | string | n/a | AWS access key ID for the S3 backend. |
| `--aws-bucket` | string | n/a | AWS S3 bucket name. |
| `--aws-bucket-endpoint` | string | n/a | S3 endpoint URL. Set this for S3-compatible services such as MinIO. |
| `--aws-region` | string | `us-east-1` | AWS region. |
| `--aws-secret` | string | n/a | AWS secret access key. |
| `-c`, `--compress` | bool | `false` | Compress the backup. Applies to PostgreSQL (maps to `compress`) and MongoDB (maps to `gzip`); ignored by MySQL and MariaDB. |
| `--config` | string | n/a | Path to the YAML configuration file. The preferred mode of operation. |
| `-d`, `--database` | string | n/a | Database name. `*` in the config triggers auto-discovery. |
| `--gcs-bucket` | string | n/a | Google Cloud Storage bucket name. Required when `--storage gcs`. |
| `--gcs-credentials-file` | string | n/a | Path to the Google Cloud service account key file. |
| `--gcs-project-id` | string | n/a | Google Cloud project ID. Optional. |
| `--gdrive-folder-id` | string | n/a | Google Drive folder ID. Required when `--storage google-drive`. |
| `--gdrive-sa-file` | string | n/a | Path to the Google Drive service account file. Required when `--storage google-drive`. |
| `-h`, `--help` | bool | `false` | Print help for `backup`. |
| `-H`, `--host` | string | `127.0.0.1` | Database host. |
| `--local-path` | string | n/a | Directory the artifact is written to when `--storage local`. |
| `-o`, `--output` | string | n/a | Output artifact name. Scheduled runs append a job name and timestamp to it. |
| `--password-env` | string | n/a | Name of the environment variable holding the database password. |
| `--password-file` | string | n/a | Path to a file whose first line is the database password. A group- or world-readable file produces a permissions warning on stderr. |
| `--pg-compression-algo` | string | n/a | PostgreSQL compression algorithm: `gzip`, `lz4`, `zstd`, or `none`. |
| `--pg-compression-level` | int | `1` | PostgreSQL compression level, `1`–`9`. |
| `--pg-out-format` | string | n/a | PostgreSQL output format: `p` (plain), `c` (custom), `d` (directory), or `t` (tar). |
| `-P`, `--port` | string | n/a | Database port. Must parse as an integer when overriding a configured job. |
| `-s`, `--storage` | string | `local` | Storage backend: `local`, `s3`, `gcs`, or `google-drive`. |
| `-t`, `--type` | string | n/a | Database engine: `mysql`, `postgres`, `mariadb`, or `mongodb`. |
| `--uri` | string | `mongodb://localhost:27017` | MongoDB connection URI. |
| `-u`, `--user` | string | `root` | Database user. |

:::warning No `--password` flag
There is no supported flag for passing a password on the command line. Use `--password-env`, `--password-file`, or `databases.<id>.password_env` in the configuration file. A password in `argv` is visible via `ps`, `/proc/<pid>/cmdline`, and shell history.
:::

### `sentinel backup verify [backup-id]`

Accepts at most one positional `backup-id`. Exactly one of `<backup-id>` or `--all` must be supplied.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--all` | bool | `false` | Verify every recorded successful backup and emit an aggregate report plus a single exit code. Mutually exclusive with a positional `backup-id`. |
| `--allow-legacy-envelope` | bool | `false`, or `true` when `SENTINEL_ALLOW_LEGACY_ENVELOPE` is `1`/`true`/`yes` | Decrypt artifacts written before the v2 encryption envelope. Unsafe: pre-v2 streams used a flawed nonce scheme. Use only to recover plaintext for re-encryption. |
| `--config` | string | `$HOME/.sentinel/config.yaml` | Path to the Sentinel YAML config. Supplies the history database path and the storage credentials used to fetch remote artifacts. |
| `-h`, `--help` | bool | `false` | Print help for `verify`. |
| `--ignore-missing-manifest` | bool | `false` | With `--all`: treat a `missing_manifest` result as a warning (exit 0) rather than an integrity failure. |
| `--job` | string | n/a | With `--all`: restrict the sweep to a single named backup job. |
| `--output` | string | inherits `log_format` from the config | Output format: `json` or `text`. |
| `--since` | string | n/a | With `--all`: only verify backups newer than this age. Accepts `30d`, `4w`, or any Go duration such as `720h`. |

Each verified backup is classified as `ok`, `corrupted`, `missing_artifact`, or `missing_manifest`. Artifacts on `s3`, `gcs`, `azure`, or `google-drive` are downloaded with their `<key>.manifest.json` sidecar to a temporary directory, verified there, and the directory is removed before the command returns.

Exit codes: `0` success, `2` backup ID not found, `3` verification skipped (no manifest), `4` operational failure (config, history database, listing), `5` at least one real integrity failure during an `--all` sweep.

### `sentinel backup chain-status`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `chain-status`. |
| `--job` | string | n/a | Configured backup job name. Required. |

### `sentinel backup chain-list`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--chain-id` | string | n/a | Incremental chain ID to list. Required. |
| `-h`, `--help` | bool | `false` | Print help for `chain-list`. |

### `sentinel backup force-full`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `force-full`. |
| `--job` | string | n/a | Configured backup job name. Required. |

:::caution `chain-status`, `chain-list`, and `force-full` require a config they cannot receive
All three handlers reject the run with `Error: --config is required`, but none of them registers a `--config` flag and `backup`'s own `--config` is a local flag rather than a persistent one, so passing `--config` returns `Error: unknown flag: --config`. As of v1.4.0 these three subcommands cannot complete. Use `sentinel monitor` to inspect chain metadata in the meantime.
:::

### `sentinel backup diff <id1> <id2>`

Requires exactly two positional backup IDs.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--config` | string | `$HOME/.sentinel/config.yaml` | Path to the Sentinel YAML config. |
| `-h`, `--help` | bool | `false` | Print help for `diff`. |
| `--output` | string | inherits `log_format` from the config | Output format: `json` or `text`. |

`diff` reads only the monitor row and the `<artifact>.manifest.json` sidecar for each ID; no artifact bytes are read. Security regressions; encryption turned off, a hash-algorithm change, an encryption-parameter downgrade; exit non-zero (1). Size and duration swings of 50% or more are marked with a warning symbol and remain exit 0.

## Examples

Run every enabled job in a configuration file:

```bash
sentinel backup --config sentinel.yaml
```

Each job prints its own progress; retention output follows for jobs with a retention policy. A non-zero exit means at least one job failed.

Run a single ad-hoc PostgreSQL backup to a local directory, reading the password from the environment:

```bash
export PGPASSWORD_FOR_SENTINEL='<from-your-secret-store>'
sentinel backup \
  --type postgres --host db.internal --port 5432 \
  --user backup_operator --database app \
  --password-env PGPASSWORD_FOR_SENTINEL \
  --storage local --local-path /var/backups/sentinel \
  --pg-out-format c --compress
```

A custom-format dump appears under `/var/backups/sentinel` alongside its `.manifest.json` sidecar.

Back up to an S3-compatible endpoint:

```bash
sentinel backup --config sentinel.yaml \
  --storage s3 --aws-bucket sentinel-backups \
  --aws-region eu-west-3 --aws-bucket-endpoint https://minio.internal:9000
```

The flags override the configured storage block for this run only; the config file is not modified.

Verify one recorded backup:

```bash
sentinel backup verify 01HQ8Z3K4M5N6P7Q8R9S --config sentinel.yaml
```

Prints the stored and computed SHA-256 values. Exit 0 means they match.

Sweep the whole repository for the last 30 days as JSON, for a cron or CI gate:

```bash
sentinel backup verify --all --since 30d --output json --config sentinel.yaml
```

Emits one JSON record per backup with `status` and `hash_match`. Exit 5 means at least one backup is corrupted or its artifact is missing.

Compare two backups of the same job to catch a silent security regression:

```bash
sentinel backup diff 01HQ8Z3K4M5N6P7Q8R9S 01HQ9A1B2C3D4E5F6G7H --config sentinel.yaml
```

Prints a table of differing fields. A non-zero exit means encryption or hashing weakened between the two runs.

## Related

- [Backups: what Sentinel captures and how](../../concepts/backup.md)
- [Configuration reference](../configuration.md)
- [`sentinel restore`](./restore.md)
- [`sentinel schedule`](./schedule.md)
- [CLI reference index](./index.md)
- [Your first PostgreSQL backup](../../tutorials/postgres/first-backup.md)

{/* sources: internal/cli/backup.go, internal/cli/backup_verify.go, internal/cli/backup_diff.go, internal/cli/exit_codes.go, internal/cli/legacy_envelope.go, internal/config/since.go, internal/adapters/storage/validation.go */}
