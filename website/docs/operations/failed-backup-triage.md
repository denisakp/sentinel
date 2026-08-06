---
title: Triaging a failed backup
description: "Find the failing job, read the full error, classify it, and re-run: the fast path from a failure alert to a green history row."
sidebar_position: 5
---

A backup job reported `failure`. This page gets you from the alert to a `success` row.

## Symptoms

A row with status `failure` in the execution history:

```text
ID                                    JOB   TYPE  CHAIN  STATUS   TIMESTAMP            DURATION  DELTA  ERROR
a331ed47-4abc-480c-a2e2-357f2083f292  shop  full  -      failure  2026-08-05 21:47:02  2ms       -      failed to ping database - failed to ping data...
```

Or a notification naming the job, or a non-zero exit from a `sentinel backup --config` run, whose last line is the same error the history row holds:

```text
Error: failed to ping database - failed to ping database: dial tcp 127.0.0.1:5432: connect: connection refused
```

Three statuses are worth knowing apart. `failure` is a job that ran and failed. `interrupted` is a job whose process died mid-run and was reconciled afterwards. `running` on a job that finished hours ago is drift, not a failure, and belongs in [Repairing repository state drift](./state-repair.md).

:::note Backup jobs never report `skipped` or `timeout`
Those two statuses exist, but only the restore executor writes them, into the separate restore history. Backups take no job lock on any code path today, so lock contention cannot skip a backup. See [Recovering from a stale lock](./stale-lock-recovery.md) for where locks do apply.
:::

## Before you start

- `history_db_path` must be set in your configuration and the file readable. Without it there is nothing to triage from.
- Shell access to the host that ran the job, so you can inspect the environment variables the job resolves credentials from.
- Capture the execution ID and the full error **before** changing anything. Once you re-run the job, a fixed `output:` artifact is overwritten.

:::caution The monitor commands print to stderr
`monitor list`, `show`, `stats` and `export` write their output to stderr, so `sentinel monitor list … > triage.txt` produces an empty file. Use `2>&1 | tee triage.txt` to capture, or `sentinel monitor export --output triage.json`, which writes the file itself and does work. Tracked as issue #165.
:::

## Resolution

### 1. Enumerate the failures

```bash
sentinel monitor list --config sentinel.yaml --status failure --last 24h 2>&1 | tee triage.txt
```

Note the `ID` and `JOB` columns. The `ERROR` column is truncated with an ellipsis, so do not classify from it.

`--last` accepts `7d`, `24h`, `30min`, and Go durations such as `90m`. `--status` matches the stored value exactly: use `failure`, `interrupted`, or `running`.

### 2. Read the full error

```bash
sentinel monitor show --config sentinel.yaml --id a331ed47-4abc-480c-a2e2-357f2083f292
```

```text
Execution Details
=================

ID: a331ed47-4abc-480c-a2e2-357f2083f292
Backup Job: shop
Database Type: postgres
Started: 2026-08-05 21:47:02
Duration: 2ms

Status: failure
Backup Type: full
File: ./backups/shop.sql
Size: 0
Error: failed to ping database - failed to ping database: dial tcp 127.0.0.1:5432: connect: connection refused
```

`Size: 0` with a `File:` path means the artifact path was reserved and nothing was written. That is normal for a failure before the dump stage.

### 3. Classify

Match the `Error:` line against the leading fragment, not the whole string.

| Error fragment | Category | Go to |
|---|---|---|
| `failed to ping database - failed to ping database: dial tcp …: connect: connection refused` | The database is unreachable from this host | [Troubleshooting common errors](./troubleshooting.md) |
| `failed to execute pg_dump command - exec: "pg_dump": executable file not found in $PATH` | The engine client binary is missing | [Environment setup](../guides/environment-setup.md) |
| `failed to execute pg_dump command - exit status 1, <stderr>` | The dump tool ran and refused. The redacted stderr is already in the message | [Additional arguments](../reference/additional-args.md) |
| `environment variable '<NAME>' is not set` | Credential resolution, raised at config load before any job runs | [Database credentials](../guides/database-credentials.md) |
| `s3: failed to upload …`, `error while uploading object to <bucket>` | Storage backend | [Check a storage backend](../guides/check-storage-backend.md) |
| `file corrupt or wrong key` | Wrong key, a legacy envelope, or a truncated artifact | [Recovering a pre-v2 envelope](./recover-legacy-envelope.md) |
| `chain=… : rule_4_non_contiguous_chain_index` and the other `rule_*` codes | Incremental lineage | [Recovering a broken incremental chain](./chain-corruption-recovery.md) |
| `context deadline exceeded` | The job exceeded `scheduler.job_timeout_minutes` (default 180) | Raise the value, then investigate source load |

There is no per-job backup timeout key. The only backup timeout is the scheduler-wide `scheduler.job_timeout_minutes`; `timeout_seconds` exists on restore jobs and on notification channels, not on backup jobs.

### 4. Apply the fix, then re-run the job

:::danger Re-running overwrites the previous artifact
A job with a fixed `output:` writes to the same path every time, and the write truncates. Re-running a failed job destroys the last good artifact at that path. Scheduled runs are safe, because the scheduler inserts a timestamp into the name; a manual `sentinel backup --config` run is not.

Copy the existing artifact and its `.manifest.json` sidecar aside first, or verify it is already worthless with `sentinel backup verify <id> --config sentinel.yaml`.
:::

`sentinel backup` has no `--job` flag: `--config` runs **every enabled job** in the file. To re-run one job only, disable the others for the duration of the run:

```yaml
databases:
  shop:
    enabled: true
    # ...
  analytics:
    enabled: false      # temporarily
```

```bash
sentinel backup --config sentinel.yaml
```

A trimmed copy of the configuration containing only the failing job works equally well and does not risk leaving `enabled: false` behind. Whichever you choose, keep `history_db_path` pointing at the same database so the replay lands in the same history.

:::caution `sentinel backup force-full --job <name>` cannot be invoked
It looks like the per-job runner, but the subcommand never registers `--config` while still requiring it, so it fails with `unknown flag: --config` when you pass it and `--config is required` when you do not. Tracked as issue #136.
:::

If the failure needs no configuration change at all and you only want to confirm connectivity, a flag-only run is the cheapest probe:

```bash
sentinel backup --type postgres --host 127.0.0.1 --port 5432 \
  --user backup --database shop --password-env PGPASSWORD \
  --local-path ./scratch --output probe.sql
```

That path writes **no history row, no manifest, and no encryption**. Treat its artifact as a diagnostic, never as a backup.

### 5. Replay several jobs

If a group of jobs failed for one shared reason, fix the cause and run the config once. Every enabled job re-runs, which is what you want here, and the same overwrite warning applies to each of them.

## Verify recovery

```bash
sentinel monitor list --config sentinel.yaml --last 15m 2>&1 | tee verify.txt
```

Each replayed job should show a `success` row with a non-zero `DURATION`. Then confirm the artifact is not just present but intact:

```bash
sentinel backup verify <new-execution-id> --config sentinel.yaml
echo "exit=$?"
```

Exit `0` is a hash match. Exit `3` is `missing_manifest`, which almost always means the job has no `output:` set: the dump engine invents a filename that never reaches the manifest writer, so no manifest is created and the history records the artifact as `unknown`. Set `output:` and re-run. Tracked as issue #151.

:::caution A verified encrypted artifact is not a proven restorable one
`backup verify` hashes the bytes as stored and never decrypts, so it proves the ciphertext is unchanged, not that your key still opens it. `--allow-legacy-envelope` is accepted here and has no effect. Prove the key separately, per [Encryption and key management](../concepts/security-encryption.md). Tracked as issue #164.
:::

Finally, close the loop on the history itself:

```bash
sentinel repair --config sentinel.yaml --dry-run
```

`No drift detected.` means no stale `running` rows or orphaned artifacts were left behind by the failure.

## Prevent recurrence

- **Set `output:` on every job.** It is the only way to get a manifest, and without a manifest nothing about the artifact can be verified later.
- **Send failures somewhere.** Configure a channel so triage starts from an alert rather than from a routine `monitor list`. See [Notifications](../concepts/notifications.md) and [Alerting setup](../guides/alerting-setup.md).
- **Enable `integrity.verify_after_upload`** so an artifact that was corrupted in transit fails the backup instead of being recorded as a success.
- **Prefer the scheduler for production runs.** Retries (3 attempts, 1s/2s/4s backoff) apply only under `sentinel schedule start`. A manual `sentinel backup --config` run makes exactly one attempt.
- **Sweep periodically**, so a silent corruption is found on your schedule rather than during a recovery: `sentinel backup verify --all --since 30d --config sentinel.yaml`.

## Related

- [Inspect monitor history](../guides/inspect-monitor-history.md): the query surface used throughout this page.
- [Monitoring and execution history](../concepts/monitoring-history.md): what a history row records and why.
- [Troubleshooting common errors](./troubleshooting.md): the per-error index for step 3.
- [Recovering from a stale lock](./stale-lock-recovery.md): the lock class of failure.
- [Recovering a broken incremental chain](./chain-corruption-recovery.md): the lineage class of failure.
- [Repairing repository state drift](./state-repair.md): stale `running` rows, orphan artifacts, and orphan manifests.
- [Verify backup integrity](../guides/verify-backup-integrity.md): step 5 in full.
- [`sentinel monitor` reference](../reference/cli/monitor.md) and [`sentinel backup` reference](../reference/cli/backup.md): every flag named above.
- [Configuration reference](../reference/configuration.md): `output`, `enabled`, `history_db_path`, `scheduler.job_timeout_minutes`.

<!-- sources: internal/cli/monitor.go, internal/cli/backup.go, internal/cli/backup_factory.go, internal/cli/backup_verify.go, internal/cli/exit_codes.go, internal/cli/repair.go, internal/domain/backup/executor.go, internal/domain/backup/planner.go, internal/ports/recorder.go, internal/ports/lock.go, internal/scheduler/backup_job_helpers.go, internal/scheduler/executor.go, internal/adapters/dump/pg/pg_dump.go, internal/adapters/dump/mysql/mysql_dump.go, internal/adapters/storage/s3/s3.go, internal/adapters/storage/local/local.go, internal/utils/file.go, internal/config/types.go, docs/runbooks/failed-backup-triage.md -->
