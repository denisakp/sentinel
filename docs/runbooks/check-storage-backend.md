# Runbook — Check storage backend

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [migrate-storage-backend](./migrate-storage-backend.md), [apply-retention](./apply-retention.md), [restore-from-gcs](./restore-from-gcs.md)

## When to use

Pre-flight before scheduling new jobs against a backend, post-deployment health check, or during failure triage when artifacts are missing.

## Preconditions

- Backend credentials resolvable from the environment / config.
- Network egress to the backend allowed.

## Steps

### Validate config first

```bash
sentinel config validate --config /workspace/infra/dataset/local.yaml
```

### Query backend status

```bash
# Text (default)
sentinel storage status --config /workspace/infra/dataset/local.yaml --output text

# JSON for scripting
sentinel storage status --config /workspace/infra/dataset/local.yaml --output json
```

Output reports reachability, backup counts, and total size per configured backend.

### Example: GCS

```yaml
defaults:
  storage:
    type: gcs
    gcs_bucket: ${SENTINEL_GCS_BUCKET}
    gcs_project_id: ${SENTINEL_GCP_PROJECT}
    gcs_credentials_file: ${SENTINEL_GCS_SA_FILE}
```

```bash
sentinel storage status --config /workspace/infra/dataset/gcs.yaml --output text
sentinel backup         --config /workspace/infra/dataset/gcs.yaml
sentinel monitor list   --config /workspace/infra/dataset/gcs.yaml --last 1h
```

Healthy GCS setup:

- `storage status` reports the backend as reachable.
- Successful backup `file_path` uses `gs://bucket/object`.
- Scheduled retention prunes expired objects without marking the run as failed.

## Verification

For each configured backend, `storage status` returns reachable and `monitor list` shows the expected `file_path` scheme:

| Backend       | `file_path` prefix                              |
|---------------|-------------------------------------------------|
| local         | `/...` (absolute path)                          |
| S3            | `s3://bucket/key`                               |
| GCS           | `gs://bucket/object`                            |
| Azure Blob    | `https://<account>.blob.core.windows.net/...`   |
| Google Drive  | `gdrive://<file-id>`                            |

## Rollback / recovery

Not applicable — read-only operation.

## References

- `internal/storage/storage.go`, `internal/storage/backend.go`
- `internal/storage/{local,sentinel_s3,gcs,gdrive,azure}/`
