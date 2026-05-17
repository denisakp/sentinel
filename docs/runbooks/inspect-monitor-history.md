# Runbook — Inspect monitor history

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [failed-backup-triage](./failed-backup-triage.md), [db-migration-status](./db-migration-status.md)

## When to use

Daily verification, post-incident triage, status review, or exporting history for external dashboards.

## Preconditions

- `history_db_path` configured in the YAML and writable.
- Monitor records exist (every `--config` run is recorded).

## Steps

### List recent executions

Default table (v1.0.2+) columns: `ID | JOB | STATUS | TIMESTAMP | DURATION | ERROR`. The `ID` is usable directly with `monitor show --id`.

```bash
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 24h
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 7d --status failure
sentinel monitor list --config /workspace/infra/dataset/local.yaml --type mysql
sentinel monitor list --config /workspace/infra/dataset/local.yaml --job postgres-sample
```

No matches: exit code 0, prints `no matching records`.

### Show a specific execution

```bash
sentinel monitor show --config /workspace/infra/dataset/local.yaml --id <execution-id>
```

### Aggregate statistics

```bash
sentinel monitor stats --config /workspace/infra/dataset/local.yaml
sentinel monitor stats --config /workspace/infra/dataset/local.yaml --job postgres-sample --last 12h
```

### Export

```bash
sentinel monitor export --config /workspace/infra/dataset/local.yaml \
  --format json --output /workspace/backups/history.json

sentinel monitor export --config /workspace/infra/dataset/local.yaml \
  --format csv --status failure --output /workspace/backups/failures.csv

sentinel monitor export --config /workspace/infra/dataset/local.yaml \
  --job postgres-sample --format json --output /workspace/backups/pg-history.json
```

## Verification

```bash
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 1h --format table
```

Each `--config` run should appear within seconds of completion.

## Rollback / recovery

`history.db` stores only execution metadata, not backup artifacts. Safe to delete and rebuild from scratch on next run. Before deleting in production: `cp history.db history.db.bak` first.

## References

- `internal/monitor/recorder.go`, `internal/monitor/querier.go`
- [db-migration-status](./db-migration-status.md) for schema migration semantics
