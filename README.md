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

### Download a prebuilt binary (recommended)

No Go toolchain required. Prebuilt binaries are published on every release for
**linux**, **macOS**, and **Windows** (amd64 + arm64, except windows/arm64),
each with a SHA-256 `checksums.txt`.

1. Grab the asset for your OS/arch from the
   [latest release](https://github.com/denisakp/sentinel/releases/latest) —
   e.g. `sentinel-<version>-linux-amd64.tar.gz` (Windows ships `.zip`).
2. Download and verify the integrity of your download:

   ```bash
   VERSION=<version>          # e.g. 1.3.0 (no leading "v")
   OS=linux                   # linux | darwin | windows
   ARCH=amd64                 # amd64 | arm64
   BASE=https://github.com/denisakp/sentinel/releases/latest/download

   curl -LO "$BASE/sentinel-$VERSION-$OS-$ARCH.tar.gz"
   curl -LO "$BASE/checksums.txt"
   sha256sum -c checksums.txt --ignore-missing
   ```
3. Extract and put it on your `PATH`:

   ```bash
   tar -xzf "sentinel-$VERSION-$OS-$ARCH.tar.gz"   # unzip on Windows
   sudo mv sentinel /usr/local/bin/
   sentinel version
   sentinel version --tools   # check pg_dump, mysqldump, mongodump versions
   ```

Prebuilt binaries still expect the relevant DB client tools (`pg_dump`,
`mysqldump`, `mongodump`, …) on your `PATH`.

### Install with `go install`

If you already have Go 1.24+:

```bash
go install github.com/denisakp/sentinel@latest
```

> **Note:** a `go install` build is compiled without release ldflags, so
> `sentinel version` reports the `dev / unknown / unknown` development
> fallback rather than a stamped version. Use a prebuilt release binary if you
> need `sentinel version` to report the real version/commit/build date.

### Build from source (dev only)

Requires Go 1.24+.

```bash
git clone https://github.com/denisakp/sentinel.git
cd sentinel
go mod download
go build -o sentinel ./...
./sentinel version
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
    # Optional Grandfather-Father-Son (GFS) long-horizon retention. When set, a
    # backup is kept if ANY rule keeps it (flat OR any GFS tier). Buckets are
    # calendar periods in UTC; empty periods are skipped. See
    # docs/runbooks/retention-gfs.md.
    gfs:
      keep_daily: 7      # newest backup of each of the last 7 days
      keep_weekly: 4     # ... last 4 ISO weeks (Mon–Sun)
      keep_monthly: 12   # ... last 12 months
      keep_yearly: 3     # ... last 3 years

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
