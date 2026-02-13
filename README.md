# Sentinel

**Status: Project in Development 🚧**

Sentinel is an open-source, cloud-native database backup and restoration tool designed for seamless management of SQL
and NoSQL databases in Docker, Kubernetes, and local environments. Currently, Sentinel is under development, so certain
features are incomplete, and documentation will be continuously updated.

## Project Purpose

Sentinel simplify the backup, restoration, and managemet of databases with sipport for local storage,
scheduled backups and restores, secure encryption, and multiple notification channels. It's designed to provide database
administrators and developers with a flexible, reliable solution for database continuity and disaster recovery testing.

## Key Features

- **Backup and Restoration** for SQL and NoSQL databases (PostgreSQL, MySQL, MariaDB, MongoDB).
- **Storage Support** for multiple environments, including local, S3-compatible, and Google Drive storage.
- **Notification System** for real-time backup and restore alerts (Slack, Discord, webhook, SMTP).
- **Scheduling and Automation** through cron-based schedules for both backups and restores.
- **Backup & Restore History** with SQLite-backed execution records and exports.
- **Restore Management** with dry-run testing, post-restore verification, and automated scheduling.
- **Retention Policies** for automatic cleanup of old backups and restore execution records.
- **Enhanced Security** with backup file encryption (AES 256) and integrity verification using hash checks (upcoming).
- **Cross-Platform Compatibility**: Built with Golang, Sentinel works seamlessly in Docker, Kubernetes, and other
  cloud-native environments.

## Features Overview

**Implemented Features**:

- [x] Backup functionality for PostgreSQL, MySQL, MariaDB, and MongoDB databases.
- [x] Restore functionality for PostgreSQL, MySQL, MariaDB, and MongoDB databases.
- [x] Local, S3-compatible, and Google Drive storage backends.
- [x] YAML configuration for multi-job backups and restores with defaults.
- [x] Cron-based scheduling for backups and restores (`sentinel schedule`).
- [x] Retention policies for backups and restore history (`sentinel retention`).
- [x] Notifications for backup and restore operations (Slack, Discord, webhook, email).
- [x] Backup and restore execution history with monitoring (`sentinel monitor`).
- [x] Restore management CLI (`sentinel restore`) with dry-run, enable/disable, and status.
- [x] Post-restore verification with automated testing workflows.

**Upcoming Features**:

- [ ] Security Enhancements: Hash verification and AES-256 encryption.
- [ ] Advanced restore options: Point-in-time recovery and incremental restores.

## Installation

At this stage, Sentinel has not yet reached an initial release, so the only way to use it is by cloning the repository
and building the project locally. A Go development environment (v1.18+) is required for building Sentinel.

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
   Use `sentinel --help` to see all available commands including `backup`, `restore`, `schedule`, `monitor`, and `retention`.

> **Note**: When the initial release (v1.0) is available, this README will be updated with more user-friendly
> installation options and instructions.

## Usage

Sentinel supports CLI-only operations and YAML-driven workflows. YAML is recommended for multi-job setups with scheduling, notifications, and automated restore testing.

### YAML-Driven Backup & Restore (Recommended)

Create a `sentinel.yaml`:

```yaml
version: "1.0"
log_format: json

defaults:
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
    schedule: "0 2 * * *"

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

# View restore history
./sentinel restore history --config sentinel.yaml
```

**Key Features:**
- **Safety First**: All restore jobs are disabled by default
- **Post-Restore Verification**: Automatic data validation after restore
- **Flexible Scheduling**: Cron-based automated disaster recovery testing
- **Conflict Management**: Configure behavior when data exists (ignore/replace/error)
- **Multi-Source Support**: Restore from local, S3, Google Drive, or other storage backends
- **Execution Tracking**: Full history of restore operations with success/failure status

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

Thank you for helping improve Sentinel!

Stay tuned for more updates, and thank you for your interest in making Sentinel a reliable tool for database continuity!

---

