# Runbook — DB migration status

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [upgrade-sentinel-binary](./upgrade-sentinel-binary.md), [inspect-monitor-history](./inspect-monitor-history.md)

## When to use

Post-upgrade verification, or when a `monitor`/`schedule`/`retention` command fails with a migration error.

## Preconditions

- `history_db_path` set in the YAML.
- Sentinel binary at the version you intend to run.

## Steps

### Check status

```bash
sentinel db migrate status --config /workspace/infra/dataset/local.yaml
```

Example output:

```
Migration Status:

Current Version: 3
Latest Version:  3
Status: Up-to-date

Applied Migrations:
  Version 1: baseline_schema (applied at 2024-01-15 10:30:00)
  Version 2: add_cleanup_columns (applied at 2024-01-15 10:30:01)
  Version 3: add_consolidated_status_values (applied at 2024-01-15 10:30:02)
```

### Migration semantics

All commands that touch `history.db` (`schedule`, `monitor`, `retention`) auto-apply pending migrations on startup. Guarantees:

1. **Atomic** — each migration runs in a transaction; failures roll back.
2. **Idempotent** — already-applied migrations are skipped (tracked via `schema_migrations`).
3. **Fail-fast** — any migration failure exits before any operation runs.
4. **Checksummed** — each migration has a checksum to detect tampering or corruption.

### When a migration fails

1. Read the error: SQL syntax / constraint violations are reported directly.
2. Inspect `schema_migrations` in the SQLite DB to see which migrations succeeded:

```bash
sqlite3 ~/.sentinel/history.db 'SELECT * FROM schema_migrations;'
```

3. If the database is unrecoverably corrupted, delete it and restart. Sentinel rebuilds from scratch. `history.db` carries metadata only — no backup artifacts are lost.
4. In production, always `cp history.db history.db.bak` before upgrading.

## Verification

```bash
sentinel db migrate status --config /workspace/infra/dataset/local.yaml | grep -E 'Up-to-date|Current Version'
```

## Rollback / recovery

Migrations are forward-only. To recover from a corrupt schema: restore `history.db.bak` (taken pre-upgrade), or delete `history.db` to rebuild.

## References

- `internal/monitor/` — schema and migration runner
- [upgrade-sentinel-binary](./upgrade-sentinel-binary.md)
