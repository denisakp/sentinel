# Sentinel Release Notes

## [Unreleased]

### Deprecated

- **`sentinel backup --password` / `-p`**: deprecated; will be removed in the next minor release. Passing a password on the command line exposes it via `ps`, `/proc/<pid>/cmdline`, and shell history. Use one of three safe channels instead: `--password-env <VAR>`, `--password-file <PATH>` (first line, right-trimmed; warns on group/world-readable mode), or the existing config field `databases.<id>.password_env`. CLI flag precedence over config is preserved (silent override). Conflicts among `--password`, `--password-env`, `--password-file` are hard errors before any DB I/O. Migration recipes: [`docs/runbooks/credentials.md`](docs/runbooks/credentials.md). (Feature 015)

### Security

- **mariadb dump**: migrated MariaDB backup adapter (`pkg/backup/mariadb_dump`) from `--password=<value>` argv to `MYSQL_PWD` env injection, closing the last argv-leak path across supported engines. PostgreSQL (`PGPASSWORD`), MySQL (`MYSQL_PWD`), and MongoDB (URI) channels unchanged. Verified via argv-inspection unit tests in `pkg/backup/mariadb_dump/args_builder_test.go` and `pkg/backup/mariadb_dump/env_injection_test.go`. (Feature 015)
- **dump adapters**: redact credentials embedded in subprocess stderr before they reach returned errors, logs, the monitor SQLite store, or notifier payloads. Previously, a failing `pg_dump` / `pg_dumpall` / `mysqldump` / `mariadb-dump` / `mongodump` (incl. oplog) invocation could leak `PGPASSWORD=…`, `MYSQL_PWD=…`, `MONGO_INITDB_ROOT_PASSWORD=…`, libpq `password = …`, `--password=…`, `-p<value>`, or URI userinfo (`scheme://user:secret@host`) into operator-visible output. All eight dump-adapter callsites now route stderr through `sanitize.RedactStderr`, which caps the embedded buffer at 64 KiB with an explicit truncation marker. CI gate (`make lint-redact-stderr`, wired into `.github/workflows/integration.yml`) prevents regression. Severity: medium (local-file disclosure on failure); no behavior change on successful backups. Audit artifact: `docs/audit/stderr-redaction-audit.md`. (Feature 012)

### Added

- **monitor schema migration framework**: monitor history DB now carries an explicit `schema_version` integer that is gated on every open. Stale DBs are migrated under a cross-process file lock (`<db>.migrate.lock` via `internal/lock`); forward-incompatible DBs (`current > BinarySchemaVersion`) are refused before any read/write with `monitor.ErrForwardIncompatible`, surfaced at the CLI with a what/why/how block and a non-zero exit. The previous silent legacy-fallback INSERT path in `RecordRestoreExecution` is removed — restore rows always carry `restore_mode` / `planning_status`. New `sentinel monitor doctor [--repair] [--json]` inspects state with stable exit codes (`0/1/2/3/4` for current/stale/forward-incompat/missing/corrupt) and idempotent migration application. Runbook: [`docs/runbooks/monitor-schema-migration.md`](docs/runbooks/monitor-schema-migration.md). (PRD-11 / spec 017)

### Fixed

- **scheduler**: connectivity-check close errors no longer terminate the process. SQL (`internal/backup/sql.PingSqlDatabase`) and Mongo (`internal/backup/mongo.CheckConnectivity`) helpers now propagate close/disconnect errors through the existing retry path instead of calling `log.Fatalf` / `log.Panic`. A `golangci-lint` `forbidigo` rule and a CI job (`.github/workflows/lint.yml`) prevent reintroduction of `log.Fatal*`, `log.Panic*`, and library-side `os.Exit`. `backup verify` 2/3/4 exit-code contract preserved via `internal/cli/exit_codes.go`. (PRD-03 / spec 016)
- **tls (MariaDB)**: mutual TLS now works. The MariaDB arg builder previously emitted `--ssl-cert=<path>` but silently dropped the configured `client_key`, breaking the handshake against any MariaDB server requiring X509 client auth (or, with a permissive server, falling back to a non-mTLS connection without operator notice). The builder now also emits `--ssl-key=<path>` whenever `tls.client_key` is set. The pair-validation in `internaltls.Config.Validate()` continues to reject half-configured mTLS at config load. PostgreSQL and MySQL audited and confirmed correct (no change). PRD 05.
- **scheduler**: bounded executor no longer leaks slots on worker panic; panics now appear in monitor history with a `worker panic: ` error-message prefix and are fed through the retry policy as ordinary failures (PRD 09, ADR 0008 promoted to Accepted).
- **restore (optional sidecar)**: optional `.manifest.json` sidecars no longer fail the restore when absent. `downloadOptionalSourceObject` in `internal/restore/source.go` previously returned the same `error` for not-found and for transport/permission failures, leaving callers to disambiguate via `errors.Is(err, ErrSourceObjectNotFound)`. The function now returns `(found bool, err error)`: absent ⇒ `(false, nil)`; transport/permission/cancellation ⇒ `(false, wrappedErr)`; present ⇒ `(true, nil)`. Call sites in `StageRestoreSource` and `StageChainArtifacts` updated; mandatory-path behaviour and the `internal/cli/restore.go` operator-facing handling of `ErrSourceObjectNotFound` unchanged. PRD 12.
- **lock**: closed the TOCTOU window in stale-lock detection. Acquisition now layers a kernel-enforced advisory `flock(2)` over the PID file and enforces the dual stale criterion (PID-dead AND age > threshold) inside the package. New typed errors (`ErrLockHeld`, `ErrLockUnsupported`, `ErrLockIO`) plus three acquisition modes (non-blocking, blocking-with-context, bounded-wait); on-disk v1 lock file format unchanged. POSIX-only (Linux + macOS) — non-POSIX targets return a clear "unsupported platform" error at first call. PRD 10, ADR 0007 promoted to Accepted.

### Security Advisory — Envelope v2

- **Scope**: All encrypted backups produced before this version (Sentinel ≤ v1.1.1) used the v1 envelope, which lacked an on-disk version byte and relied on a streaming nonce scheme whose contract was not enforced by code-level guards.
- **Impact**: The nonce-reuse risk class affects AES-GCM confidentiality. Operators MUST treat pre-v2 ciphertexts as potentially-weakened.
- **Default behavior**: From this version on, `sentinel restore` and `sentinel backup verify` refuse pre-v2 (legacy) artifacts. New encrypted backups carry the v2 envelope header (`SENC` + version byte `0x02`) on disk and `encryption.envelope_version = 2` in the manifest.
- **Opt-in flag**: `--allow-legacy-envelope` (env: `SENTINEL_ALLOW_LEGACY_ENVELOPE=1`) lets operators decrypt legacy artifacts at their own risk. A loud WARNING is printed to stderr and a structured `crypto.legacy_envelope_decrypt` log line is emitted per opt-in decryption.
- **Recommended remediation**: Re-encrypt prior backups from source. A dedicated `sentinel security reencrypt` helper is tracked under a separate PRD.
- **Inspecting an artifact**: `xxd -l 5 backup.enc` — v2 starts with `53 45 4E 43 02`; anything else is legacy.

## [v1.1.1] - March 20, 2026

### Restore Observability

- `sentinel restore history` now reads real restore execution records from monitor history instead of placeholder output.
- Restore history status values are normalized for operators: `success`, `failed`, `timeout`, `skipped`.
- Restore monitor query support now includes filtered restore execution listing with restore-specific fields.

### Restore Notifications

- Manual `sentinel restore run` now dispatches restore notifications using configured restore notification channels.
- Restore execution statuses are mapped to notification events consistently:
  - `success` -> `success`
  - `failed` and `timeout` -> `failure`
  - `skipped` -> `warning`

### Tests

- Added CLI coverage for restore notification dispatch across success, failed, timeout, and skipped outcomes.
- Added CLI restore history observability coverage for status normalization.
- Added integration coverage combining restore monitor history recording with restore notification delivery.

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
