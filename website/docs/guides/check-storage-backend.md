---
title: Checking a storage backend
description: "Confirm a storage backend is reachable before you trust it, and understand what `sentinel storage status` does not check."
sidebar_position: 11
---

Confirm that a configured storage backend answers, holds what you expect, and is spelled correctly,
before a scheduled job depends on it.

## When to use this

Use this as a pre-flight before you point a new job at a backend, as a post-deployment check after
credentials rotate, and as the first step of triage when artifacts are missing.

Do not use it as a health check for a backend that a job configures inline. `sentinel storage status`
reads the top-level `storages:` map and nothing else. A job with its own `storage:` block, or one
inheriting `defaults.storage`, is invisible to this command however healthy or broken it is. If you
want a real health check, define every backend as a named entry and have the jobs reference it by
name.

Do not use it as an alarm either. It exits `0` whether every backend answered or none of them did.
Read the output, or parse the JSON.

## Before you start

- A configuration file. Without `--config` the command looks for `./sentinel-config.yaml` in the
  working directory.
- **The whole file has to be valid.** `storage status` loads and validates the entire configuration
  first, so an unset password environment variable on an unrelated backup job stops the check before
  a single backend is contacted:

  ```text
  Error: failed to load config: failed to load config "sentinel.yaml": backup 'demo': environment variable 'DEMO_PG_PASSWORD' is not set
  ```

  Export the variables your jobs reference first; see [Environment setup](./environment-setup.md) and
  [Database credentials](./database-credentials.md).
- Credentials for each backend resolvable from the environment, and network egress to it.
- The backends declared under `storages:`. Entries under `defaults.storage` or inside a job are not
  reported.

## Steps

### 1. Declare the backends as named entries

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
  nearline-local:
    type: local
    local_path: /var/backups/sentinel

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
```

Credentials go through the `*_env` indirection, never inline. Every string in a storage block also
expands `${VAR}` at load time.

### 2. Validate the configuration

```bash
sentinel config validate --config sentinel.yaml
```

You should see `configuration is valid`. An unknown reference stops the load here with
`unknown storage reference '<name>'`, so a typo in `storage.name` can never reach a scheduled run.

:::warning A named entry's `type` is never checked
Validation checks `storage.type` on backup **jobs**, but entries under `storages:` are not type
checked at all. A `type: gdrive` entry (the correct spelling is `google-drive`) passes
`config validate` with `configuration is valid` and only surfaces later, as
`unsupported storage type: gdrive` in the status output, or at run time for a job that references it.
Tracked as [issue #161](https://github.com/denisakp/sentinel/issues/161) and
[issue #170](https://github.com/denisakp/sentinel/issues/170).
:::

### 3. Query the backends

```bash
sentinel storage status --config sentinel.yaml --output text
```

```text
Storage Backend Status (3 configured)
─────────────────────────────────────────
  prod-s3              s3           OK
    Backups: 96  Size: 12904.4 MB
    Last backup: 2026-08-05T02:00:11Z
  nearline-local       local        OK
    Backups: 12  Size: 480.2 MB
    Last backup: 2026-08-05T02:00:09Z
  archive-gcs          gcs          UNREACHABLE
    Error: gcs: failed to initialize client using gcs_credentials_file
```

Use JSON for anything automated:

```bash
sentinel storage status --config sentinel.yaml --output json
```

```json
[
  {
    "name": "nearline-local",
    "type": "local",
    "reachable": true,
    "backup_count": 3,
    "total_size_mb": 0.00000286102294921875,
    "last_backup": "2026-08-05T18:53:26Z"
  },
  {
    "name": "cold-azure",
    "type": "azure",
    "reachable": false,
    "backup_count": 0,
    "total_size_mb": 0,
    "error": "azure: unsupported auth type \"\" (must be managed_identity, connection_string, or sas_token)"
  }
]
```

Only the exact string `json` selects JSON. Any other value, including `yaml`, falls through to the
text renderer without complaint. Omitting `--output` falls back to the configuration's `log_format`,
so a file with `log_format: json` produces JSON from a bare `sentinel storage status`.

### 4. Confirm a backup actually lands

Reachability is not the same as a working job. Run one backup and read the history:

```bash
sentinel backup --config sentinel.yaml
sentinel monitor list --config sentinel.yaml --last 1h
```

Two objects per backup should appear on the backend: the artifact and its `<artifact>.manifest.json`
sidecar. The sidecar carries the integrity hash and the encryption and compression metadata. Without
it, `sentinel backup verify` reports the backup as skipped rather than verified, and a restore skips
its hash check, decryption and decompression entirely.

## Verify

For each named entry, `reachable` is `true` and `backup_count` is what you expect.

The recorded artifact reference is worth checking too, but not with the table view: `monitor list`
prints `ID`, `JOB`, `TYPE`, `CHAIN`, `STATUS`, `TIMESTAMP`, `DURATION`, `DELTA` and `ERROR`, and no
path column at all. Use `--format json`, or read one record:

```bash
sentinel monitor show <backup-id> --config sentinel.yaml
```

What appears on the `File:` line depends on the backend, and only one backend records a URI:

| `storage.type` | Recorded artifact reference |
|---|---|
| `local` | Absolute path on disk |
| `gcs` | `gs://bucket/object` |
| `s3` | The bare object key, with no `s3://` prefix and no bucket |
| `azure` | The bare blob name |
| `google-drive` | The bare file name |

Do not expect `s3://…`, an `https://<account>.blob.core.windows.net/…` URL, or a `gdrive://…`
reference. The code emits a `gs://` URL for GCS and the plain output name for every other remote
backend. Retention and verification both cope with the bare key by falling back to the bucket or
container in the job's storage config, which is exactly why the bare key survives.

## If it goes wrong

**`unsupported storage type: <value>`.** Five strings are recognised: `local`, `s3`, `gcs`,
`google-drive`, `azure`. The usual causes are `gdrive` or `google_drive` instead of `google-drive`, or
an `s3://…` URL where a bare `s3` belongs.

**An Azure entry is always `UNREACHABLE`.** The status command builds its Azure client from an `auth`
block that no YAML key can populate, so the auth type is empty and construction fails with
`azure: unsupported auth type "" (must be managed_identity, connection_string, or sas_token)`. The
backup path is unaffected: it uses a different constructor built from `azure_storage_key`. Until
[issue #144](https://github.com/denisakp/sentinel/issues/144) is closed, judge an Azure backend by
whether backups succeed, not by this command.

**`lstat : no such file or directory`.** A `type: local` entry with no `local_path`. Nothing requires
that key, so the failure appears here as an `lstat` on the empty string rather than as a validation
error. A missing directory produces the clearer `lstat ./does-not-exist: no such file or directory`.

**`backup_count` is higher than the number of backups.** The count is every object the backend
returns, so each `.manifest.json` sidecar counts as one, as does any unrelated file in the bucket or
directory. A repository holding 48 backups reports 96. Treat the number as an object count that
should grow steadily, not as a backup count.

**A backend you configured is missing from the output.** It is defined inline on a job or under
`defaults.storage`. Move it into `storages:` and reference it by name. With no `storages:` block at
all the command prints `No named storage backends configured.` and exits `0`.

**The rows come back in a different order every run.** The command iterates a map without sorting, so
both the text and the JSON row order varies between invocations. Sort by `name` before diffing two
runs.

**The command exited `0` and something was still broken.** It always does. In a monitoring check,
parse the JSON and assert on `reachable`, and treat an `error` field as a failure.

## Related

- [Storage backends](../concepts/storage-backends.md): the five backends, the port they share, and
  where backup and restore support diverge.
- [`sentinel storage`](../reference/cli/storage.md): the command and its flags.
- [Configuration reference](../reference/configuration.md): every storage key.
- [Migrating to a different storage backend](./migrate-storage-backend.md): what to check before you
  move.
- [Restoring from Google Cloud Storage](./restore-from-gcs.md): the restore side of a bucket.
- [Verifying backup integrity](./verify-backup-integrity.md): proving an artifact is intact, remote
  backends included.
- [Inspecting execution history](./inspect-monitor-history.md): reading what a run recorded.
- [Applying retention](./apply-retention.md): what deletes from these backends, and where it cannot.

{/* sources: internal/cli/storage_cmd.go, internal/cli/monitor.go, internal/adapters/storage/registry.go, internal/adapters/storage/validation.go, internal/adapters/storage/local/backend.go, internal/adapters/storage/gcs/backend.go, internal/adapters/storage/azure/auth.go, internal/domain/backup/pipeline.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go, internal/ports/storage.go, internal/ports/recorder.go, docs/runbooks/check-storage-backend.md */}
