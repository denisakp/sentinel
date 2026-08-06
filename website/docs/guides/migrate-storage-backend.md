---
title: Migrating to a different storage backend
description: "Move new backups to another destination safely, and understand which destinations are a one-way trip: restore cannot read Google Drive or Azure."
sidebar_position: 12
---

Point a backup job at a different destination, seed it, and confirm the change without losing access
to what you already have.

## When to use this

Use this when you are moving new backups from local disk to a bucket, between clouds, or into a
cheaper archive tier.

Do not use it as a data migration. Sentinel has no command that copies existing artifacts from one
backend to another; `sentinel storage` has exactly one subcommand, `status`. Changing the
configuration changes where the **next** backup is written. Everything already written stays where
it is, and Sentinel stops managing it.

:::danger Google Drive and Azure Blob are a one-way trip
Backup writes to five backends: `local`, `s3`, `gcs`, `google-drive` and `azure`. Restore reads
three: `local`, `s3` and `gcs`. There is no code path that restores from Google Drive or Azure Blob,
and configuration validation will not let you write a restore job that tries:

```text
Error: invalid config "sentinel.yaml": restore 'pg-from-drive': unsupported backup_source.type: google-drive
```

`RestoreBackupSource` still carries `gdrive_folder_id`, `gdrive_sa_file`, `azure_storage_account`,
`azure_storage_key` and `azure_container` fields, so the keys look supported when you read the type
or the reference. They are not reachable. Migrating a job to `google-drive` or `azure` means that
from that moment on, its backups cannot be restored by Sentinel at all. Recovery would mean
downloading the object with the vendor's own tooling and feeding it to `psql`, `mysql` or
`mongorestore` yourself, decrypting and decompressing by hand if the artifact is encrypted or
compressed. Tracked as [issue #145](https://github.com/denisakp/sentinel/issues/145) and
[issue #144](https://github.com/denisakp/sentinel/issues/144).

If you are moving to either one, keep a parallel job writing to `local`, `s3` or `gcs`. Treat Drive
and Azure as archive copies, never as the restorable copy.
:::

## Before you start

- The new backend proven reachable. Do that first, with
  [Checking a storage backend](./check-storage-backend.md).
- Credentials for the new backend resolvable from the environment, and list, write and delete
  permission on it. Retention needs delete.
- A decision about the old artifacts: whether you will copy them across, leave them, or delete them
  yourself. Sentinel will not do any of the three.
- **Retention run to completion on the old backend, before you switch.** See
  [Retention gets stuck across a migration](#retention-gets-stuck-across-a-migration).
- A restore rehearsal on the current backend, so you know the baseline works before you change
  anything.

## Steps

### 1. Confirm the destination

```bash
sentinel storage status --config sentinel.yaml --output text
```

Reachable, and holding either nothing or a state you recognise.

### 2. Change where the job writes

Prefer a named entry over `defaults.storage`, because named entries overlay field by field and are
the only ones `storage status` can see:

```yaml
version: "1.0"

storages:
  archive-gcs:
    type: gcs
    gcs_bucket: acme-backups-archive
    gcs_project_id: acme-platform
    gcs_credentials_file: /etc/sentinel/gcs-sa.json

databases:
  prod-postgres:
    type: postgres
    host_env: PROD_DB_HOST
    username_env: PROD_DB_USER
    password_env: PG_PASSWORD
    database: myapp_prod
    schedule: "0 2 * * *"
    storage:
      name: archive-gcs
```

:::warning `defaults.storage` is all or nothing
A job inherits `defaults.storage` only when it sets neither `storage.name` nor `storage.type`, and
the inheritance replaces the job's whole `storage:` block rather than merging into it. Two traps
follow. A job with a partial block such as `storage: { local_path: /var/backups }` and no `type`
silently loses `local_path` and takes the default wholesale. A job with `storage: { type: local }`
and nothing else inherits nothing, so it keeps writing to the old destination while everything else
moves. Audit every per-job `storage:` block before you rely on a defaults change, or convert them all
to named references, where the overlay is per field and only non-empty values win.
:::

### 3. Validate

```bash
sentinel config validate --config sentinel.yaml
```

`configuration is valid` is the bar. A typo in `storage.name` fails here with
`unknown storage reference '<name>'`. A typo in the `type` of a named entry does **not** fail here;
see [Checking a storage backend](./check-storage-backend.md).

### 4. Seed the new backend

```bash
sentinel backup --config sentinel.yaml
```

A bare `sentinel backup` with a config runs **every enabled job** in the file, not just the one you
changed. Disable the others first if that is not what you want.

Two objects should appear per backup: the artifact and its `<artifact>.manifest.json` sidecar.

:::warning Copy the sidecars if you copy artifacts by hand
If you move old artifacts across with the vendor's CLI, move the matching `.manifest.json` files with
them. Restore only verifies, decrypts and decompresses when the sidecar is staged alongside the
artifact. An encrypted or compressed artifact separated from its sidecar is fed to the restore tool
as raw bytes and fails with a parse error rather than a useful message.
:::

### 5. Confirm the artifact reference changed

```bash
sentinel monitor list --config sentinel.yaml --last 1h --format json
```

The table view has no path column; use `--format json` or `sentinel monitor show <backup-id>`. Only
GCS records a URI (`gs://bucket/object`); `local` records an absolute path, and `s3`, `azure` and
`google-drive` record the bare object name with no scheme and no bucket.

### 6. Restart the scheduler

```bash
sentinel schedule start --config sentinel.yaml
```

Or restart whatever supervises it. The scheduler reads the configuration at startup, so a running
process keeps writing to the old backend until it is restarted.

## Verify

Prove three things, in this order.

**New backups land.** `storage status` shows the object count on the new backend rising, and
`monitor list` shows the new run as `success`.

**The new artifact is intact.**

```bash
sentinel backup verify <backup-id> --config sentinel.yaml
```

Remote verification downloads the artifact and its sidecar to a temporary directory and re-hashes
them, and it works for all five backends, Google Drive and Azure included. Being able to verify an
artifact on Drive or Azure does not make it restorable; see the warning at the top.

**The new artifact restores.** This is the only check that matters, and it is only available on
`local`, `s3` and `gcs`. Point a restore job at the new backend and run it against a throwaway
database:

```bash
sentinel restore run <restore-job> --config sentinel.yaml
```

[Restoring from Google Cloud Storage](./restore-from-gcs.md) walks through one such job end to end.

## If it goes wrong

### Retention gets stuck across a migration

Retention reads candidates from the execution history, which still holds rows written against the old
backend, and then deletes them through the job's **current** storage configuration. The old rows do
not parse against the new backend and produce an error each run, for example
`failed to parse gcs uri demo.sql: missing container/bucket for non-canonical path`.

That error is not confined to the old rows. When any candidate fails, the history delete is skipped
for **all** of them, so the new artifacts are deleted from the bucket while their history rows
survive and come back as candidates on the next run. Retention never converges, and the history grows
without bound.

Run retention to completion on the old backend before you migrate, so that no old rows remain, and
preview afterwards to confirm:

```bash
sentinel retention preview --config sentinel.yaml --job prod-postgres
```

With `--job`, `sentinel retention apply` exits non-zero on this error. Without `--job` it prints
`retention completed with errors` to stderr and still exits `0`, so a cron wrapper will not notice.

### Retention cannot delete from Google Drive at all

`google-drive` has no delete branch: `sentinel retention apply` fails with
`retention delete not supported for storage type 'google-drive'`. Worse, `sentinel retention preview`
returns before it ever touches a backend, so the preview lists candidates and gives no hint that
applying them is impossible. Migrating to Google Drive means retention stops working and the
repository grows forever. Tracked as
[issue #169](https://github.com/denisakp/sentinel/issues/169).

### `sentinel repair` reports every old backup as missing

`sentinel repair` reconciles the history against the job's current repository. After a migration
every pre-migration row is classified `artifact_missing`, and that class alone makes the command exit
non-zero, so a CI gate on `sentinel repair` will fail until those rows age out. Nothing removes them
automatically. Run it with `--dry-run` first and read the report before treating the exit code as a
regression.

### `sentinel backup verify --all` fails on the old backups

Verification builds backend parameters from the job's current storage configuration but uses the
recorded backend type from each row. After an S3 to GCS migration the old rows are still typed `s3`
while the config now carries GCS keys, so the S3 bucket is empty and verification fails with
`failed to parse s3 path "…": missing container/bucket for non-canonical path`. Scope the sweep to
the period since the migration:

```bash
sentinel backup verify --all --since 7d --config sentinel.yaml
```

### The new backend is unreachable and the job silently keeps working

It does not. A backup to an unreachable backend fails the run and is recorded as a failure. What is
silent is the reverse: `storage status` reporting `OK` proves the backend answered, not that the job
uses it. Check the artifact reference in the history, not the status output.

### An Azure entry reports unreachable but backups succeed

Expected. `storage status` and the backup path build their Azure clients differently, and only the
backup path works from YAML today ([issue #144](https://github.com/denisakp/sentinel/issues/144)).

## Rolling back

Restore the previous `storage:` block, validate, and restart the scheduler. New backups resume on the
old backend immediately. What you seeded on the new backend stays there under your control, and the
history rows written while the new backend was active carry the same retention and verification
caveats in reverse.

## Related

- [Storage backends](../concepts/storage-backends.md): the five backends and the backup/restore
  asymmetry in full.
- [Checking a storage backend](./check-storage-backend.md): proving the destination answers first.
- [Restoring from Google Cloud Storage](./restore-from-gcs.md): the restore side of a bucket.
- [Restore](../concepts/restore.md): how `backup_source` is resolved and staged.
- [Retention](../concepts/retention.md): what retention deletes and when.
- [Applying retention](./apply-retention.md): running it, and reading the preview.
- [Verifying backup integrity](./verify-backup-integrity.md): single-artifact verification.
- [Repository-wide integrity sweeps](./integrity-sweep.md): `--all`, `--since` and the exit codes.
- [Configuration reference](../reference/configuration.md): every storage key.
- [`sentinel backup`](../reference/cli/backup.md): the `--storage` flag and its per-backend
  companions.

{/* sources: internal/adapters/storage/registry.go, internal/adapters/restore/runtime/staging.go, internal/config/restore_types.go, internal/config/loader.go, internal/config/validator.go, internal/config/types.go, internal/cli/retention_cleaner.go, internal/cli/retention_helpers.go, internal/cli/backup_verify.go, internal/cli/backup.go, internal/cli/repair.go, internal/cli/storage_cmd.go, internal/domain/backup/pipeline.go, internal/adapters/restore/runtime/executor.go, docs/runbooks/migrate-storage-backend.md */}
