---
title: Storage backends
description: How Sentinel writes artifacts to local disk, S3, GCS, Google Drive or Azure Blob through one interface, and where that interface stops.
sidebar_position: 9
---

A storage backend is where a backup artifact ends up once the dump has finished. Sentinel talks to
every destination through a single five-method Go interface, so the backup pipeline never contains a
branch for "this is a bucket" or "this is a disk". Five implementations exist: local disk, S3 (and
S3-compatible endpoints), Google Cloud Storage, Google Drive, and Azure Blob Storage. They are
selected by one YAML key, `storage.type`.

## Why it exists

Everything Sentinel does around a dump has to happen identically whatever the destination:
compute a SHA-256, optionally encrypt, write a manifest, optionally re-download and re-verify, and
later delete under a retention policy. If each destination brought its own upload path, those five
guarantees would have five implementations, and the weakest one would define the product.

The interface also fixes the ordering problem that remote storage creates. An artifact must be
hashed and encrypted *before* it leaves the host, not after it arrives. Because the backup pipeline
only ever sees an interface, it can redirect the dump into a local staging directory, run the whole
pipeline against a file it owns, and upload last. Adding a sixth backend is one new sub-package plus
one new case in the registry, and it inherits all of that for free.

## How it works

The port is `ports.StorageBackend` in `internal/ports/storage.go`, and it is deliberately small:

| Method | Purpose |
|---|---|
| `Upload(ctx, src, dest)` | Write a local file to the backend |
| `Download(ctx, src, dest)` | Fetch a backend object to a local path |
| `Delete(ctx, path)` | Remove one object, used by retention |
| `List(ctx, prefix)` | Enumerate objects as `StorageObject` values |
| `Exists(ctx, path)` | Presence check without transferring bytes |

`StorageObject` carries `Path`, `SizeBytes`, `LastModified` and `ETag`. That is the whole vocabulary
the rest of Sentinel has for a stored artifact, and it is why "restore the latest match" can be
expressed as a list plus a sort on `LastModified` rather than as backend-specific query syntax.

Status reporting is a second, separate interface, `ports.StatusReporter`, with one method returning a
`RepoStatus` of `Reachable`, `BackupCount`, `TotalSizeBytes`, `LastBackup` and `Error`. All five
concrete backends implement it, but callers type-assert for it rather than assume it. Keeping it out
of `StorageBackend` means a test fake can implement the storage contract without pretending to have a
repository to summarise.

One constructor builds all five: `storage.NewBackend(*BackendParams)` in
`internal/adapters/storage/registry.go`. It accepts exactly the strings `local`, `s3`, `gcs`,
`google-drive` and `azure`; an empty type defaults to `local`, and anything else returns
`unsupported storage type: <value>`. `BackendParams` is the flat union of every field any backend
needs, so the caller fills in the fields relevant to its type and ignores the rest.

### On the backup path

When a job targets anything other than `local`, the dump is redirected to a local staging directory
first. The artifact is compressed, hashed, encrypted and manifested there, and only then are two
objects uploaded: the artifact and its `<artifact>.manifest.json` sidecar. If `verify_after_upload`
is on, the artifact is re-downloaded through the same interface and re-hashed against the manifest.

### On the restore path

Restore does not go through the registry. Staging in
`internal/adapters/restore/runtime/staging.go` switches on `backup_source.type` itself and
constructs the backend directly, and it handles three cases: `local`, `s3` and `gcs`. Any other
value returns `unsupported restore source`.

### Backup and restore support are not symmetric

This is the single most important thing to know before choosing a destination.

| Backend | `storage.type` value | Backup | Restore | Notes |
|---|---|---|---|---|
| Local disk | `local` | Yes | Yes | The default when `type` is empty |
| S3 and S3-compatible | `s3` | Yes | Yes | `s3_bucket_endpoint` covers MinIO and similar |
| Google Cloud Storage | `gcs` | Yes | Yes | Service-account file, or application default credentials when omitted |
| Google Drive | `google-drive` | Yes | **No** | Restore cannot read it; see below |
| Azure Blob | `azure` | Partially | **No** | Config-file only, and not advertised by `--storage`; see below |

:::warning Backups on Google Drive and Azure Blob cannot be restored by a restore job
A restore reads from `local`, `s3` and `gcs` only. Setting `backup_source.type` to anything else is
rejected when the configuration loads:

```
unsupported backup_source.type: google-drive
```

That failure is clean and early, which is the right behaviour. The gap is that
`RestoreBackupSource` still carries `gdrive_folder_id`, `gdrive_sa_file`, `azure_storage_account`,
`azure_storage_key` and `azure_container` fields, so the schema suggests a capability that does not
exist. Tracked as [issue #145](https://github.com/denisakp/sentinel/issues/145).

The practical consequence stands: treat `google-drive` and `azure` as archival destinations only.
If you intend to restore from a backup, keep a copy on `local`, `s3` or `gcs`.
:::

:::warning Azure Blob is reachable from a config file, but not fully wired
Two gaps overlap here. The `AzureConfig` and `AzureAuthConfig` types carry full YAML tags for
`account_name`, `container`, `tier` and an `auth` block offering `managed_identity`,
`connection_string` and `sas_token`, but nothing in the parsed configuration ever reaches them, so
none of those keys can be set from YAML. What works instead is the flat `azure_storage_account`,
`azure_storage_key` and `azure_container` fields on a `storage:` block. Separately,
`sentinel backup --help` advertises `--storage` as `(local, s3, gcs, google-drive)` while the
validator accepts `azure` as well, and there are no `--azure-*` flags to accompany it, so
`--storage azure` passes validation and then fails for want of an account name. Tracked as
[issue #144](https://github.com/denisakp/sentinel/issues/144).
:::

## Configuration

A backup job carries a `storage:` block. The same shape appears under `defaults.storage`, where it
applies to every job that does not override it.

Repeating credentials across jobs is avoided with the top-level `storages:` map. Each entry is a
named `StorageConfig`; a job then references it by name, and any field the job sets locally overlays
the named entry field by field.

```yaml
version: "1.0"

storages:
  prod-s3:
    type: s3
    s3_bucket: acme-backups-prod
    s3_region: eu-west-3
    s3_access_key_id_env: SENTINEL_S3_KEY_ID
    s3_secret_access_key_env: SENTINEL_S3_SECRET
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
      name: prod-s3

  reporting-postgres:
    type: postgres
    host_env: REPORTING_DB_HOST
    username_env: PROD_DB_USER
    password_env: PG_PASSWORD
    database: reporting
    schedule: "30 3 * * *"
    storage:
      name: prod-s3
      s3_bucket: acme-backups-reporting   # overlays just this field
```

Points worth knowing about this block:

- An unknown name fails at load time with `unknown storage reference '<name>'`. It is not silently
  ignored.
- The overlay is per field and only non-empty values win, so a job can change the bucket without
  restating the region or the credential environment variables.
- Named-storage resolution runs over backup jobs only. A restore job's `backup_source` has no `name`
  key, so restore destinations are always spelled out in full.
- Credentials use the `*_env` indirection: `s3_access_key_id_env`, `s3_secret_access_key_env`,
  `azure_storage_account_env`, `azure_storage_key_env`. Every string field in a storage block also
  expands `${VAR}` at load time, which is what the runbooks use for bucket names.
- `local_path` is a directory. Object paths are resolved beneath it, so a `List` on a local backend
  is a directory walk and `Delete` removes a file.

Every key, with its type and default, is in the
[configuration reference](../reference/configuration.md).

## Example

`sentinel storage status` connects to each backend and reports what it finds:

```bash
sentinel storage status --config sentinel.yaml --output text
```

```
Storage Backend Status (2 configured)
─────────────────────────────────────────
  prod-s3              s3           OK
    Backups: 48  Size: 12904.4 MB
    Last backup: 2026-08-05T02:00:11Z
  archive-gcs          gcs          UNREACHABLE
    Error: gcs: failed to initialize client using gcs_credentials_file
```

`--output json` emits the same records with `name`, `type`, `reachable`, `backup_count`,
`total_size_mb`, `last_backup` and `error`, which is the form to use in a monitoring check. Omitting
`--output` falls back to the configuration's `log_format`.

One thing this command does not do is worth stating plainly: **it iterates the `storages:` map and
nothing else.** A job that defines its storage inline, or one that inherits `defaults.storage`, does
not appear here. With no `storages:` block at all the command is not an error, it simply reports:

```
No named storage backends configured.
  Add storage backends under the 'storages:' key in your config.
```

If you want the command to be a real health check, define your backends as named entries and
reference them from the jobs.

## Failure modes

**`unsupported storage type: <value>`.** The registry recognises five strings and nothing else. The
most common cause is `gdrive` or `google_drive` in place of `google-drive`, or `s3://…` where a bare
`s3` was wanted.

**`unsupported restore source: <value>`.** The restore job named a `backup_source.type` outside
`local`, `s3` and `gcs`. This is the asymmetry described above, and it surfaces at run time rather
than at validation time.

**`unknown storage reference '<name>'`.** A job's `storage.name` does not match any key under
`storages:`. Configuration loading stops here, so no job runs.

**An Azure entry in `storage status` always reports unreachable.** The status command builds its
Azure client from the `auth` block, which no YAML can populate today, so the auth type is empty and
client construction fails with `unsupported auth type ""`. The backup path is unaffected: it takes a
different constructor that builds a connection string from `azure_storage_key`, falling back to a
managed identity when the key is empty. Until issue #144 is closed, judge an Azure backend by whether
backups succeed, not by `storage status`.

**A restore reports the object was not found.** Both the artifact and its `.manifest.json` sidecar
are looked up by object path. The manifest is optional and its absence is not an error, but the
artifact's absence is. With `use_latest_match: true` the `backup_path` is treated as a glob and the
newest match by `LastModified` wins; a pattern that matches nothing produces the same not-found
error.

**An encrypted remote backup is refused for the `single` auto-discovery strategy.** That path uploads
directly and cannot be staged locally, so the artifact could not be encrypted before leaving the
host. Sentinel refuses rather than uploading plaintext. Use `strategy: individual`, or local storage.

**Insufficient staging space.** Restore checks free space in `staging_dir` against the size of the
objects it is about to download, and fails before transferring anything. On platforms where the check
is unavailable it is skipped rather than treated as a failure.

## Related

- [How Sentinel fits together](../intro/architecture-overview.md): where the storage port sits among
  the other moving parts.
- [Backup](./backup.md): the pipeline that hashes, encrypts and manifests an artifact before it is
  uploaded.
- [Restore](./restore.md): how `backup_source` is resolved and staged.
- [`sentinel storage` reference](../reference/cli/storage.md): the command and its flags.
- [`sentinel backup` reference](../reference/cli/backup.md): the `--storage` flag and its per-backend
  companions.
- [Configuration reference](../reference/configuration.md): every storage key.
- [Your first PostgreSQL backup](../tutorials/postgres/first-backup.md): the local backend, end to
  end.
- [Check storage backend](../guides/check-storage-backend.md): confirming Sentinel can reach where it writes.
- [Migrate storage backend](../guides/migrate-storage-backend.md): moving artifacts between backends, and the restore-support caveat.

<!-- sources: internal/ports/storage.go, internal/adapters/storage/registry.go, internal/adapters/storage/validation.go, internal/adapters/storage/local/backend.go, internal/adapters/storage/s3/backend.go, internal/adapters/storage/gcs/backend.go, internal/adapters/storage/gdrive/backend.go, internal/adapters/storage/azure/azure.go, internal/adapters/storage/azure/auth.go, internal/adapters/restore/runtime/staging.go, internal/cli/storage_cmd.go, internal/cli/backup.go, internal/cli/backup_factory.go, internal/config/types.go, internal/config/restore_types.go, internal/config/loader.go, internal/config/validator.go, docs/runbooks/check-storage-backend.md, docs/runbooks/migrate-storage-backend.md, docs/runbooks/restore-from-gcs.md -->
