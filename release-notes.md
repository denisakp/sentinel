# Sentinel Release Notes

## [v1.0.0] - February 14, 2026

### Added

- Complete backup and restore system for PostgreSQL, MySQL, MariaDB, and MongoDB
- YAML-driven configuration with global defaults and environment variable support
- Cron-based scheduling for automated backups and restores
- Execution history and monitoring with SQLite backend
- Retention policies for automatic cleanup
- Multi-storage support: Local, AWS S3, Google Drive
- Notification system: Slack, Discord, webhooks, SMTP email
- Restore management: dry-run, verification, scheduled execution
- CLI commands: `backup`, `restore`, `schedule`, `monitor`, `retention`, `config`
- Production-optimized binary (27MB, 28% smaller than debug build)
- Comprehensive build system with multiple variants and Docker support
