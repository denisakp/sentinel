# Sentinel

Sentinel is an open-source CLI tool for automated database backup, restore, and disaster recovery.
It supports PostgreSQL, MySQL, MariaDB, and MongoDB — with cloud storage, scheduling, monitoring, and notifications built in.

**Current version: v1.3.0**

---

## What it does

- **Backup & restore** for PostgreSQL, MySQL, MariaDB, MongoDB
- **Cloud storage** — Local, S3-compatible, Google Cloud Storage, Google Drive, Azure Blob
- **Incremental backup & restore** — WAL-based (PostgreSQL 17+), binary logs (MySQL/MariaDB), oplog (MongoDB)
- **Advanced restore** — PostgreSQL PITR, incremental chain assembly with fallback confirmation
- **Scheduling** — cron-based for both backups and restores
- **Retention** — automatic cleanup by count or age
- **Monitoring** — SQLite-backed execution history, stats, JSON/CSV export
- **Notifications** — Slack, Discord, email, webhook
- **Security** — AES-256 encryption opt-in, SHA-256 manifest integrity

---

## Installation

Requires Go 1.24+.

```bash
git clone https://github.com/denis-yaovi/sentinel.git
cd sentinel
go mod download
go build -o sentinel
```

Verify:

```bash
./sentinel version
./sentinel version --tools   # check pg_dump, mysqldump, mongodump versions
```

---

## Quick start

Create a `sentinel.yaml`:

```yaml
version: "1.0"
log_format: json

defaults:
  schedule: "0 2 * * *"
  storage:
    type: local
    local_path: ./backups
  notifications:
    - type: slack
      webhook_url_env: SLACK_WEBHOOK_URL
      events: [failure, warning]
  retention:
    keep_last: 30
    keep_days: 90

databases:
  prod-postgres:
    type: postgres
    host_env: PROD_DB_HOST
    port: 5432
    username_env: PROD_DB_USER
    password_env: PG_PASSWORD
    database: myapp_prod

restores:
  test-restore:
    type: postgres
    enabled: false
    host: localhost
    port: 5432
    username_env: TEST_DB_USER
    password_env: TEST_DB_PASSWORD
    database: test_db
    schedule: "0 3 * * 0"
    backup_source:
      type: local
      backup_path: ./backups/prod-postgres-latest.sql
    verify_after_restore: true
```

Run a backup:

```bash
./sentinel backup --config sentinel.yaml
```

---

## Core commands

```bash
# Backup
sentinel backup --config sentinel.yaml

# Scheduler (starts cron loop for backups + restores)
sentinel schedule start --config sentinel.yaml
sentinel schedule list --config sentinel.yaml
sentinel schedule status prod-postgres --config sentinel.yaml

# Restore
sentinel restore list --config sentinel.yaml
sentinel restore enable test-restore --config sentinel.yaml
sentinel restore dry-run test-restore --config sentinel.yaml
sentinel restore run test-restore --config sentinel.yaml
sentinel restore history --config sentinel.yaml

# Monitoring
sentinel monitor list --config sentinel.yaml --last 7d
sentinel monitor stats --config sentinel.yaml
sentinel monitor export --config sentinel.yaml --format json --output history.json

# Retention
sentinel retention preview --config sentinel.yaml
sentinel retention apply --config sentinel.yaml

# Config & storage
sentinel config validate --config sentinel.yaml
sentinel storage status --config sentinel.yaml

# Encryption key
sentinel security init-key

# Schema migrations
sentinel db migrate status --config sentinel.yaml
```

---

## Incremental backup

Enable incremental backup on a job:

```yaml
databases:
  mysql-prod:
    type: mysql
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PASSWORD
    database: appdb
    storage:
      type: local
      local_path: ./backups
    incremental_backup:
      enabled: true
      max_chain_depth: 6
      binlog_check: true
    mysql:
      binlog_path: /var/lib/mysql
```

Inspect and manage chains:

```bash
sentinel backup chain-status --config sentinel.yaml
sentinel backup chain-list --config sentinel.yaml
sentinel backup force-full --job mysql-prod --config sentinel.yaml
```

---

## Advanced restore (PITR & incremental)

```yaml
restores:
  postgres-incident-recovery:
    enabled: true
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: POSTGRES_PASSWORD
    database: appdb
    verify_after_restore: true
    backup_source:
      type: local
      backup_path: ./backups/appdb-base.dump
    restore_mode: pitr
    pitr_timestamp: "2026-03-20T23:59:00Z"

  mysql-incremental-recovery:
    enabled: true
    type: mysql
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PASSWORD
    database: appdb
    backup_source:
      type: local
      backup_path: ./backups/appdb-base.sql
    restore_mode: incremental
    incremental_from_backup: appdb-base
    mysql:
      binlog_target_time: "2026-03-21T04:15:00Z"
```

```bash
sentinel restore dry-run postgres-incident-recovery --config sentinel.yaml
sentinel restore run postgres-incident-recovery --config sentinel.yaml
sentinel restore validate-chain mysql-incremental-recovery --config sentinel.yaml
sentinel restore history postgres-incident-recovery --config sentinel.yaml
```

> For full operator notes on PITR, incremental chain rules, and fallback confirmation, see [docs/runbooks/restore-pitr-and-incremental.md](docs/runbooks/restore-pitr-and-incremental.md).

---

## Encryption

Encryption is opt-in. To enable:

```yaml
encryption_key_env: SENTINEL_MASTER_KEY
```

```bash
sentinel security init-key          # generate key
export SENTINEL_MASTER_KEY=<key>
sentinel backup --config sentinel.yaml
```

Plaintext is the default when no encryption config is present. See [docs/runbooks/enable-encryption.md](docs/runbooks/enable-encryption.md) for full security semantics.

---

## Contributing

- [CONTRIBUTING.md](CONTRIBUTING.md) — setup, coding standards, PR process
- [SECURITY.md](SECURITY.md) — reporting vulnerabilities
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) — community standards
- [Bug report](.github/ISSUE_TEMPLATE/1-bug.md) · [Feature request](.github/ISSUE_TEMPLATE/2-feature-request.md)
