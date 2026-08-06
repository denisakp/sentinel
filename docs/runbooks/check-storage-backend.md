# Runbook — Check storage backend

> **Superseded by the documentation site: [guides/check-storage-backend](https://denisakp.github.io/sentinel/guides/check-storage-backend).**
>
> This runbook refers to a `file_path` column that `monitor list` does not have. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

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

## Remote artifact security (spec 047)

Config-driven backups to a remote backend (S3, GCS, Azure Blob, Google Drive) are
now hashed, encrypted (when `encryption_key_env` is set), and manifested on par
with local storage. Alongside each remote artifact Sentinel uploads a
`<name>.manifest.json` sidecar carrying the integrity hash and encryption
metadata, so `sentinel backup verify` and restore can locate and validate the
object. Expect two objects per backup in the bucket: the artifact and its sidecar.

- Verify a remote backend actually holds ciphertext (not plaintext) after an
  encrypted backup — see [enable-encryption](./enable-encryption.md).
- Known limitation: encrypted remote backups are unsupported for the
  auto-discovery `strategy: single` path — that combination fails loud. Use
  `strategy: individual` or local storage. See [enable-encryption](./enable-encryption.md).

## Rollback / recovery

Not applicable — read-only operation.

## References

- `internal/ports/storage.go` — `StorageBackend` port + `StatusReporter`
- `internal/adapters/storage/registry.go` — `NewBackend(...)` single factory
- `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/`
