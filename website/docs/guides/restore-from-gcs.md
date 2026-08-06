---
title: Restoring from Google Cloud Storage
description: "Configure and run a restore job whose artifact lives in a GCS bucket: staging, credentials, pattern matching and the failure modes."
sidebar_position: 13
---

Restore a database from a backup artifact stored in Google Cloud Storage, staging the object locally
first.

## When to use this

Use this when the artifact you need is a GCS object rather than a file on the machine running
Sentinel: a real recovery, a scheduled restore rehearsal, or a CI gate proving last night's backup is
restorable.

Do not use it for Google Drive or Azure Blob. Those are backup destinations only; a restore job
naming them is rejected at configuration load with `unsupported backup_source.type: <value>`. GCS,
S3 and local disk are the three sources a restore job can read.

Do not use it to restore several databases at once either. A restore job targets exactly one
database. To run many jobs together, see
[Running restore jobs in parallel](./parallel-restore.md).

## Before you start

- **A service account with read access to the object**, specifically `storage.objects.get` and
  `storage.objects.list`. Listing is not optional: Sentinel resolves the object's size from a bucket
  listing before it downloads anything, so a key that can read a single object but not list the
  bucket fails.
- **A key file readable by the Sentinel process**, or application default credentials in the
  environment. Omitting `gcs_credentials_file` falls back to application default credentials.
- **A staging directory with room for the artifact.** Set `restore.staging_dir` globally or
  `staging_dir` on the job; validation requires one of the two. Sentinel checks free space against
  the object size and refuses before transferring anything.
- **A writable `scheduler.lock_dir`.** It defaults to `/var/run/sentinel`, which a non-root operator
  usually cannot create.
- **A target database you can afford to overwrite.**
- Never a credential on a command line or inline in YAML. Use `password_env`, and a key file path for
  GCS.

## Steps

### 1. Define the restore job

```yaml
version: "1.0"

restore:
  staging_dir: /var/lib/sentinel/staging

scheduler:
  lock_dir: /var/lib/sentinel/locks

restores:
  pg-gcs-restore:
    type: postgres
    enabled: true
    host: 127.0.0.1
    port: 5432
    username: sentinel
    password_env: SENTINEL_PGPASSWORD
    database: myapp_restore_check
    schedule: "0 3 * * 0"
    keep_file: false
    backup_source:
      type: gcs
      gcs_bucket: ${SENTINEL_GCS_BUCKET}
      gcs_project_id: ${SENTINEL_GCP_PROJECT}
      gcs_credentials_file: /etc/sentinel/gcs-sa.json
      backup_path: myapp_prod_2026-08-05T02-00-00.sql
```

`${VAR}` is expanded at load time. `schedule` is required once a restore job is enabled, so enabling
a job for on-demand use also registers it with the scheduler; there is no manual-only mode.

To take the newest object matching a pattern instead of a fixed name:

```yaml
    backup_source:
      type: gcs
      gcs_bucket: ${SENTINEL_GCS_BUCKET}
      backup_path: "myapp_prod_*.sql"
      use_latest_match: true
```

Matching is done with Go's `filepath.Match` against the full object name, and `*` does not cross a
`/`. A pattern of `*.sql` will not match `nightly/myapp.sql`; write `nightly/*.sql`. The newest
match by last-modified time wins.

### 2. Validate

```bash
sentinel config validate --config sentinel.yaml
```

### 3. Check the wiring

```bash
sentinel restore dry-run pg-gcs-restore --config sentinel.yaml
```

```text
Dry-run: Job "pg-gcs-restore"
  Type: postgres
  Database: myapp_restore_check
  Backup Source Type: gcs
  Backup Path: myapp_prod_2026-08-05T02-00-00.sql
  Restore Mode: full
  Timeout: 0 seconds

NOTE: This is a dry-run. No data will be restored.
```

Dry-run echoes the configuration and nothing more. It does not contact GCS and does not contact the
database, so it proves the job is wired, never that the object exists.

### 4. Run it

:::danger Destructive
This writes into the database named by `database:`, and with `conflict_strategy: replace` it can drop
objects there first. Confirm the artifact with
[`sentinel backup verify`](./verify-backup-integrity.md) and point the job at a database you can
afford to lose.
:::

```bash
sentinel restore run pg-gcs-restore --config sentinel.yaml
```

Sentinel downloads the object into `staging_dir` under a unique name, downloads the optional
`<object>.manifest.json` sidecar beside it, verifies the SHA-256 against the manifest, decrypts and
decompresses as the manifest dictates, restores from the resulting file, and deletes the staged files
unless you asked it not to. Staged files are created `0600` inside a `0700` directory.

To keep the staged artifact for inspection:

```bash
sentinel restore run pg-gcs-restore --config sentinel.yaml --keep-file
```

`--keep-file` is equivalent to `keep_file: true` on the job, or `restore.keep_file: true` globally.

Three flags can override the source for one invocation: `--gcs-bucket`, `--gcs-project-id` and
`--gcs-credentials-file`. Note that passing `--gcs-bucket` also forces `backup_source.type` to `gcs`,
so it will silently redirect a job whose source is local or S3.

## Verify

The command prints its outcome and exits non-zero on failure. Then read the recorded history:

```bash
sentinel restore history pg-gcs-restore --config sentinel.yaml
```

```text
RESTORE | DATABASE | MODE | PLAN | STATUS | DURATION | TIMESTAMP | REASON | FALLBACK
pg-gcs-restore | myapp_restore_check | full | - | success | 12s | 2026-08-05T18:56:17Z | - | -
```

Then check the data itself. Connect to the target and count rows in a table you know the size of.
Sentinel does not do this for you: `verify_after_restore: true` cannot succeed today, because no
verification handler is wired into the execution path, and the job fails with
`verification handler is required for restore mode "full"` **after** the data has already been
written ([issue #149](https://github.com/denisakp/sentinel/issues/149)). Leave it unset.

Finally, confirm the staging directory is clean unless you passed `--keep-file`.

## If it goes wrong

**`failed to initialize gcs restore backend: gcs: failed to initialize client using gcs_credentials_file`.**
Client construction failed. The message is the same whether the path does not exist, the file is not
valid JSON, or the key has been revoked, because the underlying error is discarded. Check the path
exists and is readable by the Sentinel user, then validate the key with `gcloud` before looking
further. With no `gcs_credentials_file` set, the same failure reads
`gcs: failed to initialize client using application default credentials`.

**`restore source object not found: <object>`.** The object key is wrong, or the service account
cannot list the bucket. `backup_path` is the object name inside `gcs_bucket`, not a `gs://` URL and
not a local path. With `use_latest_match: true`, a pattern matching nothing produces the same error.

**`failed to acquire restore lock: mkdir "/var/run/sentinel": permission denied`.** Set
`scheduler.lock_dir` to a directory your account owns.

**`insufficient staging space`.** The preflight compares free space in `staging_dir` against the
object size and stops before downloading. Free space, or point `staging_dir` at a larger volume. On
platforms where the check is unavailable it is skipped rather than treated as a failure.

**`integrity_check_failed`.** The staged artifact's SHA-256 does not match its manifest. Do not
reach for `--skip-hash-verify` first; it downgrades the abort to a warning and restores bytes you
have just been told are wrong. Try another artifact, and only use the flag for a last-copy recovery
you have independently checked.

**The restore ran but the target is full of binary garbage, or `psql` reported a syntax error.** The
manifest sidecar was missing, so the verify, decrypt and decompress stages were all skipped and the
raw stored bytes went to the restore tool. Confirm that `<object>.manifest.json` exists next to the
artifact in the bucket. A missing sidecar is deliberately not an error, which is what makes this
failure look like a corrupt backup rather than a missing file.

**The job is disabled and ran anyway.** `sentinel restore run <job-name>` does not consult `enabled`;
that flag governs only `--all` and the scheduler
([issue #139](https://github.com/denisakp/sentinel/issues/139)). The `restore enable`, `disable`,
`pause` and `resume` subcommands print success and change nothing
([issue #137](https://github.com/denisakp/sentinel/issues/137)); only the YAML has any effect.

## Related

- [Restore](../concepts/restore.md): the full sequence a job goes through, and every reason code.
- [Storage backends](../concepts/storage-backends.md): which backends restore can read, and which it
  cannot.
- [Backup manifests](../concepts/manifest.md): what the `.manifest.json` sidecar carries and why
  restore needs it.
- [Encryption at rest](../concepts/security-encryption.md): what happens when the artifact is
  encrypted.
- [Compressing backup artifacts](./backup-compression.md): the decompress stage restore inserts
  automatically.
- [Checking a storage backend](./check-storage-backend.md): proving the bucket answers first.
- [Running restore jobs in parallel](./parallel-restore.md): the same job, fanned out.
- [Verifying backup integrity](./verify-backup-integrity.md): checking an artifact before you restore
  it.
- [`sentinel restore`](../reference/cli/restore.md): every subcommand and flag.
- [Configuration reference](../reference/configuration.md): every `backup_source` key.

{/* sources: internal/adapters/restore/runtime/staging.go, internal/adapters/restore/runtime/executor.go, internal/adapters/storage/gcs/backend.go, internal/cli/restore.go, internal/config/restore_types.go, internal/config/types.go, internal/config/validator.go, internal/domain/restore/executor.go, docs/runbooks/restore-from-gcs.md */}
