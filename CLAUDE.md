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
- `internal/adapters/monitor/` — SQLite execution history adapter implementing `ports.Recorder` (spec 032). `NewMonitor(dbPath) (*Monitor, error)`; always `defer .Close()`. Schema is version-gated via `BinarySchemaVersion`; migrations run under a file lock on every open and forward-incompat DBs are refused (`ErrForwardIncompatible`). Inspect with `sentinel monitor doctor [--repair]`.
- `internal/scheduler/` — cron loop (`scheduler.go`), execution (`executor.go`), restore hook (`restore_integration.go`)
- `internal/ports/` — port (hexagonal interface) declarations; spec 028. Contains `StorageBackend`, `StorageObject`, `RepoStatus`, `StatusReporter`, plus the other architectural ports (dump, recorder, notifier, etc.). `internal/ports/storagetesting/` houses an in-memory `MockBackend` test fake reachable from domain test code without importing any adapter.
- `internal/adapters/storage/` — storage backends (spec 029). Sub-packages `local/`, `s3/`, `gcs/`, `gdrive/`, `azure/`. Single registry constructor `NewBackend(p *BackendParams) (ports.StorageBackend, error)` covering all five types; legacy driver-side `Storage` interface + `NewStorage` live in `writer.go`. Cross-adapter contract suite `contract_test.go` + `contract_integration_test.go` (latter behind `//go:build integration`).
- `internal/adapters/crypto/` — AES-256-GCM streaming (key, encrypt, decrypt, hash); implements `ports.EncryptWriter` / `DecryptReader` / `Hasher` / `KeyProvider` (spec 030).
- `internal/domain/manifest/` — pure manifest v1 lineage validator (spec 037; relocated from `internal/manifest/`). `ValidateIncrementalLineageContract` only; stdlib + ports only.
- `internal/adapters/manifest_store/` — driving adapter for `ports.ManifestStore` (spec 037). Owns `WriteManifest`/`ReadManifest`/`LoadRestoreManifest`/`VerifyBackupHash` + `Adapter` (file I/O + `internal/adapters/crypto.HashingWriter` for SHA-256).
- `internal/adapters/db_probe/` — driving adapter for `ports.DBProber` (spec 037). Consolidates SQL ping/list/connectivity (PG/MySQL/MariaDB) + Mongo ping/list + PG cascade-safety. Package-level helpers (`PingSqlDatabase`, `ListDatabases`, `CheckConnectivity`, `CheckMongoConnectivity`, `ListMongoDatabases`, `AssessPostgresCascadeSafetyDSN`) reachable for legacy callers.
- `internal/adapters/lock/` — file-based concurrency adapter implementing `ports.LockManager`; `RunWithLock` / `RunWithTimeout` + stale lock scan on startup (spec 031)
- `internal/sanitize/` — credential redaction (`RedactArgs`), used by all arg builders
- `internal/adapters/tls/` — TLS adapter implementing `ports.Prober` (spec 033). `Adapter` satisfies the port; `BuildTLSArgs` + `ProbeTLSConnection` remain reachable as package-level functions. Domain type `internaltls.Config` lives in `internal/ports/tls.go` (spec 028); the adapter maps from `config.TLSConfig` (YAML). Mongo PEM lifecycle (`MongoTLSMaterial`, `PrepareMongoTLS`, `SweepOrphanMaterial`, `Register`/`Unregister`/`CloseAll`) moved to `internal/adapters/dump/mongo/` by spec 035.
- `internal/adapters/notifier/` — multi-channel dispatcher (slack/discord/email/webhook) implementing `ports.Dispatcher` + `ports.Notifier` (spec 034). `*Dispatcher` satisfies `ports.Dispatcher`; each `*SlackNotifier`/`*DiscordNotifier`/`*EmailNotifier`/`*WebhookNotifier` satisfies `ports.Notifier`. Compile-time port assertions in `conformance.go`. Context/config types (`BackupContext`, `RestoreContext`, `WebhookNotificationConfig`, `EmailNotificationConfig`, `ErrNon2xxResponse`) live in `internal/ports/notifier.go`.
- `internal/domain/backup/` — pure backup helpers (spec 037 partial). `ParseAdditionalArgs`, `RemoveArgsDuplicate`, `ValidateDbType` + `incremental/` sub-package (chain math, prerequisites). Backup orchestration Executor extraction deferred to spec 038.
- `internal/domain/restore/` — pure restore plan types (spec 037 partial). `AdvancedRestoreMode`, `PlanStatus`, `FallbackCandidate`, `AdvancedRestorePlan` + `incremental/` sub-package (chain resolver, fallback evaluator). Restore Executor extraction deferred to spec 038.
- `internal/domain/schedule/` — pure schedule model (spec 037 partial). `JobInfo`, `JobStatus`, `ExecutionRecord`, `JobKind`, `ScheduledJob`, `Schedule` interface, `NextRun`, `Validate`. Cron parser refactor deferred to spec 038.
- `internal/domain/retention/` — pure policy evaluation (spec 037 partial). `Policy`, `BackupRecord`, `BackupCandidate`, `CalculateCandidates`, `ProtectActiveBaseline`. Manager + DELETE chain still in `internal/retention/`; full carve deferred to spec 038.
- `internal/restore/` — partial: still owns `executor.go` (orchestration), `planner.go`, `pipeline.go`, `source.go`, `postgres_pitr.go`, and `incremental/assembler.go` (uses `pgcombine` adapter); `plan_types.go` + `incremental/bridge.go` are spec 037 re-export bridges (`TODO(spec-038): remove`). Full deletion deferred to spec 038.
- `internal/retention/` — partial: still owns `Manager` + `Apply`/`ApplyRestoreRetention`/`ApplyAll`/`ListCandidates` + `cleaner.go` (DELETE chain across local/S3/GCS/Azure); `types.go` + `calculator.go` are spec 037 re-export bridges. Full deletion deferred to spec 038.
- `internal/utils/` — shared helpers (`file.go`, `time.go`, `scheduled_output.go`)
- `internal/version/`
- `internal/adapters/dump/{pg,mysql,mariadb,mongo}/` — engine-specific dump adapters (spec 035). Each package exposes a zero-field `Builder` satisfying `ports.DumpBuilder` and keeps its existing `Backup`/`BackupAll` entry points + `*DumpArgs` types. Compile-time port assertions in `conformance.go`. `internal/adapters/dump/mongo/` also owns Mongo PEM lifecycle (`material.go`, `cleanup.go`, `sweep.go`). Port types `DumpBuilder`/`DumpCleanup`/`BuildContext`/`BuildResult` live in `internal/ports/dump.go`.
- `internal/adapters/restore/incremental/{mysqlbinlog,pgcombine}/` — external-binary wrappers for restore-side incremental replay (spec 035). Single-callsite each (executor.go, assembler.go); no port introduced.
- `internal/adapters/restore/{pg,mysql,mariadb,mongo}/` — engine-specific restore adapters (spec 036). Each package exposes a zero-field `Builder` satisfying `ports.RestoreBuilder` and keeps its existing `Restore` entry point + `RestoreArgs` type (mongo additionally has `OplogReplayArgs` + `ReplayOplog`). Compile-time port assertions in `conformance.go`. Port types `RestoreBuilder`/`RestoreBuildContext`/`RestoreBuildResult`/`RestoreOptions` live in `internal/ports/restore.go`.

### Import-cycle rule
Hexagonal layering per ADR 0001: **`adapters → ports ← domain`**. Adapter sub-packages under `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/`, `internal/adapters/dump/{pg,mysql,mariadb,mongo}/`, and `internal/adapters/restore/{pg,mysql,mariadb,mongo}/` MUST NOT import each other. Storage registry imports every storage sibling. Dump and restore adapters don't import sibling adapters within their axis. Domain code reaches concrete storage backends only through `storage.NewBackend(...)` returning `ports.StorageBackend`. Domain code reaches dumps through `ports.DumpBuilder` and restores through `ports.RestoreBuilder` (each engine adapter exposes a `Builder` value).

**Known narrow exceptions** (pre-existing; called out so they aren't flagged as new debt):
- `internal/config/marshal.go` imports the four dump adapter packages for `*DumpArgs` type names referenced in `Build*DumpArgs` return types (spec 035 clarification Q1) and the four restore adapter packages for `RestoreArgs`/`OplogReplayArgs` type names referenced in `Build*RestoreArgs`/`BuildMongoOplogReplayArgs` return types (spec 036 clarification Q1). Future spec may introduce `ports.DumpArgsFactory`/`RestoreArgsFactory` to remove the direction.
- `internal/restore/{executor.go,incremental/assembler.go}` import `internal/adapters/restore/incremental/{mysqlbinlog,pgcombine}/` directly (single-callsite each; no port — spec 035 clarification Q2).
- `internal/cli/root.go` and `internal/adapters/restore/mongo/args_builder.go` import `internal/adapters/dump/mongo` for Mongo PEM lifecycle calls (`SweepOrphanMaterial`, `CloseAll`, `PrepareMongoTLS`). Cross-axis exception preserved by spec 036; a future spec may host shared Mongo PEM lifecycle in a neutral package.
- Adapter dumps (`internal/adapters/dump/{pg,mysql,mariadb,mongo}/`) import `internal/adapters/db_probe` for `CheckConnectivity` / `CheckMongoConnectivity` (spec 037; replaces prior `internal/backup/sql` cross-import). Documented; no port until adapter dumps accept `ports.DBProber` via constructor injection (spec 038 candidate).
- Spec 037 left 4 re-export bridge files (`// TODO(spec-038): remove`): `internal/retention/{types,calculator}.go`, `internal/restore/plan_types.go`, `internal/restore/incremental/bridge.go`. They re-export pure domain symbols under legacy paths; full retirement awaits spec 038's Executor extractions.

### Retry / locking
`withRetry` runs 3 attempts with 1s/2s/4s backoffs (`RunBackupWithRetry` helper). All backup/restore execution wraps in a per-job file lock from `internal/adapters/lock`.

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
`specs/037-domain-extraction/plan.md`
<!-- SPECKIT END -->
