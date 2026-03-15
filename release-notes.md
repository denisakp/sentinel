# Sentinel Release Notes

## [v1.1.0] - March 15, 2026

### Added

- Google Cloud Storage (`gcs`) is now a first-class storage backend for config-driven and one-off backup flows.
- `sentinel restore run` now supports restore jobs backed by GCS artifacts.
- Restore jobs can retain the staged download for debugging with `keep_file: true` or `--keep-file`.

### Changed

- Successful GCS backups now record canonical `gs://bucket/object` monitor paths.
- GCS authentication prefers `gcs_credentials_file` when configured and otherwise falls back to Application Default Credentials.
- GCS initialization errors are sanitized so credential material is not leaked in returned error strings.

### Retention

- Scheduled retention now deletes expired GCS artifacts and keeps record cleanup consistent with object deletion.
- Retention cleanup remains warning-level and non-fatal for scheduled backup success reporting.

### Restore

- Restore downloads from GCS now produce actionable missing-object errors.
- Staged restore files are cleaned after restore attempts by default.

### Tests

- Added GCS backend coverage for upload, list, exists, delete, ADC fallback, credential precedence, and sanitized auth failures.
- Added restore coverage for GCS flag wiring, staged cleanup, keep-file mode, and missing-object error handling.
- Added retention coverage for GCS delete behavior and scheduled warning-path regressions.
- Full repository quality gates passed: `go test ./...` and `go vet ./...`.

## [v1.0.6] - March 15, 2026

### Changed

- Scheduler-driven backups now generate unique artifact names per successful run even when `output` is fixed in YAML.
- Scheduled artifact naming uses prefix + UTC second timestamp with same-second collision suffix (`-N`) and preserves existing database-format extension semantics.
- Configured outputs that already include canonical extensions are normalized in scheduled flow to prevent double-extension artifacts.

### Retention

- Backup retention is now evaluated automatically after each successful scheduled backup run.
- Retention policy keeps cumulative semantics when both `keep_last` and `keep_days` are configured.
- Retention cleanup failures are surfaced as warnings and do not flip successful backup execution status to failed.

### Observability

- Scheduled backup monitor records continue to store artifact paths corresponding to each unique generated artifact per run.

### Tests

- Added scheduler naming regressions for fixed-output uniqueness, empty-output default naming, and canonical extension normalization.
- Added retention cumulative-policy regression coverage and warning-path coverage for unsupported retention delete backends.
- Added CLI scheduler-mode orchestration tests for scheduled naming and post-success retention warning behavior.
- Full repository quality gates passed: `go test ./...` and `go vet ./...`.

## [v1.0.5] - March 14, 2026

### Changed

- `schedule status` now declares a required positional argument in usage: `status <job-name>`.
- `schedule status` help now includes an explicit valid example with a concrete job name.
- Missing or extra positional arguments now fail through standard Cobra argument validation for consistent operator feedback.

### Stability

- Valid `schedule status <job-name> --config <path>` behavior is preserved, including status field output (`Job`, `Schedule`, `Next Execution`, `Last Execution`, `Last Status`).
- Unknown-job runtime errors remain distinct from missing-argument validation errors.

### Tests

- Added scheduler regression coverage for help usage contract, help examples, missing/extra argument validation, and valid single-argument success path.
- Added command-level observability tests for usage metadata, arg validator behavior, and unknown-job distinction.
- Full repository quality gates passed: `go test ./...` and `go vet ./...`.

## [v1.0.3] - March 14, 2026

### Changed

- Config-driven CLI commands now resolve configuration with a shared order: explicit `--config` first, otherwise `./sentinel-config.yaml`.
- Legacy per-command fallback behavior was removed in favor of one consistent resolver across command families.
- `backup` now surfaces actionable missing-config guidance when neither `--config` nor `./sentinel-config.yaml` is available.

### Added

- Shared CLI resolver helpers in `internal/cli/config_resolver.go` for full-validation and minimal-validation loading flows.
- Default config discovery support for these command families:
  - `backup`
  - `restore` (`list`, `status`, `enable`, `disable`, `dry-run`, `history`, `pause`, `resume`)
  - `schedule` (`start`, `list`, `status`)
  - `monitor` (`list`, `stats`, `show`, `export`)
  - `retention` (`apply`, `preview`)
  - `config validate`
  - `db migrate status` (minimal validation path)
  - `storage status`

### Developer Experience

- `config validate` and `db migrate status` no longer require `--config` when `./sentinel-config.yaml` is present.
- `restore --config` help text was updated to remove required-only wording and match new default discovery behavior.

### Tests

- Added unit tests for resolver precedence and missing-file guidance in `internal/cli/config_resolver_test.go`.
- Added integration regression coverage for default discovery, explicit override precedence, and missing-config error behavior in `tests/integration/default_config_path_integration_test.go`.
- Full repository test suite (`go test ./...`) passes with the new resolver flow.

## [v1.0.2] - March 13, 2026

### Changed

- `monitor list` default table columns now display `ID | JOB | STATUS | TIMESTAMP | DURATION | ERROR`.
  The `ID` is the first column, enabling direct handoff to `monitor show --id <ID>` for failure triage.
- `monitor list` table `ERROR` column shows a deterministic 48-character truncated preview. Full error text is preserved in JSON/CSV exports.
- `monitor stats` no longer requires `--job`. Omitting `--job` returns one combined aggregate statistics summary across all jobs.
- `schedule list` default table no longer includes `LAST STATUS`. Use `--format json` to access `last_status` in machine-readable output.
- `schedule list` and `monitor list` output uses aligned fixed-width table formatting for terminal readability.

### Added

- `sentinel schedule list --format json` for machine-readable schedule output including `last_status`.
- Empty-result `monitor list` and `monitor stats` queries now exit with code `0` and print explicit `no matching records` message instead of producing empty output.

### Tests

- Regression coverage for monitor list column order, ID handoff, error truncation determinism, aggregate stats, and empty-result semantics.
- Regression coverage for schedule list `LAST STATUS` removal and JSON machine-readable compatibility.
- Regression coverage ensuring full error messages are preserved in JSON/CSV export after table truncation was introduced.

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
