# Runbook — Restore rehearsal (DR drill)

> **Superseded by the documentation site: [guides/index](https://denisakp.github.io/sentinel/guides/index).**
>
> This runbook the site page is held pending #149; verify a rehearsal by querying the restored database, not with `verify_after_restore`. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [restore-from-backup](./restore-from-backup.md), [restore-pitr-and-incremental](./restore-pitr-and-incremental.md), [verify-backup-integrity](./verify-backup-integrity.md)

## When to use

Quarterly DR drill (recommended cadence per DB engine), or post-incident validation that backups are actually restorable.

## Preconditions

- Scratch DB instance reachable (separate from production).
- Backup artifact recent enough to validate current schemas.
- `verify_after_restore: true` allowed on the rehearsal job.

## Steps

### Create a scratch DB

```bash
# Example: PostgreSQL
psql -h <host> -U postgres -c 'CREATE DATABASE sentinel_rehearsal;'
```

### Add a rehearsal restore job

```yaml
restores:
  pg-rehearsal:
    type: postgres
    enabled: true
    host: <scratch-host>
    port: 5432
    username: <scratch-user>
    password_env: SCRATCH_PG_PASSWORD
    database: sentinel_rehearsal
    schedule: "0 6 1 */3 *"   # 06:00 UTC on the 1st, quarterly
    backup_source:
      type: local
      backup_path: /workspace/backups/postgres-dev.sql
    verify_after_restore: true
```

### Dry-run

```bash
sentinel restore dry-run pg-rehearsal --config /workspace/infra/dataset/local.yaml
```

Expected: `configuration is valid`. No writes.

### Execute

```bash
sentinel restore run pg-rehearsal --config /workspace/infra/dataset/local.yaml
```

### Confirm history

```bash
sentinel restore history pg-rehearsal --config /workspace/infra/dataset/local.yaml
```

Expect `STATUS=success`.

### DB-side validation

```bash
psql -h <scratch-host> -U <scratch-user> -d sentinel_rehearsal -c '\dt'
psql -h <scratch-host> -U <scratch-user> -d sentinel_rehearsal \
  -c 'SELECT COUNT(*) FROM <key-table>;'
```

Compare row counts and key timestamp anchors against source.

## Suggested cadence

| Engine     | Cadence   |
|------------|-----------|
| PostgreSQL | Quarterly |
| MySQL      | Quarterly |
| MariaDB    | Quarterly |
| MongoDB    | Quarterly |

Bump to monthly for production tiers with strict RPO/RTO.

## Verification

A green `restore history` row plus matching DB-side counts. Record results in your DR log.

## Rollback / recovery

Drop the scratch DB after the drill:

```bash
psql -h <host> -U postgres -c 'DROP DATABASE sentinel_rehearsal;'
```

## References

- [restore-from-backup](./restore-from-backup.md)
- [verify-backup-integrity](./verify-backup-integrity.md)
- `internal/scheduler/restore_executor.go`
