# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Sentinel — Go 1.24 CLI for automated DB backup, restore, and disaster recovery (PostgreSQL, MySQL, MariaDB, MongoDB). Entry point: `main.go` → `internal/cli.Execute()`. Built with Cobra.

Current version: v1.3.0. Branch convention: feature branches off `develop` (the active integration line where merges land first). Promotion chain: `develop → 1.x` (`1.x` = stable release line). `main` is a frozen v1.0 fossil (2024) — not an active line. The canonical remote is `upstream` (denisakp/sentinel); `origin` is the working fork.

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
- `internal/cli/` — Cobra commands (one file per command group). `restore run` accepts `--all` (run every enabled restore job concurrently, bounded by top-level `max_concurrent_restores`, default 1) + `--parallel N` override; `handleRestoreRunAll` fans out over a `chan struct{}` semaphore reusing `runOneRestoreJob` (spec 045 / PRD 31 — job axis; DB axis deferred, restore has no multi-DB auto-discovery).
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
- `internal/adapters/tls/` — TLS adapter implementing `ports.Prober` (spec 033). `Adapter` satisfies the port; `BuildTLSArgs` + `ProbeTLSConnection` remain reachable as package-level functions. Domain type `internaltls.Config` lives in `internal/ports/tls.go` (spec 028); the adapter maps from `config.TLSConfig` (YAML). Mongo PEM lifecycle (`MongoTLSMaterial`, `PrepareMongoTLS`, `SweepOrphanMaterial`, `Register`/`Unregister`/`CloseAll`) lives in the neutral `internal/adapters/mongo_tls/` (spec 035 relocated it out of `tls/` to `dump/mongo/`; spec 042 / PRD 29 moved it to its neutral home).
- `internal/adapters/notifier/` — multi-channel dispatcher (slack/discord/email/webhook) implementing `ports.Dispatcher` + `ports.Notifier` (spec 034). `*Dispatcher` satisfies `ports.Dispatcher`; each `*SlackNotifier`/`*DiscordNotifier`/`*EmailNotifier`/`*WebhookNotifier` satisfies `ports.Notifier`. Compile-time port assertions in `conformance.go`. Context/config types (`BackupContext`, `RestoreContext`, `WebhookNotificationConfig`, `EmailNotificationConfig`, `ErrNon2xxResponse`) live in `internal/ports/notifier.go`.
- `internal/domain/backup/` — backup domain (specs 037 + 038). `Executor` with 9-port constructor (`NewExecutor(DumpBuilder, StorageBackend, EncryptWriter, Hasher, Recorder, Dispatcher, LockManager, DBProber, ManifestStore)`) + `Run` (Validate → Lock → Ping → Dump → Pipeline → Record → Notify); `Job` carries factory-wired hooks (`EncryptArtifact`, `ArchiveBinlogs`/`ArchiveOplog`, `VerifyArtifactHash`) for adapter-backed steps. Plus `planner.go` (full-vs-incremental via `ports.Recorder`), `pipeline.go` (manifest/encryption), `source.go` (SQL source enumeration via `ports.DBProber`), `ParseAdditionalArgs`, `ValidateDbType`, `incremental/` (chain math). Constructed via `internal/cli/backup_factory.go::NewBackupExecutorFromConfig`.
- `internal/domain/restore/` — restore domain (specs 037 + 038). `Executor` with 9-port constructor (`NewExecutor(RestoreBuilder, StorageBackend, DecryptReader, Hasher, Recorder, Dispatcher, LockManager, DBProber, ChainAssembler)`) + `Run` (full pre-carve `ExecuteRestore` control flow); `Job` hooks (`StageSource`/`StageChain`/`Preflight`/`RestoreOptions`/`BinlogReplay`/`OplogReplay`/…) wired by `internal/adapters/restore/runtime`. Plus pure planner (`Plan`/`PlanFromManifestPath` + reason codes), plan types, `ResolveChainObject`, `StagedArtifact` + cleanup, `incremental/` (chain resolver, fallback, assembly preconditions). **Single construction site**: `internal/adapters/restore/runtime/executor.go`.
- `internal/domain/schedule/` — pure schedule model (specs 037 + 038). `JobInfo`, `JobStatus`, `ExecutionRecord`, `JobKind`, `ScheduledJob`, `Schedule` interface, `NextRun`, `Validate`. The scheduler runtime stores parsed cron values as `schedule.Schedule` (`jobState.parsedSchedule`, `Scheduler.JobSchedule(name)`); `internal/cli/{schedule,restore}.go` call `schedule.Validate` at config-validation time.
- `internal/domain/retention/` — pure policy evaluation (spec 037; carve completed by spec 038). `Policy`, `BackupRecord`, `BackupCandidate`, `CalculateCandidates`, `ProtectActiveBaseline`. Orchestration lives in `internal/cli/retention_helpers.go` (`applyJobRetention`: domain calc → `ports.StorageBackend.Delete` → `ports.Recorder.RetentionDeleteRecords`); history DELETEs in `internal/adapters/monitor/retention.go`.
- `internal/adapters/restore/runtime/` — shared driving adapter for restore execution (spec 038). `ExecuteRestore(ctx, *ExecutionRequest)` + the single `domain/restore.NewExecutor` construction site; owns staging (`StageRestoreSource`/`StageChainArtifacts`), preflight verify+decrypt (`PreRestoreVerifyAndDecrypt`), and the config-typed planner bridge (`PlanAdvancedRestore*`). Consumed by `internal/cli/restore.go`, `internal/scheduler/restore_{executor,integration}.go`, and the integration tests.
- `internal/adapters/restore/chain_assembler/` — `ports.ChainAssembler` adapter over `pg_combinebackup` (spec 038 FR-013); conformance assertion + `combinePostgresChain` test seam. Test fake: `internal/ports/chainassemblertesting.MockAssembler`.
- `internal/utils/` — shared helpers (`file.go`, `time.go`, `scheduled_output.go`)
- `internal/version/`
- `internal/adapters/dump/{pg,mysql,mariadb,mongo}/` — engine-specific dump adapters (spec 035). Each package exposes a zero-field `Builder` satisfying `ports.DumpBuilder` and keeps its existing `Backup`/`BackupAll` entry points + `*DumpArgs` types. Compile-time port assertions in `conformance.go`. Port types `DumpBuilder`/`DumpCleanup`/`BuildContext`/`BuildResult` live in `internal/ports/dump.go`. (Mongo PEM lifecycle moved out to the neutral `internal/adapters/mongo_tls/` by spec 042 / PRD 29. The mysql + mariadb args-builder body + validator are shared via `internal/adapters/mysqlargs.BuildArgs(Input, Flavor)` — a sibling outside `dump/` so the axis rule allows both engines to import it — by spec 044 / PRD 15; each engine's `argsBuilder` delegates with its `Flavor`. Dump builders are constructed via `internal/adapters/dump/registry.go::NewBuilder(engine, prober)` with an injected `ports.DBProber`, spec 043 / PRD 30.)
- `internal/adapters/restore/incremental/{mysqlbinlog,pgcombine}/` — external-binary wrappers for incremental replay (spec 035). `pgcombine` is reached only through `internal/adapters/restore/chain_assembler/` (`ports.ChainAssembler`); `mysqlbinlog` is consumed by `internal/cli/backup_factory.go` (archive) and `internal/adapters/restore/runtime/` (replay).
- `internal/adapters/restore/{pg,mysql,mariadb,mongo}/` — engine-specific restore adapters (spec 036). Each package exposes a zero-field `Builder` satisfying `ports.RestoreBuilder` and keeps its existing `Restore` entry point + `RestoreArgs` type (mongo additionally has `OplogReplayArgs` + `ReplayOplog`). Compile-time port assertions in `conformance.go`. Port types `RestoreBuilder`/`RestoreBuildContext`/`RestoreBuildResult`/`RestoreOptions` live in `internal/ports/restore.go`.

### Import-cycle rule
**Enforced in CI via `.golangci.yml` (depguard rule groups: `domain-pure`, `ports-mostly-pure`, `adapter-{dump,restore,storage}-axis`, `config-marshal`); see spec 039 / PRD 25. Run locally with `make lint`.** Hexagonal layering per ADR 0001: **`adapters → ports ← domain`**. Adapter sub-packages under `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/`, `internal/adapters/dump/{pg,mysql,mariadb,mongo}/`, and `internal/adapters/restore/{pg,mysql,mariadb,mongo}/` MUST NOT import each other. Storage registry imports every storage sibling. Dump and restore adapters don't import sibling adapters within their axis. Domain code reaches concrete storage backends only through `storage.NewBackend(...)` returning `ports.StorageBackend`. Domain code reaches dumps through `ports.DumpBuilder` and restores through `ports.RestoreBuilder` (each engine adapter exposes a `Builder` value).

**Adapter→adapter cross-imports: none.** The ADR 0001 Phase-D cleanups closed every adapter→adapter cross-import — dump args via `ports.DumpArgsFactory` (spec 040 / PRD 27), restore args via `ports.RestoreArgsFactory` (spec 041 / PRD 28), Mongo PEM lifecycle relocated to the neutral `internal/adapters/mongo_tls/` (spec 042 / PRD 29), and dump connectivity via injected `ports.DBProber` (spec 043 / PRD 30). Dump/restore builders are constructed through `internal/adapters/dump/registry.go` / `internal/adapters/restore/registry.go` with their dependencies injected.

**Remaining documented items** (deliberate / by-design / out-of-scope — NOT layering drift):
- `internal/ports/recorder.go` imports `internal/domain/retention` for the `BackupCandidate` and `Policy` parameter types on `RetentionDeleteRecords` / `DeleteRestoreExecutions` (spec 038 Q2). The reverse import (`domain → ports`) is the normal direction; this is the only deliberate ports-side dependency on domain.
- `internal/config/marshal.go` imports `internal/adapters/restore/incremental/mysqlbinlog` for the `mysqlbinlog.ReplayArgs` return type of `BuildMySQLBinlogReplayArgs` (incremental binlog-replay helper; a separate axis from the restore engine adapters). Out of scope for PRDs 27/28; a future micro-cleanup may retire this last `config → adapter` reference.
- `internal/adapters/restore/runtime/` (driving adapter) imports the four engine restore adapters, `chain_assembler`, `mysqlbinlog`, crypto, lock, monitor, and `internal/config` — by design: it is the composition root for restore execution (spec 038 FR-011). The domain Executor itself reaches engines only through `ports.RestoreBuilder`.

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

Repo uses Spec Kit (`.specify/`). Skills available: `speckit.specify`, `speckit.clarify`, `speckit.plan`, `speckit.tasks`, `speckit.analyze`, `speckit.taskstoissues`, `speckit.implement`, plus `speckit.checklist` and `speckit.constitution`. Use when feature work touches spec/plan/task artifacts.

### Mandatory feature flow (fixed order)
Every new feature MUST run these skills in this exact order — no skipping, no reordering:

1. `speckit.specify` — write the feature spec
2. `speckit.clarify` — resolve ambiguities in the spec
3. `speckit.plan` — produce the implementation plan
4. `speckit.tasks` — break the plan into tasks
5. `speckit.analyze` — cross-check spec/plan/tasks consistency
6. `speckit.taskstoissues` — file tasks as tracked issues
7. `speckit.implement` — execute the tasks

`speckit.checklist` and `speckit.constitution` are supporting skills invoked ad hoc, not part of the ordered flow. The canonical machine-readable definition of this order lives in `.specify/workflows/speckit/workflow.yml`; keep it and this section in sync.

## Docs
- `README.md` — user-facing usage, config examples
- `docs/runbooks/` — operational procedures (PITR, incremental chains, encryption, stale-lock recovery, etc.). See `docs/runbooks/README.md` for the index.
- `release-notes.md`

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan:
`specs/045-parallel-multi-job-restore/plan.md`
<!-- SPECKIT END -->
