---
title: sentinel restore
description: Reference for sentinel restore and its ten subcommands, with every flag, the run --all concurrency model, and the enable and run caveats.
sidebar_position: 3
---

Groups the subcommands that inspect, validate, and execute restore jobs defined under `restores:` in the YAML configuration file.

## Synopsis

```text
sentinel restore [command]
```

`sentinel restore` has no `RunE` of its own; it is a command group. Every subcommand loads the configuration through `--config` (or `./sentinel-config.yaml` in the working directory), runs full schema validation, and then looks the job up by its `restores:` map key.

Restore jobs default to **disabled**: when `enabled` is absent from a job's YAML block, the loader sets it to `false`. This is the opposite of backup jobs, which default to `true`.

## Subcommands

| Subcommand | Arguments | Purpose |
|---|---|---|
| `list` | none | Prints every configured restore job with its type, schedule, enabled state, and target database. |
| `status` | `<job-name>` (exactly 1) | Prints one job's type, database, schedule, enabled state, effective restore mode, `verify_after_restore`, timeout, and `keep_file`. |
| `enable` | `<job-name>` (exactly 1) | Logs and prints an enable acknowledgement for the named job. Does not write to the configuration file; see the caution below. |
| `disable` | `<job-name>` (exactly 1) | Logs and prints a disable acknowledgement. Does not write to the configuration file. |
| `pause` | `<job-name>` (exactly 1) | Logs and prints a pause acknowledgement. Does not write to the configuration file. |
| `resume` | `<job-name>` (exactly 1) | Logs and prints a resume acknowledgement. Does not write to the configuration file. |
| `dry-run` | `<job-name>` (exactly 1) | Prints the resolved execution parameters for one job without contacting a database or fetching an artifact. |
| `run` | `[job-name]` (at most 1) | Executes one restore job immediately, or every enabled job with `--all`. |
| `history` | `[job-name]` (optional) | Prints the 50 most recent restore executions from the SQLite history database, optionally filtered to one job. |
| `validate-chain` | `<job-name>` (exactly 1) | Validates incremental lineage and planner readiness for one `restore_mode: incremental` job without restoring anything. |

:::caution `enable`, `disable`, `pause`, and `resume` do not persist
As of v1.4.0 all four handlers only emit a log line and a confirmation message. They do not modify the configuration file and there is no separate state store, so the change is lost the moment the process exits. To actually enable a restore job, set `enabled: true` on it in the YAML configuration file. The same applies to disabling, pausing, and resuming.
:::

## Flags

### Flags available on every restore command

These are persistent flags on `sentinel restore`, so they are accepted by the group itself and by all ten subcommands.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--allow-legacy-envelope` | bool | `false`, or `true` when `SENTINEL_ALLOW_LEGACY_ENVELOPE` is `1`, `true`, `TRUE`, `yes`, or `YES` | Decrypt artifacts written before the v2 encryption envelope. Unsafe: pre-v2 streams used a flawed nonce scheme. Use only to recover plaintext for re-encryption. Without it, a pre-v2 artifact is refused with an explicit error naming the job and backup path. |
| `--config` | string | `./sentinel-config.yaml` in the working directory | Path to the YAML configuration file. If the flag is empty and the default file is absent, the command fails with a message telling you to pass `--config`. |
| `-h`, `--help` | bool | `false` | Print help for the command it is passed to. |
| `--log-level` | string | `info` | Log level for the JSON handler installed by `PersistentPreRun`: `debug`, `info`, `warn`, or `error`. Any other value falls back to `info`. |

### `sentinel restore run [job-name]`

Local flags, registered on `run` only. They are deliberately not persistent, so they cannot leak onto `list`, `status`, or `dry-run`.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--all` | bool | `false` | Run every **enabled** restore job concurrently instead of one named job. Combining `--all` with a positional job name is rejected with `cannot combine --all with a job name`. |
| `--gcs-bucket` | string | n/a | Google Cloud Storage bucket name. Setting it also forces `backup_source.type` to `gcs` for this run. |
| `--gcs-credentials-file` | string | n/a | Path to the Google Cloud service account key file, overriding `backup_source.gcs_credentials_file`. |
| `--gcs-project-id` | string | n/a | Google Cloud project ID, overriding `backup_source.gcs_project_id`. Optional. |
| `--keep-file` | bool | `false` | Keep the staged restore artifact after the run instead of deleting it. Sets `keep_file` on the job for this run only; the completion message then prints the retained path. |
| `--parallel` | int | `0` | Maximum concurrent restore jobs for `--all`. `0` means "use `max_concurrent_restores`". Ignored when `--all` is not set. |
| `--skip-hash-verify` | bool | `false` | Unsafe: proceed even when the artifact's SHA-256 does not match the manifest. Downgrades the integrity abort to a logged `WARNING` plus a stderr banner. Flag-only and per-invocation; it is never read from an environment variable or the configuration file. |

All run overrides are applied to an in-memory copy of the job. The configuration file is never modified.

#### Concurrency for `--all`

The effective limit is resolved in this order:

1. `--parallel N` when `N` is greater than `0`.
2. The top-level `max_concurrent_restores` configuration key when it is greater than `0`.
3. `1`.

`max_concurrent_restores` defaults to `1`, so `--all` is **serial by default**: parallelism is opt-in. The validator accepts values from `1` to `100`. Jobs are dispatched into a bounded semaphore; a per-job failure or panic is captured rather than propagated, so one bad job never aborts its siblings. When the batch finishes, `run` prints one `OK`/`FAILED` line per job followed by `Restore run-all: <n>/<total> succeeded (concurrency=<limit>)`, and exits non-zero if any job failed.

`--all` selects jobs by their enabled state. Because the loader defaults an unset `enabled` to `false`, only jobs with an explicit `enabled: true` are included; with none, the command prints `No enabled restore jobs to run.` and exits `0`.

:::caution `run <job-name>` ignores `enabled`
The enabled state gates `--all` and the scheduler. A single named `sentinel restore run <job-name>` executes the job even when it is `enabled: false`: nothing in the run path or in `ValidateRestoreJob` rejects a disabled job. Treat `enabled: false` as "not scheduled and not swept by `--all`", not as a safety interlock against a manual run.
:::

#### What `run` does

Each job goes through `internal/adapters/restore/runtime.ExecuteRestore`, which validates the job, constructs the domain executor, and runs: acquire the per-job file lock (a held lock yields status `skipped` with reason `lock_conflict`) → apply `timeout_seconds` → stage the artifact from the configured source → plan the restore → verify SHA-256 against the manifest and decrypt → assemble and replay any incremental chain → run the engine restore → optional post-restore verification → record the execution → dispatch notifications.

### Subcommands with no local flags

`list`, `status`, `enable`, `disable`, `pause`, `resume`, `dry-run`, `history`, and `validate-chain` register no flags of their own. Each accepts the persistent flags above plus:

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for that subcommand. |

### What `dry-run` checks

`dry-run` is a configuration-resolution check, not a rehearsal of the restore. It:

- Loads and fully validates the configuration, including cron validation of every enabled, scheduled restore job.
- Fails if the named job is not present in `restores:`.
- Prints the job's type, target database, `backup_source.type`, `backup_source.backup_path`, effective restore mode (`full` when `restore_mode` is unset), and `timeout_seconds`.
- Ends with `NOTE: This is a dry-run. No data will be restored.`

It does **not** connect to the database, contact the storage backend, confirm the artifact exists, verify a hash, or acquire a lock. Use `validate-chain` for incremental lineage and `sentinel backup verify` for artifact integrity.

### What `validate-chain` checks

Rejects the job with `restore job %q is not configured for incremental mode` unless its effective restore mode is `incremental`. It then builds the advanced restore request, reads the manifest at `<backup_path>.manifest.json` (joined with `backup_source.local_path` for relative local paths), plans the restore, and requires plan status `ready`. On success it prints the baseline backup ID, target backup ID, chain depth, and the ordered artifact IDs. No artifact is downloaded and no database is touched.

### `history` output

Columns: `RESTORE`, `DATABASE`, `MODE`, `PLAN`, `STATUS`, `DURATION`, `TIMESTAMP`, `REASON`, `FALLBACK`. `MODE` shows `full` when unset; `PLAN`, `REASON`, and `FALLBACK` show `-` when empty. Timestamps are UTC RFC 3339. Stored statuses are normalised; `completed` prints as `success`, `failure` prints as `failed`. The query returns at most 50 records. With no records it prints `no restore records found`.

## Examples

List every configured restore job and its enabled state:

```bash
sentinel restore list --config sentinel.yaml
```

Each job prints its name, type, schedule, status, and target database. A job you never gave an explicit `enabled:` shows `disabled`.

Resolve one job's parameters without touching anything:

```bash
sentinel restore dry-run postgres_nightly --config sentinel.yaml
```

Prints the source type, backup path, restore mode, and timeout that a real run would use.

Validate an incremental chain before trusting it in a recovery:

```bash
sentinel restore validate-chain postgres_nightly --config sentinel.yaml
```

Prints the baseline, target, depth, and ordered artifact IDs. A non-zero exit means the lineage or planner status is not `ready`.

Run one restore job:

:::danger Destructive
`restore run` writes into the target database named by the job's `database` key, and a job with `conflict_strategy: replace` drops existing objects first. Confirm you have a verified backup with `sentinel backup verify <backup-id>` and that the job points at a scratch or standby instance before continuing.
:::

```bash
sentinel restore run postgres_nightly --config sentinel.yaml
```

Prints `Restore job "postgres_nightly" completed successfully`. Credentials come from the job's `password_env` / `uri_env` keys; there is no password flag.

Keep the staged artifact to debug a failing restore:

```bash
sentinel restore run postgres_nightly --config sentinel.yaml --keep-file
```

The completion line reports the retained staged path instead of deleting it.

Restore from a Google Cloud Storage bucket for this run only:

```bash
sentinel restore run postgres_nightly --config sentinel.yaml \
  --gcs-bucket sentinel-backups \
  --gcs-credentials-file /etc/sentinel/gcs-sa.json
```

`--gcs-bucket` switches the job's source type to `gcs` in memory; the configuration file is unchanged.

Run every enabled restore job, four at a time:

:::danger Destructive
`--all` writes into the target database of every enabled restore job in the configuration. Review `sentinel restore list` first so you know exactly which databases are in scope.
:::

```bash
sentinel restore run --all --parallel 4 --config sentinel.yaml
```

Prints one `OK`/`FAILED` line per job, then `Restore run-all: 4/5 succeeded (concurrency=4)`. Exit is non-zero when any job failed. Without `--parallel`, the limit falls back to `max_concurrent_restores`, which defaults to `1`.

Review the recent restore record for one job:

```bash
sentinel restore history postgres_nightly --config sentinel.yaml
```

Prints up to 50 rows with mode, planning status, outcome, duration, and any fallback decision.

Recover from a last-copy artifact whose hash no longer matches its manifest:

:::danger Unsafe and destructive
`--skip-hash-verify` restores data that failed its integrity check. Use it only for an artifact you have independently verified, or in last-copy disaster recovery, and never against production.
:::

```bash
sentinel restore run postgres_nightly --config sentinel.yaml --skip-hash-verify
```

A `SECURITY WARNING` is written to the logs and to stderr. For encrypted artifacts this silences only the SHA-256 comparison; the AES-256-GCM authentication tag remains an independent gate that this flag does not bypass, so a truly corrupt ciphertext still fails to decrypt.

## Related

- [Restores: how Sentinel puts data back](../../concepts/restore.md)
- [Backups: what Sentinel captures and how](../../concepts/backup.md)
- [Scheduling: how Sentinel runs jobs unattended](../../concepts/schedule.md)
- [Configuration reference](../configuration.md)
- [`sentinel backup`](./backup.md)
- [`sentinel schedule`](./schedule.md)
- [CLI reference index](./index.md)
- [Restoring a PostgreSQL backup](../../tutorials/postgres/restore.md)
- [Point-in-time recovery on PostgreSQL](../../tutorials/postgres/pitr.md)
- [Incremental WAL backups on PostgreSQL](../../tutorials/postgres/incremental-wal.md)

<!-- sources: internal/cli/restore.go, internal/cli/config_resolver.go, internal/cli/legacy_envelope.go, internal/config/restore_types.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go, internal/adapters/restore/runtime/executor.go, internal/adapters/restore/runtime/preflight.go, internal/domain/restore/executor.go -->
