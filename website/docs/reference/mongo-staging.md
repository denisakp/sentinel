---
title: MongoDB staging directories
description: Where mongodump writes before a remote upload, which of the two staging locations applies, and how to clear orphans left by a killed process.
sidebar_position: 4
---

A MongoDB backup bound for a remote storage backend cannot stream straight from `mongodump` to the bucket. Sentinel runs `mongodump` in archive mode against a transient local file, uploads that file, and removes it. Which directory it uses, and who cleans it up, depends on how the backup was started.

## The three paths

| Path | How it is reached | `mongodump` mode | Transient location |
|---|---|---|---|
| Local storage | `storage.type: local`, or `--storage local` | `--out=<dir>` | None. No staging directory is created. |
| Config-driven remote | `sentinel backup --config …` with a remote backend | `--archive=<file>` | A per-run temporary directory named `sentinel-stage-*`, created in the system temporary directory. |
| Flag-driven remote | `sentinel backup --type mongodb --storage s3 …` with no usable configuration | `--archive=<file>` | `<local-root>/.staging/<job-id>/` |

Remote means any backend that is not `local`: `s3`, `gcs`, `google-drive`, or `azure`.

The archive file is named `dump.archive`, or `dump.archive.gz` when the job compresses.

## Config-driven remote backups

This is the path almost every deployment uses, and the one the scheduler takes.

Before the dump runs, the backup factory creates a fresh directory with the pattern `sentinel-stage-*` in the system temporary directory (`$TMPDIR`, or `/tmp` where that is unset) and hands its path to the MongoDB dump adapter. `mongodump` writes `sentinel-stage-XXXXXX/dump.archive[.gz]` and returns the archive's plaintext SHA-256 without uploading anything.

The backup executor then owns the rest: hashing, optional encryption, manifest generation, upload to the real backend, and removal of the staging directory. Cleanup is a deferred `RemoveAll` on the executor's staging path, so it runs on the success path and on every error path.

:::note `.staging/` is not used here
The `<backup_path>/.staging/<job-id>/` layout described in older operator notes belongs to the flag-driven path below. A configuration-driven MongoDB backup to S3, GCS, Google Drive, or Azure stages in the system temporary directory and never creates a `.staging/` directory inside your backup path.
:::

## Flag-driven remote backups

When `sentinel backup` runs with `--type mongodb` and a remote `--storage` value and no configuration file is in play, the dump adapter stages the archive itself.

The local root is resolved in this order:

1. `--local-path`, when given.
2. `$BACKUP_DIRECTORY`, when set.
3. `~/sentinel`.

Note that the local root is resolved even though the destination is remote: for a remote backend the storage handler returns a bucket or folder identifier rather than a filesystem path, so a separate local root is resolved purely to host the staging directory.

The staging directory is then `<local-root>/.staging/<job-id>/`, where `<job-id>` is always generated as `mongo-<unix-nanos>-<6-hex>`, for example `mongo-1780000000123456789-a1b2c3`. A collision on that name is fatal rather than silently reused. The archive is uploaded through the storage backend and the directory is removed by a deferred cleanup on both the success and failure paths.

:::caution This path produces no manifest and no history row
The flag-driven ad-hoc backup bypasses the backup executor entirely, so it writes no `.manifest.json` sidecar, records nothing in the monitor history database, and cannot be encrypted. Use a configuration file for anything you intend to verify or restore later.
:::

## What operators observe

- A short-lived `sentinel-stage-*` directory in the system temporary directory for the duration of a configured remote MongoDB backup, holding one archive file.
- A short-lived `.staging/<job-id>/` directory inside the local root for an ad-hoc remote MongoDB backup.
- Nothing at all for a local-storage backup.
- Disk usage during the run roughly equal to the size of the compressed dump, on the filesystem hosting the staging directory. On a container with a small writable layer or a `tmpfs`-backed `/tmp`, this is the constraint that fails first on a large database.

A staging directory that is still present, and still non-empty, after the process has exited means the process was killed before its deferred cleanup could run: `SIGKILL`, an OOM kill, or a node reboot. A normal failure, including a `mongodump` error or an upload error, still cleans up.

## Clearing orphans

Identify the leftover directory, confirm no Sentinel process is running against it, then remove it.

:::danger Destructive
`rm -rf` on the wrong path removes real backups. Confirm the path is a staging directory, and confirm that no backup is currently running with `pgrep -a sentinel`, before running either command.
:::

For a configured remote backup:

```bash
ls -la "${TMPDIR:-/tmp}"/sentinel-stage-*
rm -rf "${TMPDIR:-/tmp}"/sentinel-stage-XXXXXX
```

For an ad-hoc remote backup, against the local root the run used:

```bash
ls -la /var/backups/sentinel/.staging/
rm -rf /var/backups/sentinel/.staging/mongo-1780000000123456789-a1b2c3
```

Remove only entries that clearly belong to a run that is no longer active. The generated job ID contains the creation time in Unix nanoseconds, which is the quickest way to tell an orphan from a live run.

An orphaned archive is a plaintext database dump. Treat it as sensitive: remove it promptly rather than leaving it for the next reboot to clear.

## MongoDB TLS material is separate

A MongoDB job with client certificates needs `mongodump` to receive a single combined PEM. Sentinel writes that file as `sentinel-mongo-tls-<pid>-*.pem` in the **system temporary directory**, not inside the staging directory, and closes it through a deferred cleanup in the dump adapter. A process-wide cleanup ring closes any still-open material on `SIGINT` or `SIGTERM`, where deferred calls would not run.

Files orphaned by a `SIGKILL` are handled separately: every Sentinel CLI invocation sweeps the system temporary directory at startup and removes material files whose owning process ID is no longer alive. In practice you do not need to clean these up by hand; running any Sentinel command does it.

## Related

- [Storage backends](../concepts/storage-backends.md)
- [Backups: what Sentinel captures and how](../concepts/backup.md)
- [Backup manifests](../concepts/manifest.md)
- [`additional_args` parsing](./additional-args.md)
- [Configuration reference](./configuration.md)
- [`sentinel backup`](./cli/backup.md)
- [`sentinel storage`](./cli/storage.md)

{/* sources: internal/adapters/dump/mongo/staging.go, internal/adapters/dump/mongo/mongo_dump.go, internal/adapters/dump/mongo/builder.go, internal/cli/backup_factory.go, internal/cli/backup.go, internal/cli/root.go, internal/domain/backup/executor.go, internal/adapters/storage/local/folder.go, internal/adapters/mongo_tls/material.go, internal/adapters/mongo_tls/sweep.go, docs/runbooks/mongo-staging.md */}
