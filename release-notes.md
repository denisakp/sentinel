# Sentinel Release Notes

## [v1.0.0] - February 14, 2026

### Added

- Backup and restore for PostgreSQL, MySQL, MariaDB, and MongoDB
- YAML configuration with defaults and environment variable support
- Cron scheduling for backups and restores
- Monitoring and execution history with SQLite
- Retention policies for automated cleanup
- Storage backends: Local, S3-compatible, Google Drive, Azure Blob
- Notifications: Slack, Discord, webhook, SMTP email
- Restore management (list, enable/disable, dry-run, history, status)
- Core CLI commands: `backup`, `restore`, `schedule`, `monitor`, `retention`, `config`, `db`

### V1 Consolidation (Internal)

- Embedded database migration system for monitor/history schema
- `sentinel db migrate status` for migration visibility
- Pending migrations applied automatically on scheduler/monitor/retention startup
- Fail-fast startup if migration fails
- Consolidated execution statuses: `pending`, `running`, `completed`, `failed`, `interrupted`
- Cleanup lifecycle fields in history records: `finished_at`, `cleanup_attempted`, `cleanup_succeeded`, `cleanup_error`
- Startup reconciliation marks stale `running` executions as `interrupted`
- Integration test coverage for backup/restore flows and migration behavior
