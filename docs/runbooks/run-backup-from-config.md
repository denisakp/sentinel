# Runbook — Run backup from config

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [environment-setup](./environment-setup.md), [start-scheduler](./start-scheduler.md), [inspect-monitor-history](./inspect-monitor-history.md), [verify-backup-integrity](./verify-backup-integrity.md)

## When to use

One-shot backup run from a YAML config: ad-hoc dumps, manual triage, or seeding a new storage backend. For recurring runs use [start-scheduler](./start-scheduler.md).

## Preconditions

- Sentinel binary on `PATH`.
- DB client tools installed (`pg_dump`, `mysqldump`, `mariadb-dump`, `mongodump`).
- Config file exists, validated, and `password_env` variables exported.
- `history_db_path` writable if you want monitor records.

## Steps

### Export `password_env` values

```bash
export DEV_POSTGRES_PASSWORD=sentinel
export DEV_MYSQL_PASSWORD=sentinel
export DEV_MARIADB_PASSWORD=sentinel
```

### Validate the config first

```bash
sentinel config validate --config /workspace/infra/dataset/local.yaml
```

Expected: `Configuration file is valid`.

### Run all configured backups

```bash
sentinel backup --config /workspace/infra/dataset/local.yaml
```

Sentinel dispatches per-job: `postgres` → `pg_dump`, `mysql` → `mysqldump`, `mariadb` → `mariadb-dump`, `mongodb` → `mongodump`. Jobs run sequentially. `schedule:` fields in the config are ignored by `backup --config` (only used by `schedule start`).

### Sample config

```yaml
version: "1.0"
log_format: json
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  postgres-sample:
    type: postgres
    host: sentinel-databases-pgsql-1
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-dev
    schedule: "*/10 * * * *"

  mysql-sample:
    type: mysql
    host: sentinel-databases-mysql-1
    port: 3306
    username: root
    password_env: DEV_MYSQL_PASSWORD
    database: Chinook_AutoIncrement
    output: mysql-dev
    schedule: "*/10 * * * *"

  mariadb-sample:
    type: mariadb
    host: sentinel-databases-mariadb-1
    port: 3306
    username: root
    password_env: DEV_MARIADB_PASSWORD
    database: Chinook_AutoIncrement
    output: mariadb-dev
    schedule: "*/10 * * * *"
```

### Add MongoDB

Append under `databases:`:

```yaml
  mongo-sample:
    type: mongodb
    uri: "mongodb://host.docker.internal:27017"
    database: sentinel
    output: mongo-dev
    schedule: "*/10 * * * *"
```

Then re-run `sentinel backup --config ...`.

## Verification

```bash
ls -lh /workspace/backups/
head -20 /workspace/backups/postgres-dev.sql
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 1h
```

Every job should appear with `STATUS=success`. For integrity proof see [verify-backup-integrity](./verify-backup-integrity.md).

## Rollback / recovery

If a run produced bad artifacts, delete the offending files and re-run a single job:

```bash
rm /workspace/backups/mysql-dev.sql
sentinel backup --config /workspace/infra/dataset/local.yaml --job mysql-sample
```

For failed runs see [failed-backup-triage](./failed-backup-triage.md).

## References

- `internal/cli/backup.go`
- `internal/adapters/dump/{pg,mysql,mariadb,mongo}/args_builder.go`
