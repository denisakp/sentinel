# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Sentinel — Go 1.24 CLI for automated DB backup, restore, and disaster recovery (PostgreSQL, MySQL, MariaDB, MongoDB). Entry point: `main.go` → `internal/cli.Execute()`. Built with Cobra.

Current version: v1.3.0. Branch convention: feature branches off `main` (or active line like `1.x`).

## Build / Test / Run

```bash
go build -o sentinel ./...    # build binary
go vet ./...                  # static analysis
go test ./...                 # all tests
go test ./internal/cli -run TestFoo    # single test
./sentinel <cmd> --config sentinel.yaml
```

End-to-end (requires Docker):

```bash
make e2e          # build image + start infra (pg/mysql/mariadb/mongo + Azurite + fake-gcs) + run scripts/e2e.sh + teardown
make e2e-quick    # same, skip image rebuild
make infra-up     # start infra only
make infra-down   # stop everything
```

E2E runner: `scripts/e2e.sh`. Infra compose files: `infra/docker/docker-compose.yml`, `docker-compose.e2e.yml`. Docker network `sentinel`.

## Architecture

### CLI registration pattern
Each command file in `internal/cli/` exports a `*cobra.Command` and registers subcommands in its own `init()`. Root (`internal/cli/root.go`) explicitly calls `RootCmd.AddCommand(...)` for every top-level command. Adding a new top-level command requires editing `root.go`.

Top-level commands: `backup`, `schedule`, `restore`, `monitor`, `retention`, `config`, `db`, `security`, `storage`, `version`.

### Package layout
- `internal/cli/` — Cobra commands (one file per command group)
- `internal/config/` — `types.go` (YAML schema), `loader.go`, `validator.go`
- `internal/monitor/` — SQLite execution history. `NewMonitor(dbPath) (*Monitor, error)`; always `defer .Close()`
- `internal/scheduler/` — cron loop (`scheduler.go`), execution (`executor.go`), restore hook (`restore_integration.go`)
- `internal/storage/` — backend dispatcher; sub-packages `local/`, `sentinel_s3/`, `gcs/`, `gdrive/`, `azure/`, shared types in `types/`
- `internal/crypto/` — AES-256-GCM streaming (key, encrypt, decrypt, hash)
- `internal/manifest/` — SHA-256 manifest + HashingWriter for integrity
- `internal/lock/` — file-based concurrency; `RunWithLock` / `RunWithTimeout` + stale lock scan on startup
- `internal/sanitize/` — credential redaction (`RedactArgs`), used by all arg builders
- `internal/tls/` — `internaltls.Config` (domain) mirrors `config.TLSConfig` (YAML); map manually
- `internal/retention/`, `internal/notifier/` (slack/discord/email/webhook), `internal/version/`
- `pkg/backup/{pg,mysql,mariadb,mongo}_dump/args_builder.go` — engine-specific dump arg builders
- `pkg/backup/{mysqlbinlog,pg_combine}/` — incremental backup helpers (WAL / binlogs)
- `pkg/restore/{pg,mysql,mariadb,mongo}_restore/args_builder.go` — restore arg builders

### Import-cycle rule
`internal/storage/storage.go` imports its sub-packages (local, s3, gdrive, ...). Sub-packages MUST NOT import `internal/storage`. Use `internal/storage/types` for shared types.

### Retry / locking
`withRetry` runs 3 attempts with 1s/2s/4s backoffs (`RunBackupWithRetry` helper). All backup/restore execution wraps in a per-job file lock from `internal/lock`.

### Storage backends
Implement `StorageBackend` interface (`internal/storage/backend.go`). Local, S3-compatible, GCS, Google Drive, Azure Blob.

### Incremental + PITR
- Postgres: WAL-based (PG17+), PITR via `restore_mode: pitr` + `pitr_timestamp`
- MySQL/MariaDB: binary logs, `binlog_target_time`
- MongoDB: oplog
- Chain commands: `backup chain-status`, `backup chain-list`, `backup force-full`, `restore validate-chain`

### Encryption
Opt-in via `encryption_key_env`. Generate with `sentinel security init-key`. Plaintext default when no key configured.

## Spec-driven workflow

Repo uses Spec Kit (`.specify/`, `specs/`, `prds/`). Skills available: `speckit.specify`, `speckit.plan`, `speckit.tasks`, `speckit.implement`, `speckit.clarify`, `speckit.analyze`, `speckit.checklist`, `speckit.constitution`, `speckit.taskstoissues`. Use when feature work touches spec/plan/task artifacts.

## Docs
- `README.md` — user-facing usage, config examples
- `docs/testing-guide.md` — operator notes for PITR, incremental chains, encryption semantics
- `docs/roadmap/`, `release-notes.md`

<!-- SPECKIT START -->
Active plan: `specs/007-crypto-nonce-xor-fix/plan.md`
<!-- SPECKIT END -->
