# Sentinel

**Status: v1.0.0 released**

Sentinel is an open-source, cloud-native database backup and restore tool for SQL and NoSQL databases.
It supports local and cloud storage, scheduled operations, monitoring, retention, and notifications.

## Project Purpose

Sentinel simplifies database backup, restore, and operational automation for teams running in local, Docker,
and Kubernetes environments.

## Key Features

- **Backup and Restoration** for SQL and NoSQL databases (PostgreSQL, MySQL, MariaDB, MongoDB).
- **Storage Support** for local, S3-compatible, Google Cloud Storage, Google Drive, and Azure Blob.
- **Notification System** for real-time backup and restore alerts (Slack, Discord, webhook, SMTP).
- **Scheduling and Automation** through cron-based schedules for both backups and restores.
- **Backup & Restore History** with SQLite-backed execution records and exports.
- **Restore Management** with dry-run testing, post-restore verification, and automated scheduling.
- **Retention Policies** for automatic cleanup of old backups and restore execution records.
- **Cross-Platform Compatibility**: Built with Go, Sentinel works seamlessly in Docker, Kubernetes, and other
  cloud-native environments.

## Features Overview

**Implemented Features**:

- [x] Backup functionality for PostgreSQL, MySQL, MariaDB, and MongoDB databases.
- [x] Restore functionality for PostgreSQL, MySQL, MariaDB, and MongoDB databases.
- [x] Local, S3-compatible, Google Cloud Storage, Google Drive, and Azure Blob storage backends.
- [x] YAML configuration for multi-job backups and restores with defaults.
- [x] Cron-based scheduling for backups and restores (`sentinel schedule`).
- [x] Retention policies for backups and restore history (`sentinel retention`).
- [x] Notifications for backup and restore operations (Slack, Discord, webhook, email).
- [x] Backup and restore execution history with monitoring (`sentinel monitor`).
- [x] Restore management CLI (`sentinel restore`) with dry-run, enable/disable, and status.
- [x] Post-restore verification with automated testing workflows.

## Installation

Sentinel v1.0.0 is available from source. Clone the repository and build locally.
A Go development environment (v1.18+) is required.

1. **Clone the Repository**:

   ```bash
   git clone https://github.com/denisakp/sentinel.git
   cd sentinel
   ```

2. **Build the Project**:

   ```bash
   go mod download 
   go build -o sentinel
   ```

3. **Run Sentinel**:
   Sentinel supports both backup and restore operations. To create a backup of your PostgreSQL database, for
   example, run:

   ```bash
   ./sentinel backup --type postgres --host mydb.host.tld --port 5432 --user my-user --password 1234 --database sample
   ```

  Use `sentinel --help` to see all available commands including `backup`, `restore`, `schedule`, `monitor`,
  `retention`, `config`, and `db`.

> **Note**: Prebuilt release distribution guidance may be expanded in future versions.

## Usage

Sentinel supports CLI-only operations and YAML-driven workflows. YAML is recommended for multi-job setups with 
scheduling, notifications, and automated restore testing.

### YAML-Driven Backup & Restore (Recommended)

Create a `sentinel.yaml`:

```yaml
version: "1.0"
log_format: json

defaults:
  schedule: "0 2 * * *"  # Optional shared cron for backup jobs without local schedule
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
    schedule: "15 2 * * *"  # Explicit schedule overrides defaults.schedule

restores:
  test-restore:
    type: postgres
    enabled: false  # Safety: disabled by default
    host: localhost
    port: 5432
    username_env: TEST_DB_USER
    password_env: TEST_DB_PASSWORD
    database: test_db
    schedule: "0 3 * * 0"  # Weekly on Sunday
    backup_source:
      type: local
      backup_path: ./backups/prod-postgres-latest.sql
    verify_after_restore: true
    retention:
      keep_last: 10
      keep_days: 30
```

Schedule defaulting behavior for backups:

- `defaults.schedule` is inherited by backup jobs that omit `schedule`.
- Job-level `schedule` always takes precedence over `defaults.schedule`.
- `defaults.schedule` applies only to backup jobs (`databases`), not restore jobs.

Encryption is explicit opt-in for config-driven backups:

- If neither `encryption_key_env` nor `encryption_key_file` is set, backups remain plaintext.
- `SENTINEL_MASTER_KEY` in the shell does nothing unless referenced by config.
- If encryption is explicitly configured but key material is missing/invalid, backup fails (no plaintext fallback).

Run backups:

```bash
./sentinel backup --config sentinel.yaml
```

Scheduler commands (backups and restores):

```bash
# Start scheduler for both backups and restores
./sentinel schedule start --config sentinel.yaml

# List all scheduled jobs (backups and restores)
./sentinel schedule list --config sentinel.yaml

# Check specific job status
./sentinel schedule status prod-postgres --config sentinel.yaml
```

Restore management commands:

```bash
# List all restore jobs
./sentinel restore list --config sentinel.yaml

# Enable a restore job
./sentinel restore enable test-restore --config sentinel.yaml

# Test restore without applying (dry-run)
./sentinel restore dry-run test-restore --config sentinel.yaml

# Run a restore job immediately
./sentinel restore run test-restore --config sentinel.yaml

# View restore execution history
./sentinel restore history --config sentinel.yaml

# Check restore job status
./sentinel restore status test-restore --config sentinel.yaml
```

Monitoring and retention:

```bash
# View backup history
./sentinel monitor list --config sentinel.yaml --last 7d

# View restore history
./sentinel monitor list --type restore --config sentinel.yaml

# Preview retention cleanup
./sentinel retention preview --config sentinel.yaml

# Apply retention policies
./sentinel retention apply --config sentinel.yaml
```

### CLI-Only Backup

Note: Encryption opt-in semantics apply to config-driven backup flows (`--config`).
Imperative flag-only backup mode remains unchanged in this patch.

The `backup` command supports backup operations for:

- **PostgreSQL**
- **MySQL**
- **MariaDB**
- **MongoDB**

Example usage:

```bash
./sentinel backup --type mysql --host mydb.host.tld --port 3307 --user my-user --password 1234 --database sample
```

For additional options, run:

```bash
./sentinel backup -h
```

### Restore Operations

The `restore` command provides comprehensive restore management:

**Available Commands:**

- `list` - View all configured restore jobs
- `status <job>` - Check specific restore job status and configuration
- `enable <job>` - Enable a restore job for scheduled execution
- `disable <job>` - Disable a restore job
- `dry-run <job>` - Test restore configuration without applying changes
- `run <job>` - Execute a restore job immediately
- `history [job]` - View restore execution history
- `pause <job>` - Temporarily pause a restore job
- `resume <job>` - Resume a paused restore job

**Example usage:**

```bash
# List all restore jobs from config
./sentinel restore list --config sentinel.yaml

# Enable a restore job for scheduling
./sentinel restore enable weekly-test-restore --config sentinel.yaml

# Test restore configuration (dry-run)
./sentinel restore dry-run weekly-test-restore --config sentinel.yaml

# Run a restore job immediately
./sentinel restore run weekly-test-restore --config sentinel.yaml

# View restore history
./sentinel restore history --config sentinel.yaml
```

**Key Features:**

- **Safety First**: All restore jobs are disabled by default
- **Post-Restore Verification**: Automatic data validation after restore
- **Flexible Scheduling**: Cron-based automated disaster recovery testing
- **Conflict Management**: Configure behavior when data exists (ignore/replace/error)
- **Multi-Source Support**: Restore from local, S3, Google Cloud Storage, Google Drive, or other storage backends
- **Execution Tracking**: Full history of restore operations with success/failure status

For GCS restore jobs, Sentinel downloads the selected object to a local staged file before restore execution.
The staged file is removed after the attempt by default, or retained when `keep_file: true` is configured or `--keep-file` is passed to `sentinel restore run`.

---

## Database Migration & Reliability

Sentinel uses an embedded schema migration system to manage its SQLite history database. Migrations ensure backward compatibility and safe schema evolution.

### Automatic Migration on Startup

All commands that interact with the history database (`schedule`, `monitor`, `retention`) automatically apply pending migrations on startup. If a migration fails, the process exits immediately (fail-fast behavior) to prevent operating with an inconsistent schema.

### Checking Migration Status

Use the `db migrate status` command to inspect the current migration state:

```bash
./sentinel db migrate status
```

**Output includes:**

- Current applied migration version
- Latest available migration version
- Whether the database is up-to-date
- Full list of applied migrations with timestamps

**Example Output:**

```txt
Migration Status:

Current Version: 3
Latest Version:  3
Status: ✓ Up-to-date

Applied Migrations:
  Version 1: baseline_schema (applied at 2024-01-15 10:30:00)
  Version 2: add_cleanup_columns (applied at 2024-01-15 10:30:01)
  Version 3: add_consolidated_status_values (applied at 2024-01-15 10:30:02)
```

### Migration Reliability Guarantees

1. **Atomic Application**: Each migration runs in a transaction; failures roll back completely.
2. **Idempotency**: Migrations that have already been applied are skipped (tracked via `schema_migrations` table).
3. **Fail-Fast**: If any migration fails, the application exits before executing any operations.
4. **Version Checksums**: Each migration file has a checksum to detect tampering or corruption.

### Troubleshooting Migrations

If a migration fails:

1. Check the error message for specific SQL syntax or constraint violations.
2. Inspect the `schema_migrations` table in the SQLite database to see which migrations succeeded.
3. If the database is corrupted, delete it and restart (Sentinel will rebuild from scratch).
4. For production environments, always back up the history database before upgrading Sentinel.

**Note:** The history database (`history.db`) only stores execution metadata (backup/restore records), not actual backup artifacts. It is safe to delete and rebuild if needed.

---

## Contributions

Sentinel is under active development, and we welcome contributions from the community! To get started, please review the
following resources:

- [CONTRIBUTING.md](CONTRIBUTING.md): Guidelines for contributing, including setup instructions, coding standards, and
  our pull request process.
- [SECURITY.md](SECURITY.md): Important information on reporting security vulnerabilities responsibly.
- **Issue Templates**:
  - [Feature Request](.github/ISSUE_TEMPLATE/2-feature-request.md): To suggest new features.
  - [Bug Report](.github/ISSUE_TEMPLATE/1-bug.md): To report bugs or issues.
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md): Community standards for respectful and inclusive collaboration.

Thank you for helping improve Sentinel.
