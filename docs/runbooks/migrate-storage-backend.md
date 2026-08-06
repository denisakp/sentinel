# Runbook — Migrate storage backend

> **Superseded by the documentation site: [guides/migrate-storage-backend](https://denisakp.github.io/sentinel/guides/migrate-storage-backend).**
>
> This runbook same column problem, and claims retention runs cleanly after a migration. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [check-storage-backend](./check-storage-backend.md), [apply-retention](./apply-retention.md), [run-backup-from-config](./run-backup-from-config.md)

## When to use

Switching `defaults.storage` from local to S3 / GCS / Azure Blob, or between cloud backends. Existing local artifacts are kept as operator-managed cold storage.

## Preconditions

- Credentials for the new backend resolvable (env vars, service-account file).
- Permissions on the new backend: list, write, delete.
- A validated YAML pointing at the new backend.

## Steps

### 1. Verify the new backend is reachable

**Before** switching production traffic:

```bash
sentinel storage status --config /workspace/infra/dataset/new-backend.yaml --output text
```

Confirm reachable + zero existing objects (or known seed state).

### 2. Update `defaults.storage`

Example — local → GCS:

```yaml
defaults:
  storage:
    type: gcs
    gcs_bucket: ${SENTINEL_GCS_BUCKET}
    gcs_project_id: ${SENTINEL_GCP_PROJECT}
    gcs_credentials_file: ${SENTINEL_GCS_SA_FILE}
```

Per-job `storage:` overrides still win — audit those if you want a uniform migration.

### 3. Validate the config

```bash
sentinel config validate --config /workspace/infra/dataset/sentinel.yaml
```

### 4. Seed the new backend with a one-shot backup

```bash
sentinel backup --config /workspace/infra/dataset/sentinel.yaml
```

### 5. Verify new artifacts

```bash
sentinel monitor list --config /workspace/infra/dataset/sentinel.yaml --last 1h
```

The `file_path` column should reflect the new scheme:

| Backend       | `file_path` prefix                              |
|---------------|-------------------------------------------------|
| local         | `/...` absolute path                            |
| S3            | `s3://bucket/key`                               |
| GCS           | `gs://bucket/object`                            |
| Azure Blob    | `https://<account>.blob.core.windows.net/...`   |

### 6. Restart the scheduler

```bash
systemctl restart sentinel-scheduler
# or
sentinel schedule start --config /workspace/infra/dataset/sentinel.yaml
```

## Retention semantics post-migration

- Retention runs cleanly against the new backend (`retention apply`).
- Old artifacts on the previous backend become operator-managed cold storage — Sentinel will not touch them unless that backend is reintroduced in the config.
- Audit and prune old artifacts manually once you no longer need them.

## Verification

```bash
sentinel storage status --config /workspace/infra/dataset/sentinel.yaml --output json
sentinel monitor list   --config /workspace/infra/dataset/sentinel.yaml --last 24h
sentinel retention preview --config /workspace/infra/dataset/sentinel.yaml
```

## Rollback / recovery

To revert: restore the old `defaults.storage` block, validate, restart scheduler. New artifacts will resume on the old backend; the seed artifact on the new backend remains under your control.

## References

- `internal/adapters/storage/registry.go`, `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/`
- [check-storage-backend](./check-storage-backend.md)
