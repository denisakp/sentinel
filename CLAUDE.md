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
- `internal/monitor/` — SQLite execution history. `NewMonitor(dbPath) (*Monitor, error)`; always `defer .Close()`. Schema is version-gated via `BinarySchemaVersion`; migrations run under a file lock on every open and forward-incompat DBs are refused (`ErrForwardIncompatible`). Inspect with `sentinel monitor doctor [--repair]`.
- `internal/scheduler/` — cron loop (`scheduler.go`), execution (`executor.go`), restore hook (`restore_integration.go`)
- `internal/ports/` — port (hexagonal interface) declarations; spec 028. Contains `StorageBackend`, `StorageObject`, `RepoStatus`, `StatusReporter`, plus the other architectural ports (dump, recorder, notifier, etc.). `internal/ports/storagetesting/` houses an in-memory `MockBackend` test fake reachable from domain test code without importing any adapter.
- `internal/adapters/storage/` — storage backends (spec 029). Sub-packages `local/`, `s3/`, `gcs/`, `gdrive/`, `azure/`. Single registry constructor `NewBackend(p *BackendParams) (ports.StorageBackend, error)` covering all five types; legacy driver-side `Storage` interface + `NewStorage` live in `writer.go`. Cross-adapter contract suite `contract_test.go` + `contract_integration_test.go` (latter behind `//go:build integration`).
- `internal/crypto/` — AES-256-GCM streaming (key, encrypt, decrypt, hash)
- `internal/manifest/` — SHA-256 manifest + HashingWriter for integrity
- `internal/lock/` — file-based concurrency; `RunWithLock` / `RunWithTimeout` + stale lock scan on startup
- `internal/sanitize/` — credential redaction (`RedactArgs`), used by all arg builders
- `internal/tls/` — `internaltls.Config` (domain) mirrors `config.TLSConfig` (YAML); map manually
- `internal/backup/` — execution engine: `executor.go`, `planner.go`, `pipeline.go`, `source.go`, `postgres_pitr.go`, `postgres_conflicts.go`; sub-packages `incremental/`, `mongo/`, `sql/`
- `internal/restore/` — restore arg validation (`args.go`, `validator.go`) + `incremental/`
- `internal/utils/` — shared helpers (`file.go`, `time.go`, `scheduled_output.go`)
- `internal/retention/`, `internal/notifier/` (slack/discord/email/webhook), `internal/version/`
- `pkg/backup/{pg,mysql,mariadb,mongo}_dump/args_builder.go` — engine-specific dump arg builders
- `pkg/backup/{mysqlbinlog,pg_combine}/` — incremental backup helpers (WAL / binlogs)
- `pkg/restore/{pg,mysql,mariadb,mongo}_restore/args_builder.go` — restore arg builders

### Import-cycle rule
Hexagonal layering per ADR 0001: **`adapters → ports ← domain`**. Adapter sub-packages under `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/` MUST NOT import each other and MUST NOT import the parent `internal/adapters/storage` package (which holds the registry). The registry is allowed to import every adapter sibling. Domain code reaches concrete backends only through `storage.NewBackend(...)` returning `ports.StorageBackend` (and optionally `ports.StatusReporter` via type-assertion). The legacy `internal/storage/` package is gone; see spec 029.

### Retry / locking
`withRetry` runs 3 attempts with 1s/2s/4s backoffs (`RunBackupWithRetry` helper). All backup/restore execution wraps in a per-job file lock from `internal/lock`.

### Storage backends
Implement `ports.StorageBackend` (`internal/ports/storage.go`). Concrete adapters: `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/`. Adding a new backend = one new sub-package + one new line in `internal/adapters/storage/registry.go`.

### Incremental + PITR
- Postgres: WAL-based (PG17+), PITR via `restore_mode: pitr` + `pitr_timestamp`
- MySQL/MariaDB: binary logs, `binlog_target_time`
- MongoDB: oplog
- Chain commands: `backup chain-status`, `backup chain-list`, `backup force-full`, `restore validate-chain`

### Encryption
Opt-in via `encryption_key_env`. Generate with `sentinel security init-key`. Plaintext default when no key configured.

## Spec-driven workflow

Repo uses Spec Kit (`.specify/`). Skills available: `speckit.specify`, `speckit.plan`, `speckit.tasks`, `speckit.implement`, `speckit.clarify`, `speckit.analyze`, `speckit.checklist`, `speckit.constitution`, `speckit.taskstoissues`. Use when feature work touches spec/plan/task artifacts.

## Docs
- `README.md` — user-facing usage, config examples
- `docs/runbooks/` — operational procedures (PITR, incremental chains, encryption, stale-lock recovery, etc.). See `docs/runbooks/README.md` for the index.
- `docs/roadmap/`, `release-notes.md`

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan:
`specs/029-storage-adapters/plan.md`
<!-- SPECKIT END -->
