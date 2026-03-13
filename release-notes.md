# Sentinel Release Notes

## [v1.0.1] - March 13, 2026

### Changed

- Backup encryption is now explicit opt-in for config-driven backups.
- Sentinel no longer implicitly sets `encryption_key_env` when encryption fields are absent in YAML.
- Ambient `SENTINEL_MASTER_KEY` alone no longer changes backup encryption mode.

### Security

- When explicit encryption is configured and key material is missing/invalid, backup now fails with actionable error output.
- Key source precedence remains deterministic: `encryption_key_env` first, `encryption_key_file` fallback when env value is empty.

### Tests

- Added regression coverage for plaintext-by-default behavior, explicit encryption success/failure paths, and restore compatibility for encrypted artifacts.

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
