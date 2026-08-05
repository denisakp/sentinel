# Runbook — Restore from GCS

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [restore-from-backup](./restore-from-backup.md), [check-storage-backend](./check-storage-backend.md)

## When to use

Restore where the backup artifact lives in Google Cloud Storage rather than locally.

## Preconditions

- `SENTINEL_GCS_BUCKET`, `SENTINEL_GCP_PROJECT`, `SENTINEL_GCS_SA_FILE` exported.
- Service account has `storage.objects.get` on the target object.
- Local disk has space for the staged download (size of the source object).

## Steps

### Define the job

```yaml
restores:
  pg-gcs-restore:
    type: postgres
    enabled: true
    host: host.docker.internal
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel_restore
    schedule: "0 3 * * 0"
    keep_file: false
    backup_source:
      type: gcs
      gcs_bucket: ${SENTINEL_GCS_BUCKET}
      gcs_project_id: ${SENTINEL_GCP_PROJECT}
      gcs_credentials_file: ${SENTINEL_GCS_SA_FILE}
      backup_path: postgres-dev_2026-03-15T02-00-00.sql
```

### Run on demand

```bash
sentinel restore run pg-gcs-restore --config /workspace/infra/dataset/local.yaml
```

### Debug path — keep the staged file

```bash
sentinel restore run pg-gcs-restore \
  --config /workspace/infra/dataset/local.yaml \
  --keep-file
```

## Behavior

- Sentinel downloads the GCS object to a local staged file.
- The restore runs against the staged file.
- The staged file is deleted after success or failure unless `keep_file: true` or `--keep-file` is set.
- Missing GCS objects surface an actionable restore error (not a generic storage failure).

## Verification

```bash
sentinel restore history pg-gcs-restore --config /workspace/infra/dataset/local.yaml
sentinel storage status --config /workspace/infra/dataset/local.yaml --output text
```

## Rollback / recovery

Same as [restore-from-backup](./restore-from-backup.md) — restore is destructive on target. Re-restore from a known-good earlier object.

## References

- `internal/adapters/storage/gcs/`
- [check-storage-backend](./check-storage-backend.md)
