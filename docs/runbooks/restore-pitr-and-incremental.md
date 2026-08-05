# Runbook — PITR and incremental restore

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [restore-from-backup](./restore-from-backup.md), [chain-corruption-recovery](./chain-corruption-recovery.md), [verify-backup-integrity](./verify-backup-integrity.md)

## When to use

Point-in-time recovery (PostgreSQL) or incremental chain replay (Postgres / MySQL / MariaDB / MongoDB) for fine-grained recovery to a target time.

## Preconditions

- Base backup artifact exists with advanced restore metadata in its manifest.
- For PostgreSQL PITR: backup ships with PITR-capable metadata; target timestamp falls inside the manifest recoverable window.
- For MySQL/MariaDB: archived binlog artifacts available; `mysql.binlog_path` readable at backup capture time.
- For MongoDB: backup was captured against a replica-set with oplog access.

## Steps

### Define PITR + incremental jobs

```yaml
restores:
  postgres-incident-recovery:
    enabled: true
    type: postgres
    host: host.docker.internal
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    verify_after_restore: true
    backup_source:
      type: local
      backup_path: /workspace/backups/postgres-dev.dump
    restore_mode: pitr
    pitr_timestamp: "2026-03-20T23:59:00Z"

  mysql-incremental-recovery:
    enabled: true
    type: mysql
    host: host.docker.internal
    port: 3307
    username: sentinel
    password_env: DEV_MYSQL_PASSWORD
    database: sentinel
    backup_source:
      type: local
      backup_path: /workspace/backups/mysql-dev.sql
    restore_mode: incremental
    incremental_from_backup: mysql-dev-base
    mysql:
      binlog_target_time: "2026-03-21T04:15:00Z"

databases:
  mysql-dev:
    type: mysql
    host: host.docker.internal
    port: 3307
    username: sentinel
    password_env: DEV_MYSQL_PASSWORD
    database: sentinel
    storage:
      type: local
      local_path: /workspace/backups
    incremental_backup:
      enabled: true
      max_chain_depth: 6
      binlog_check: true
    mysql:
      binlog_path: /var/lib/mysql
```

### Validate, then run

```bash
sentinel restore list --config /workspace/infra/dataset/local.yaml
sentinel restore dry-run postgres-incident-recovery --config /workspace/infra/dataset/local.yaml
sentinel restore run     postgres-incident-recovery --config /workspace/infra/dataset/local.yaml
sentinel restore run     mysql-incremental-recovery --config /workspace/infra/dataset/local.yaml
sentinel restore history postgres-incident-recovery --config /workspace/infra/dataset/local.yaml
sentinel monitor list    --config /workspace/infra/dataset/local.yaml --last 7d
```

## Rules

- PITR requests without a timezone-aware timestamp fail fast.
- PITR requests outside the manifest recoverable window are rejected before replay.
- `restore history` includes advanced restore mode and planning status fields.
- PostgreSQL incremental restore assembles ordered chain artifacts before execution.
- MySQL/MariaDB replay archived binlog artifacts after the base restore. **`mysql.binlog_target_time` and `mysql.binlog_target_position` are mutually exclusive.**
- MongoDB incremental restore replays archived oplog artifacts after the base restore.
- Requests without executable lineage support remain blocked unless `confirm_full_fallback: true` (and a full-restore fallback is acceptable).

## Verification

```bash
sentinel restore history postgres-incident-recovery --config /workspace/infra/dataset/local.yaml
sentinel restore validate-chain mysql-incremental-recovery --config /workspace/infra/dataset/local.yaml
```

Application-side: target row counts and timestamp-anchored values match the requested recovery point.

## Rollback / recovery

If the chain is broken or the recovery point is unreachable: see [chain-corruption-recovery](./chain-corruption-recovery.md). To force a fresh base, `sentinel backup force-full --job <name>`.

## References

- `internal/adapters/restore/incremental/mysqlbinlog/`, `internal/adapters/restore/incremental/pgcombine/`
- `internal/scheduler/restore_integration.go`
- ADR 0005 — Manifest v1 (`docs/adr/0005-manifest-format-v1.md`) — advanced restore metadata
