# Runbook — Restore from backup

> **Superseded by the documentation site: [tutorials/postgres/restore](https://denisakp.github.io/sentinel/tutorials/postgres/restore).**
>
> This runbook superseded by the per-engine tutorial tracks. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [restore-pitr-and-incremental](./restore-pitr-and-incremental.md), [restore-from-gcs](./restore-from-gcs.md), [restore-rehearsal](./restore-rehearsal.md), [verify-backup-integrity](./verify-backup-integrity.md)

## When to use

Disaster recovery, scratch-DB seeding, or scheduled DR drills. Restore jobs are **disabled by default** for safety — they must be explicitly enabled.

## Preconditions

- Target DB exists and is reachable.
- Backup artifact reachable from the configured `backup_source` (local path, S3, GCS, Azure, GDrive).
- For encrypted artifacts: matching key resolvable from `encryption_key_env`.

## Steps

### Define a restore job

```yaml
restores:
  pg-test-restore:
    type: postgres
    enabled: false          # opt-in
    host: host.docker.internal
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel_restore   # target DB (must exist)
    schedule: "0 3 * * 0"        # Sun 03:00 weekly
    backup_source:
      type: local
      backup_path: /workspace/backups/postgres-dev.sql
    verify_after_restore: true
    retention:
      keep_last: 3
```

### Inspect and validate

```bash
sentinel restore list   --config /workspace/infra/dataset/local.yaml
sentinel restore status pg-test-restore --config /workspace/infra/dataset/local.yaml
```

### Dry-run (no writes)

Validates connectivity, source, and config without applying changes.

```bash
sentinel restore dry-run pg-test-restore --config /workspace/infra/dataset/local.yaml
```

Expected: `Dry-run for restore job 'pg-test-restore': configuration is valid`.

### Enable / pause / resume

```bash
sentinel restore enable pg-test-restore --config /workspace/infra/dataset/local.yaml
sentinel restore pause  pg-test-restore --config /workspace/infra/dataset/local.yaml
sentinel restore resume pg-test-restore --config /workspace/infra/dataset/local.yaml
```

### Run on demand

```bash
sentinel restore run pg-test-restore --config /workspace/infra/dataset/local.yaml
```

### History

```bash
sentinel restore history --config /workspace/infra/dataset/local.yaml
sentinel restore history pg-test-restore --config /workspace/infra/dataset/local.yaml
```

## Restore history status semantics

- `success` — restore completed; post-restore checks (if configured) passed.
- `failed` — restore execution failed (integrity / validation / runtime).
- `timeout` — exceeded `timeout_seconds`.
- `skipped` — intentionally skipped (e.g. `lock_conflict`, `concurrency_limit_reached`).

Notification event mapping:

- `success` → `success` event.
- `failed`, `timeout` → `failure` event.
- `skipped` → `warning` event.

## Verification

```bash
sentinel restore history pg-test-restore --config /workspace/infra/dataset/local.yaml
psql -h host.docker.internal -U sentinel -d sentinel_restore -c '\dt'
```

Row counts on key tables match expectations from source.

## Rollback / recovery

Restore is destructive on the target DB. Rollback means re-restoring from a known-good earlier artifact, or dropping/recreating the target DB and rerunning.

## References

- `internal/cli/restore.go`, `internal/scheduler/restore_executor.go`
- `internal/adapters/restore/{pg,mysql,mariadb,mongo}/args_builder.go`
