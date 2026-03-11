# Sentinel AI Coding Assistant Instructions

Sentinel is a Go CLI for backup and restore across PostgreSQL, MySQL, MariaDB, and MongoDB.
It supports YAML-driven operations, scheduled execution, retention, monitoring, notifications, security controls, and multi-backend storage.

As of March 2026, this repository has moved beyond "new YAML feature" phase. Treat the YAML/scheduler stack as established and prioritize roadmap-driven incremental improvements.

## Current Product Status (Source of Truth)

Check `docs/roadmap/ROADMAP.md` first for milestone order and delivery state.

Implemented and available now:
- Backup and restore for PostgreSQL, MySQL, MariaDB, MongoDB
- YAML configuration with defaults, env interpolation, and validation
- Cron-based scheduling for backups and restores
- Retention management and cleanup
- Monitoring/history (SQLite) and stats/export paths
- Notifications (Slack, Discord, Email, Webhook)
- Security foundation: sanitization, hash/encryption utilities, TLS config/probe support
- Storage backends: local, S3-compatible, Google Drive, Azure Blob

Roadmap direction:
- `v1.1.0`: MySQL/MariaDB compression (next)
- `v1.2.0`: advanced restore options (PITR/incremental)
- `v1.3.0`: security and reliability hardening completion
- `v2.0.0`: enterprise scale and performance

## Repository Structure (Use Actual Paths)

Primary packages:
- CLI commands: `internal/cli/`
- Config loading/validation/types: `internal/config/`
- Scheduler/executor: `internal/scheduler/`
- Retention: `internal/retention/`
- Monitor/history: `internal/monitor/`
- Storage abstraction/backends: `internal/storage/`
- Security helpers: `internal/crypto/`, `internal/sanitize/`, `internal/tls/`, `internal/manifest/`
- Backup engines: `pkg/backup/{pg_dump,mysql_dump,mariadb_dump,mongo_dump}/`
- Restore engines: `pkg/restore/...`

Specs and planning artifacts:
- Active roadmap docs: `docs/roadmap/`
- Feature specs/contracts: `specs/`

Do not reference `cmd/` paths for Sentinel CLI orchestration in this repo unless a specific file actually exists there.

## Architecture and Reuse Rules

Prefer extension over duplication:
- Reuse existing DB backup implementations under `pkg/backup/*`
- Reuse storage factory/validation in `internal/storage/`
- Reuse config interpolation/validation logic in `internal/config/`
- Reuse monitor recorder/query primitives in `internal/monitor/`

When adding behavior:
- Wire YAML -> typed args in `internal/config/marshal.go`
- Validate options in `internal/config/validator.go`
- Execute via existing backup/restore package entrypoints
- Record executions via monitor package

## Data Flow Expectations

CLI-driven flow:
1. Parse flags in `internal/cli/*.go`
2. Validate DB/storage/config
3. Build engine args (`internal/config/marshal.go` and package-specific builders)
4. Run dump/restore tool wrappers in `pkg/backup/*` or `pkg/restore/*`
5. Persist artifacts through storage backends
6. Record monitor history and emit notifications

YAML-driven flow:
1. `internal/config/loader.go` parses YAML and resolves `${ENV_VAR}`
2. `internal/config/validator.go` enforces schema/rules
3. Scheduler/CLI executes enabled jobs
4. Retention and monitor logic run per configured policy

## Storage Backends and Configuration

Supported backends in current code:
- `local`
- `s3` (S3-compatible, including MinIO-style endpoints)
- `google-drive`
- `azure`

Google Drive remains supported and wired through:
- `internal/storage/gdrive/`
- `internal/storage/storage.go`
- `internal/cli/backup.go` flags `--gdrive-folder-id` and `--gdrive-sa-file`

## Security and Credential Conventions

Required practices:
- Prefer `_env` fields for secrets in YAML (`password_env`, cloud credential env vars)
- Do not introduce new plaintext secret fields unless absolutely required
- Keep password passing via process environment where supported (`PGPASSWORD`, `MYSQL_PWD`)
- Use sanitization utilities before logging args or sensitive values
- Wrap errors with `%w` and context (`fmt.Errorf("...: %w", err)`)

Restore pre-flight integrity/decryption uses shared logic in `internal/restore/pipeline.go`.

## Testing Conventions

Prefer table-driven tests with explicit failure cases.

When changing config/scheduler/security/storage behavior, add or update:
- Unit tests near modified package
- Integration tests under `tests/` when cross-package behavior changes

Avoid `time.Sleep()` in scheduler tests; use deterministic clock/mocking patterns where available.

## Feature-Specific Guidance (Near-Term)

For `v1.1.0` compression work:
- Add MySQL/MariaDB compression options in config types/validation
- Map options through `internal/config/marshal.go`
- Extend args builders in `pkg/backup/mysql_dump` and `pkg/backup/mariadb_dump`
- Add tests for option validation and generated dump args

For advanced restore/security follow-ups:
- Check existing restore scheduling/execution integration before adding new plumbing
- Prefer extending monitor schema/queries over parallel ad-hoc tracking

## Critical Files to Read First

- `docs/roadmap/ROADMAP.md`
- `internal/cli/backup.go`
- `internal/cli/schedule.go`
- `internal/config/loader.go`
- `internal/config/validator.go`
- `internal/config/marshal.go`
- `internal/storage/storage.go`
- `internal/monitor/`
- `pkg/backup/pg_dump/` (reference for mature compression handling)

## Build and Validation Commands

```bash
go mod download
go build -o sentinel
go test ./...
./sentinel backup -h
./sentinel schedule -h
./sentinel retention -h
./sentinel monitor -h
```

## Common Gotchas

- PostgreSQL format/compression compatibility rules are strict (validate before execution)
- Storage validation runs before some DB-specific logic
- Scheduler restore coverage is implemented but still has iterative hardening areas; verify behavior with tests
- Cloud storage often stages through local temp paths before upload
- Keep roadmap version naming in docs as-is; some filenames are intentionally historical


