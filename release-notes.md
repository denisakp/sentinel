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

### V1 Consolidation (Internal)

**Schema Migration Visibility**:
- Embedded SQL migration system for history database schema management
- `sentinel db migrate status` command to inspect migration state
- Automatic migration application on startup with fail-fast behavior
- Schema versioning with checksums for tamper detection
- Migrations tracked in `schema_migrations` table

**Atomic Execution State Management**:
- Extended execution status values: `pending`, `running`, `completed`, `failed`, `interrupted`
- Atomic cleanup tracking with `finished_at`, `cleanup_attempted`, `cleanup_succeeded`, `cleanup_error` columns
- Interrupted execution reconciliation on scheduler restart (marks stale `running` records as `interrupted`)
- Cleanup result recording for all failure scenarios
- Atomic status transitions to prevent partial state corruption

**End-to-End Integration Testing**:
- Docker-backed integration tests using testcontainers-go
- Per-engine test coverage: PostgreSQL, MySQL, MariaDB, MongoDB
- Required scenarios per engine: backup, restore, failure injection, dry-run
- Build tag isolation (`//go:build integration`) to separate unit and integration tests
- CI workflow for automated integration testing via `make test-integration`
- Migration failure integration test to verify fail-fast behavior

**Behavior Changes**:
- `sentinel schedule start` now applies pending migrations automatically before starting scheduler
- Scheduler startup fails immediately if migrations cannot be applied (no degraded mode)
- All `running` executions from crashed scheduler processes are marked as `interrupted` on restart
- Export commands (JSON, CSV) now include new status values and cleanup columns
