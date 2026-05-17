# Sentinel – External Tester Guide

> Version: **v1.3.0** — Tested on Ubuntu 24.04 (container) with PostgreSQL 17+, MySQL 8, MariaDB 11, MongoDB 8  
> Target audience: external testers who want to exercise Sentinel end-to-end against real databases.

---

## Table of Contents

1. [What is Sentinel?](#1-what-is-sentinel)
2. [Command Reference Overview](#2-command-reference-overview)
3. [Environment Setup](#3-environment-setup)
   - [Option A – Ubuntu Container (recommended)](#option-a--ubuntu-container-recommended)
   - [Option B – Dev Docker Image (Sentinel + clients bundled)](#option-b--dev-docker-image-sentinel--clients-bundled)
4. [Database Connectivity Matrix](#4-database-connectivity-matrix)
5. [Scenario 1 – Config-Only Backup Run (Docker)](#5-scenario-1--config-only-backup-run-docker)
6. [Scenario 2 – Scheduled Declarative Workflow](#6-scenario-2--scheduled-declarative-workflow)
7. [Monitoring Backup History](#7-monitoring-backup-history)
8. [Retention Policies](#8-retention-policies)
9. [Restore Management](#9-restore-management)
10. [Backup Security (Encryption)](#10-backup-security-encryption)
11. [Config Validation and Storage Status](#11-config-validation-and-storage-status)
12. [Verifying Everything Works](#12-verifying-everything-works)
13. [Troubleshooting](#13-troubleshooting)

---

## 1. What is Sentinel?

Sentinel is a cloud-native CLI tool for secure, automated database backup and restore. It supports:

| Feature | Details |
|---|---|
| **Databases** | PostgreSQL, MySQL, MariaDB, MongoDB |
| **Storage backends** | Local filesystem, AWS S3 / S3-compatible, Google Cloud Storage, Google Drive, Azure Blob |
| **Scheduling** | Cron-based (`schedule start`) for both backups and restores |
| **Retention** | Automatic cleanup by count (`keep_last`) or age (`keep_days`) |
| **Monitoring** | SQLite-backed execution history, statistics, CSV/JSON export |
| **Notifications** | Slack, Discord, webhook, SMTP email |
| **Security** | AES-256 encryption, SHA-256 manifest integrity verification |
| **Restore management** | Dry-run, enable/disable, post-restore verification |

For containerized testing, this guide uses a single mode:
- **Declarative (`--config`)** — mount a YAML file and run all actions from that config.

---

## 2. Command Reference Overview

```
sentinel backup      — Run one or more database backups
sentinel restore     — Schedule, dry-run, enable/disable restore jobs
sentinel schedule    — Start/stop the cron scheduler for backups and restores
sentinel monitor     — Query backup execution history and statistics
sentinel retention   — Apply or preview backup cleanup policies
sentinel config      — Validate YAML configuration files
sentinel storage     — Check configured storage backend status
sentinel security    — Generate and manage AES-256 encryption keys
sentinel db          — Database schema migration management
```

### Quick-reference cheat sheet

```bash
# Backup from config
sentinel backup --config /etc/sentinel/sentinel.yaml

# List last 24h of backup history
sentinel monitor list --config sentinel.yaml --last 24h

# Preview retention cleanup
sentinel retention preview --config sentinel.yaml

# Apply retention policies
sentinel retention apply --config sentinel.yaml

# Start scheduled backups & restores
sentinel schedule start --config sentinel.yaml
```

Default config discovery check (when `./sentinel-config.yaml` exists):

```bash
sentinel monitor list --last 24h
sentinel schedule list
sentinel restore list
```

Explicit override precedence check:

```bash
sentinel monitor list --config /tmp/other.yaml --last 24h
sentinel restore list --config /tmp/other.yaml
```

---

## 3. Environment Setup

### Prerequisites on the host

The databases must already be running and reachable. In this guide they are started with:

```bash
docker compose -f infra/docker/docker-compose.yml up -d pgsql mysql mariadb
docker run -d --name sentinel-mongo -p 27017:27017 mongo:8.2
```

Exposed ports (default docker-compose.yml):

| Service | Host port | DB | Credentials |
|---|---|---|---|
| PostgreSQL 16 | `5432` | `sentinel` | `sentinel / sentinel` |
| MySQL 8 LTS | `3307` | `sentinel` | `sentinel / sentinel` |
| MariaDB 11 | `3306` | `sentinel` | `sentinel / sentinel` |
| MongoDB 8.2 | `27017` | — | no auth (dev) |

---

### Option A – Ubuntu Container (recommended)

This is the primary test path. You run Sentinel inside a fresh Ubuntu container and connect to the host databases.

#### Step 1 — Start an Ubuntu container on the same Docker network

```bash
docker run -it --rm \
  --name sentinel-tester \
  --add-host=host.docker.internal:host-gateway \
  -v "$PWD:/workspace" \
  ubuntu:24.04 bash
```

The `-v "$PWD:/workspace"` mount makes the Sentinel binary, config files, and backup output visible both inside and outside the container.

#### Step 2 — Install required dump client tools inside the container

Sentinel shells out to native DB tools. Install them once:

```bash
apt-get update && apt-get install -y \
  ca-certificates curl gnupg wget \
  postgresql-client-18 \
  mysql-client

# MariaDB client (mariadb-dump)
MARIADB_VERSION="11.8.6"
wget -q "https://downloads.mariadb.org/rest-api/mariadb/${MARIADB_VERSION}/mariadb-${MARIADB_VERSION}-linux-systemd-x86_64.tar.gz" \
    -O mariadb.tar.gz
tar -xzf mariadb.tar.gz \
    "mariadb-${MARIADB_VERSION}-linux-systemd-x86_64/bin/mariadb-dump" \
    --strip-components=2
mv mariadb-dump /usr/local/bin/
chmod +x /usr/local/bin/mariadb-dump
rm mariadb.tar.gz

# MongoDB database tools
wget -qO mongodb-database-tools.deb \
    https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-x86_64-100.14.1.deb
dpkg -i mongodb-database-tools.deb
rm mongodb-database-tools.deb
```

Verify:

```bash
pg_dump --version
mysqldump --version
mariadb-dump --version
mongodump --version
```

#### Step 3 — Install the Sentinel binary

```bash
cp /workspace/sentinel-linux-amd64 /usr/local/bin/sentinel
chmod +x /usr/local/bin/sentinel
sentinel --help
```

#### Step 4 — Prepare working directories

```bash
mkdir -p /workspace/backups /workspace/.sentinel
```

---

### Option B – Dev Docker Image (Sentinel + clients bundled)

An alternative image is provided that ships the Sentinel binary and all DB client tools together. Build it once:

```bash
docker build -f infra/docker/Dockerfile.dev -t sentinel-dev:local .
```

Then run any Sentinel command by mounting your workspace:

```bash
docker run --rm \
  --add-host=host.docker.internal:host-gateway \
  -e DEV_POSTGRES_PASSWORD=sentinel \
  -e DEV_MYSQL_PASSWORD=sentinel \
  -e DEV_MARIADB_PASSWORD=sentinel \
  -v "$PWD:/workspace" \
  sentinel-dev:local \
  sh -lc 'sentinel backup --config /workspace/infra/dataset/local.yaml'
```

The rest of this guide uses the Ubuntu container path (Option A) inside `/workspace`.

---

## 4. Database Connectivity Matrix

Use these values when writing config files inside the container. `host.docker.internal` resolves to the Docker host machine from inside any container that has `--add-host=host.docker.internal:host-gateway`.

| Database | host | port | user | password | database |
|---|---|---|---|---|---|
| PostgreSQL | `host.docker.internal` | `5432` | `sentinel` | `sentinel` | `sentinel` |
| MySQL | `host.docker.internal` | `3307` | `sentinel` | `sentinel` | `sentinel` |
| MariaDB | `host.docker.internal` | `3306` | `sentinel` | `sentinel` | `sentinel` |
| MongoDB | `host.docker.internal` | `27017` | — | — | `sentinel` |

---

## 5. Scenario 1 – Config-Only Backup Run (Docker)

In the container, run Sentinel only with `--config`. This keeps execution predictable and easy for external testers.

### 5.1 Export environment variables used by `password_env`

```bash
export DEV_POSTGRES_PASSWORD=sentinel
export DEV_MYSQL_PASSWORD=sentinel
export DEV_MARIADB_PASSWORD=sentinel
```

### 5.2 Validate the config

```bash
sentinel config validate --config /workspace/infra/dataset/local.yaml
```

Expected:
```
Configuration file is valid
```

### 5.3 Run all configured backups in one command

```bash
sentinel backup --config /workspace/infra/dataset/local.yaml
```

Expected output (example):
```
Backup successfully written to /workspace/backups/postgres-dev.sql
Backup complete !
Backup successfully written to /workspace/backups/mysql-dev.sql
Backup complete !
Backup successfully written to /workspace/backups/mariadb-dev.sql
Backup complete !
```

### 5.4 Verify generated artifacts

```bash
ls -lh /workspace/backups/
head -20 /workspace/backups/postgres-dev.sql
head -20 /workspace/backups/mysql-dev.sql
head -20 /workspace/backups/mariadb-dev.sql
```

---

## 6. Scenario 2 – Scheduled Declarative Workflow

The YAML config is the recommended approach for multi-database workflows, scheduling, retention, monitoring, and notifications.

### 6.1 The config file

A ready-to-use config is provided at `infra/dataset/local.yaml`. It backs up PostgreSQL, MySQL, and MariaDB to `/workspace/backups` and writes monitoring history to `/workspace/.sentinel/history.db`.

```yaml
# infra/dataset/local.yaml
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

Notes:

- Passwords are read from environment variables (`password_env`) — never stored in plain text in the file.
- `history_db_path` enables execution history recording for `monitor` commands.
- `schedule` fields are used only by `sentinel schedule start`; they are ignored by `sentinel backup --config`.

### 6.1.1 Default backup schedule inheritance checks

You can set a shared schedule once at `defaults.schedule` and omit `schedule` on selected backup jobs.

```yaml
defaults:
  schedule: "*/10 * * * *"
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
    # schedule omitted -> inherits defaults.schedule
```

Validation/behavior checks:

```bash
sentinel schedule list --config /workspace/infra/dataset/local.yaml --format json
sentinel schedule status postgres-sample --config /workspace/infra/dataset/local.yaml
```

Expected:

- Jobs without local `schedule` show the inherited effective schedule.
- Jobs with local `schedule` keep their explicit value.
- Whitespace-only `defaults.schedule` fails `sentinel config validate` with job-scoped cron error context.

### 6.2 Validate the config before running

```bash
sentinel config validate --config /workspace/infra/dataset/local.yaml
```

Expected:

```text
Configuration file is valid
```

### 6.3 Run all backups from config (one-shot)

```bash
export DEV_POSTGRES_PASSWORD=sentinel
export DEV_MYSQL_PASSWORD=sentinel
export DEV_MARIADB_PASSWORD=sentinel

sentinel backup --config /workspace/infra/dataset/local.yaml
```

Sentinel detects each `type` and dispatches the correct backup engine automatically:

- `postgres-sample` → `pg_dump`
- `mysql-sample` → `mysqldump`
- `mariadb-sample` → `mariadb-dump`

Each job runs sequentially. Expected output:

```text
Backup successfully written to /workspace/backups/postgres-dev.sql
Backup complete !
Backup successfully written to /workspace/backups/mysql-dev.sql
Backup complete !
Backup successfully written to /workspace/backups/mariadb-dev.sql
Backup complete !
```

### 6.4 Add MongoDB to the config

To also backup MongoDB, add this job under `databases:` in the config:

```yaml
  mongo-sample:
    type: mongodb
    uri: "mongodb://host.docker.internal:27017"
    database: sentinel
    output: mongo-dev
    schedule: "*/10 * * * *"
```

Then run again:

```bash
sentinel backup --config /workspace/infra/dataset/local.yaml
```

### 6.5 Run the scheduler (continuous, cron-driven)

The `schedule start` command keeps Sentinel running and executes backup and restore jobs according to their `schedule:` cron expressions.

```bash
# This runs in the foreground. Use tmux or run in background for testing.
sentinel schedule start --config /workspace/infra/dataset/local.yaml
```

List what jobs are registered:

```bash
sentinel schedule list --config /workspace/infra/dataset/local.yaml
```

Expected output (table, v1.0.2+):

```text
TYPE    NAME              SCHEDULE        NEXT EXECUTION
backup  postgres-sample   */10 * * * *    2026-03-13T02:00:00Z
backup  mysql-sample      */10 * * * *    2026-03-13T02:00:00Z
backup  mariadb-sample    */10 * * * *    2026-03-13T02:00:00Z
```

Note: `LAST STATUS` is no longer shown in the default table view (v1.0.2). Use `--format json` to get all fields including `last_status` for scripting.

---

## 7. Monitoring Backup History

`monitor` commands require `history_db_path` to be set in the config so that Sentinel knows where to read the SQLite database.

### 7.1 List recent executions

The default table (v1.0.2+) shows: `ID | JOB | STATUS | TIMESTAMP | DURATION | ERROR`.
The `ID` in the first column can be used directly with `monitor show --id <ID>` for detailed inspection.
The `ERROR` column shows a truncated preview for quick failure triage; the full message appears in `monitor show`.

```bash
# Last 24 hours (table format, default)
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 24h

# Last 7 days, only failures — copy the ID from output to drill in with monitor show
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 7d --status failure

# Filter by database type
sentinel monitor list --config /workspace/infra/dataset/local.yaml --type mysql

# Filter by specific job
sentinel monitor list --config /workspace/infra/dataset/local.yaml --job postgres-sample
```

If no records match the filter, the command exits with code `0` and prints:

```text
no matching records
```

### 7.2 Show a specific execution by ID

IDs are visible in the `ID` column of `monitor list`.

```bash
sentinel monitor show --config /workspace/infra/dataset/local.yaml --id <execution-id>
```

### 7.3 View aggregate statistics

```bash
# Stats for all jobs over 30 days (default) — --job is optional
sentinel monitor stats --config /workspace/infra/dataset/local.yaml

# Stats for one job over 12 hours
sentinel monitor stats --config /workspace/infra/dataset/local.yaml \
  --job postgres-sample --last 12h
```

If the job name does not match any records, the command exits with code `0` and prints:

```text
no matching records
```

### 7.4 Export history

```bash
# Export all history as JSON
sentinel monitor export --config /workspace/infra/dataset/local.yaml \
  --format json --output /workspace/backups/history.json

# Export only failures to CSV
sentinel monitor export --config /workspace/infra/dataset/local.yaml \
  --format csv --status failure --output /workspace/backups/failures.csv

# Export one job
sentinel monitor export --config /workspace/infra/dataset/local.yaml \
  --job postgres-sample --format json --output /workspace/backups/pg-history.json
```

### 7.5 Check migration status

Sentinel manages its own SQLite schema through migrations. All commands that interact with the
history database (`schedule`, `monitor`, `retention`) automatically apply pending migrations on
startup. If a migration fails, the process exits immediately (fail-fast) to prevent operating with
an inconsistent schema.

```bash
sentinel db migrate status --config /workspace/infra/dataset/local.yaml
```

Example output:

```
Migration Status:

Current Version: 3
Latest Version:  3
Status: ✓ Up-to-date

Applied Migrations:
  Version 1: baseline_schema (applied at 2024-01-15 10:30:00)
  Version 2: add_cleanup_columns (applied at 2024-01-15 10:30:01)
  Version 3: add_consolidated_status_values (applied at 2024-01-15 10:30:02)
```

Migration reliability guarantees:

1. **Atomic**: each migration runs in a transaction — failures roll back completely.
2. **Idempotent**: already-applied migrations are skipped (tracked via `schema_migrations` table).
3. **Fail-fast**: any migration failure exits before any operation runs.
4. **Checksummed**: each migration has a checksum to detect tampering or corruption.

If a migration fails:

1. Check the error message for SQL syntax or constraint violations.
2. Inspect the `schema_migrations` table in the SQLite database to see which migrations succeeded.
3. If the database is corrupted, delete it and restart — Sentinel rebuilds from scratch.
4. In production, back up `history.db` before upgrading Sentinel.

> `history.db` stores only execution metadata, not backup artifacts. It is safe to delete and
> rebuild if needed.

---

## 8. Retention Policies

### v1.0.6 Scheduled Backup Naming + Retention Behavior

For scheduler-driven executions, Sentinel now treats configured `output` values as prefixes and appends a UTC second timestamp.

Examples for repeated runs with the same configured output:

- `postgres-dev_2026-03-15T02-00-00.sql`
- `postgres-dev_2026-03-15T02-00-00-1.sql` (same-second collision suffix)

Validation notes:

- Scheduler mode only: one-shot `sentinel backup --config ...` naming remains unchanged.
- Canonical extension normalization: a configured output like `postgres-dev.sql` is normalized before suffixing to avoid double extensions.
- Post-success retention: when `keep_last` and/or `keep_days` are configured, retention is evaluated automatically after successful scheduled runs.
- Retention failures are warning-level and do not convert an already successful backup into a failed result.

Retention ensures old backup artifacts are cleaned up automatically. Policies are defined per job or in `defaults`.

### 8.1 Config with retention

Add `retention:` to any job or to `defaults:`:

```yaml
defaults:
  storage:
    type: local
    local_path: /workspace/backups
  retention:
    keep_last: 5    # keep the 5 most recent backups per job
    keep_days: 30   # and only those from the last 30 days
```

### 8.2 Preview before deleting (dry-run)

Always preview first:

```bash
sentinel retention preview --config /workspace/infra/dataset/local.yaml
```

Output shows which files would be deleted and why (exceeds `keep_last` or `keep_days`).

### 8.3 Apply retention (delete old backups)

```bash
# Apply to all jobs
sentinel retention apply --config /workspace/infra/dataset/local.yaml

# Apply to one specific job
sentinel retention apply --config /workspace/infra/dataset/local.yaml \
  --job postgres-sample

# Preview apply (no delete) inline
sentinel retention apply --config /workspace/infra/dataset/local.yaml --dry-run
```

---

## 9. Restore Management

Restore jobs are **disabled by default** for safety. They must be explicitly enabled before execution.

### 9.1 Add a restore job to the config

```yaml
restores:
  pg-test-restore:
    type: postgres
    enabled: false          # must be explicitly enabled
    host: host.docker.internal
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel_restore   # target DB (must exist)
    schedule: "0 3 * * 0"        # weekly on Sunday 03:00
    backup_source:
      type: local
      backup_path: /workspace/backups/postgres-dev.sql
    verify_after_restore: true
    retention:
      keep_last: 3
```

### 9.2 List configured restore jobs

```bash
sentinel restore list --config /workspace/infra/dataset/local.yaml
```

### 9.3 Check a job's status

```bash
sentinel restore status pg-test-restore \
  --config /workspace/infra/dataset/local.yaml
```

### 9.4 Dry-run: validate without restoring

Tests connectivity, source availability, and config validity. Does **not** write any data.

```bash
sentinel restore dry-run pg-test-restore \
  --config /workspace/infra/dataset/local.yaml
```

Expected output:
```
Dry-run for restore job 'pg-test-restore': configuration is valid
```

### 9.5 Enable a restore job

```bash
sentinel restore enable pg-test-restore \
  --config /workspace/infra/dataset/local.yaml
```

### 9.6 Pause and resume

```bash
sentinel restore pause pg-test-restore \
  --config /workspace/infra/dataset/local.yaml

sentinel restore resume pg-test-restore \
  --config /workspace/infra/dataset/local.yaml
```

### 9.7 View restore execution history

```bash
# All restore jobs
sentinel restore history --config /workspace/infra/dataset/local.yaml

# Specific job
sentinel restore history pg-test-restore \
  --config /workspace/infra/dataset/local.yaml
```

Restore history status semantics:

- `success`: restore completed and post-restore checks (if configured) passed.
- `failed`: restore execution failed (including integrity/validation/runtime failures).
- `timeout`: restore exceeded `timeout_seconds`.
- `skipped`: restore was intentionally skipped (for example `lock_conflict` or `concurrency_limit_reached`).

Restore notification event mapping:

- `success` status triggers `success` notification events.
- `failed` and `timeout` statuses trigger `failure` notification events.
- `skipped` status triggers `warning` notification events.

### 9.7.1 Advanced restore validation and incremental workflows

Sentinel's advanced restore path supports PostgreSQL PITR requests when the selected backup artifact ships with advanced restore metadata in its manifest.
Incremental restore execution is also implemented for PostgreSQL chain assembly, MySQL/MariaDB binlog replay, and MongoDB oplog replay.

Minimal restore job example:

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

Validation flow:

```bash
# Confirm the restore job is visible and enabled as expected
sentinel restore list --config /workspace/infra/dataset/local.yaml

# Validate the PITR request without applying changes
sentinel restore dry-run postgres-incident-recovery --config /workspace/infra/dataset/local.yaml

# Execute the restore when the manifest advertises a recoverable PITR window
sentinel restore run postgres-incident-recovery --config /workspace/infra/dataset/local.yaml

# Execute an incremental restore when the lineage metadata is executable
sentinel restore run mysql-incremental-recovery --config /workspace/infra/dataset/local.yaml

# Inspect audit fields recorded for the restore
sentinel restore history postgres-incident-recovery --config /workspace/infra/dataset/local.yaml

# Inspect backup history with incremental metadata columns
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 7d
```

Expected outcomes:

- PITR requests without a timezone-aware timestamp fail fast.
- PITR requests outside the manifest recoverable window are rejected before replay.
- `restore history` includes advanced restore mode and planning status fields for operator review.
- PostgreSQL incremental restore assembles ordered chain artifacts before restore execution.
- MySQL and MariaDB incremental restore replay archived binlog artifacts after the base restore; `mysql.binlog_target_time` and `mysql.binlog_target_position` are mutually exclusive.
- MongoDB incremental restore replays archived oplog artifacts after the base restore and requires replica-set oplog access during backup capture.
- MySQL and MariaDB incremental backup requires a readable local or mounted `mysql.binlog_path`.
- Incremental requests without executable lineage support remain blocked unless `confirm_full_fallback: true` is set and a full-restore fallback is acceptable.

### 9.8 Restore from GCS

Add a restore job that points at a GCS object:

```yaml
restores:
  pg-gcs-restore:
    type: postgres
    enabled: true
    host: host.docker.internal
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel_restore
    schedule: "0 3 * * 0"
    keep_file: false
    backup_source:
      type: gcs
      gcs_bucket: ${SENTINEL_GCS_BUCKET}
      gcs_project_id: ${SENTINEL_GCP_PROJECT}
      gcs_credentials_file: ${SENTINEL_GCS_SA_FILE}
      backup_path: postgres-dev_2026-03-15T02-00-00.sql
```

Run it on demand:

```bash
sentinel restore run pg-gcs-restore --config /workspace/infra/dataset/local.yaml
```

Debug a failing restore while retaining the staged download:

```bash
sentinel restore run pg-gcs-restore \
  --config /workspace/infra/dataset/local.yaml \
  --keep-file
```

Expected behavior:

- Sentinel downloads the GCS object to a local staged file.
- The restore runs against the staged file.
- The staged file is deleted after success or failure unless `keep_file: true` or `--keep-file` is used.
- Missing objects surface an actionable restore error instead of a generic storage failure.

---

## 10. Backup Security (Encryption)

Sentinel supports AES-256 encryption of backup artifacts and SHA-256 manifest-based integrity verification.

Activation rule (v1.0.1): encryption is opt-in for config-driven backups.

- No `encryption_key_env`/`encryption_key_file` in config: backup stays plaintext.
- Ambient `SENTINEL_MASTER_KEY` alone does not enable encryption.
- Explicit encryption config with missing/invalid key fails backup at runtime.

### 10.1 Generate a master key

```bash
sentinel security init-key
```

Output (text format):

```text
SENTINEL_MASTER_KEY=<base64-encoded-256-bit-key>
```

Store it in your environment securely:

```bash
export SENTINEL_MASTER_KEY=<value-from-above>
```

### 10.2 Enable encryption in config

```yaml
encryption_key_env: SENTINEL_MASTER_KEY

databases:
  postgres-secure:
    type: postgres
    host: host.docker.internal
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-encrypted
    storage:
      type: local
      local_path: /workspace/backups
```

Run backup with encryption active:

```bash
export SENTINEL_MASTER_KEY=<your-key>
export DEV_POSTGRES_PASSWORD=sentinel

sentinel backup --config /workspace/infra/dataset/local.yaml
```

The backup artifact and its SHA-256 integrity manifest are written to the local path.

### 10.3 Runtime failure behavior for explicit encryption

`sentinel config validate` remains structural and does not require live key material.
Key presence and correctness are enforced when backup executes.

Example failure check:

```bash
unset SENTINEL_MASTER_KEY
sentinel backup --config /workspace/infra/dataset/local.yaml
```

Expected: backup exits with a key-resolution/encryption error and does not silently produce a plaintext success artifact.

### 10.4 Verify backup integrity

Get the backup execution ID from `monitor list`, then:

```bash
sentinel backup verify <execution-id> \
  --config /workspace/infra/dataset/local.yaml
```

Expected:
```
Integrity check passed: SHA-256 fingerprint matches manifest
```

---

## 11. Config Validation and Storage Status

### 11.1 Validate a config file

Always validate before deploying a new config:

```bash
sentinel config validate --config /workspace/infra/dataset/local.yaml
```

### 11.2 Storage backend status

Check that each configured storage backend is reachable and report backup counts and size:

```bash
# Text output (default)
sentinel storage status --config /workspace/infra/dataset/local.yaml --output text

# JSON output (for scripting)
sentinel storage status --config /workspace/infra/dataset/local.yaml --output json
```

Example GCS config snippet for storage status and backup validation:

```yaml
defaults:
  storage:
    type: gcs
    gcs_bucket: ${SENTINEL_GCS_BUCKET}
    gcs_project_id: ${SENTINEL_GCP_PROJECT}
    gcs_credentials_file: ${SENTINEL_GCS_SA_FILE}
```

Recommended GCS checks:

```bash
sentinel storage status --config /workspace/infra/dataset/gcs.yaml --output text
sentinel backup --config /workspace/infra/dataset/gcs.yaml
sentinel monitor list --config /workspace/infra/dataset/gcs.yaml --last 1h
```

Expected outcomes for a healthy GCS setup:

- `storage status` reports the configured GCS backend as reachable.
- Successful backup history records store `file_path` using `gs://bucket/object` format.
- Scheduled retention can remove expired GCS artifacts without marking the original backup run as failed.

---

## 12. Verifying Everything Works

Run through this checklist after each test run:

### Backup artifacts

```bash
ls -lh /workspace/backups/
```

All files should be non-zero in size.

| File | Non-empty? | Content check |
|---|---|---|
| `postgres-dev.sql` | `stat -c%s postgres-dev.sql \| awk '$1>0'` | `head -5` should show `--` PostgreSQL dump header |
| `mysql-dev.sql` | same | `head -5` should show `-- MySQL dump` header |
| `mariadb-dev.sql` | same | `head -5` should show `-- MariaDB dump` header |
| `mongo-dev/` | `find mongo-dev -name "*.bson" | wc -l` should be > 0 | BSON files inside |

Quick one-liner to check all file sizes:

```bash
find /workspace/backups -maxdepth 1 -type f -exec ls -lh {} \; | awk '{print $5, $9}'
```

### Monitor records

```bash
sentinel monitor list \
  --config /workspace/infra/dataset/local.yaml \
  --last 1h \
  --format table
```

Every backup run that used `--config` should show a record with `STATUS=success`.

### SQL content sanity check

```bash
# PostgreSQL: verify it's a valid SQL dump
grep -c "CREATE TABLE\|INSERT INTO\|PostgreSQL database dump" \
  /workspace/backups/postgres-dev.sql

# MySQL
grep -m 1 "MySQL dump" /workspace/backups/mysql-dev.sql

# MariaDB
grep -m 1 "MariaDB dump" /workspace/backups/mariadb-dev.sql
```

---

## 13. Troubleshooting

### `exec: "pg_dump": executable file not found in $PATH`

The host OS client tool is missing. Run the client installation steps in [Section 3](#3-environment-setup).

### `failed to ping database`

1. Check the host/port — from inside the container use `host.docker.internal`, not `localhost`.
2. Verify the DB container is running: `docker ps`.
3. Test raw connectivity: `nc -zv host.docker.internal 5432`.

### `mysqldump: [Warning] Using a password on the command line interface can be insecure`

This is a warning, not an error. The backup succeeds. In this guide, credentials are provided via `password_env` in YAML.

### Backup file is created but is empty (0 bytes)

For PostgreSQL plain format (`pg-out-format: p`), Sentinel captures `pg_dump` stdout and writes it via `WriteBackup`. If the DB is empty (no user tables), the dump header is still written and the file is non-zero. If you see zero bytes, the `pg_dump` process likely exited with an error — check stderr by running with `log_format: text` in the config.

### `error: scheduler not running` on `schedule stop`

`sentinel schedule stop` only works while the scheduler is running in the same process session. For a background run use a process manager (systemd, supervisor) or simply CTRL-C the `schedule start` foreground process.

### Monitor shows no records

Ensure `history_db_path` is set in the YAML config and that the path is writable. Monitor records are written for config-driven runs (`sentinel ... --config ...`).

### `configuration file already has an encryption key set`

`sentinel security init-key` will fail if `SENTINEL_MASTER_KEY` is already set in the environment. Use `--force` to regenerate:

```bash
sentinel security init-key --force
```

---

## Envelope versions (v1 legacy vs v2)

Encrypted backups produced from this release onward carry a 5-byte envelope header on disk: ASCII `SENC` + version byte `0x02`. The manifest mirrors this as `encryption.envelope_version = 2`.

### Identify the version of an artifact

```bash
xxd -l 5 /path/to/backup.enc
# v2 → 00000000: 5345 4e43 02
# legacy → anything else (typically a small uint32-le length such as 1000 1000)
```

### Recovering a legacy (pre-v2) artifact

`sentinel restore` and `sentinel backup verify` refuse legacy artifacts by default. To recover plaintext for one-shot re-encryption:

```bash
SENTINEL_ALLOW_LEGACY_ENVELOPE=1 ./sentinel restore run <job> --allow-legacy-envelope
# Emits a WARNING line to stderr and a `crypto.legacy_envelope_decrypt` log record.
```

Treat the recovered plaintext as **recovered, not safe to keep** — re-encrypt from source with the current Sentinel build.

---

*For questions or issues, open a ticket at the project repository.*
