# Sentinel Release Notes

## [Unreleased]

### Performance

- **backup hashing**: compute the plaintext manifest hash inline with the dump write, eliminating a second full read of the artefact. Each engine adapter (`pg_dump`, `pg_dumpall`, `mysqldump` single + `--all-databases`, `mariadb-dump` single + `--all-databases`, `mongodump` local) now returns `(digest, error)` where `digest` is `hex(sha256(payload))` of the bytes handed to storage; the orchestrator stores it in `.manifest.json` as-is. The encrypted path (AES-256-GCM) and `sentinel backup verify` are unchanged. (PRD 19)

### Deprecated

- **`sentinel backup --password` / `-p`**: deprecated; will be removed in the next minor release. Passing a password on the command line exposes it via `ps`, `/proc/<pid>/cmdline`, and shell history. Use one of three safe channels instead: `--password-env <VAR>`, `--password-file <PATH>` (first line, right-trimmed; warns on group/world-readable mode), or the existing config field `databases.<id>.password_env`. CLI flag precedence over config is preserved (silent override). Conflicts among `--password`, `--password-env`, `--password-file` are hard errors before any DB I/O. Migration recipes: [`docs/runbooks/credentials.md`](docs/runbooks/credentials.md). (Feature 015)

### Security

- **remote-storage backups bypassed encryption + integrity (silent plaintext upload)**: config-driven backups to a remote backend (S3, GCS, Azure Blob, Google Drive) previously uploaded **plaintext, unhashed, unmanifested** artifacts even when `encryption_key_env` was configured — a silent security bypass on exactly the backends where at-rest encryption matters most: the run reported success while the object landing in the bucket was readable SQL. Root cause: the post-dump security step (hash → encrypt → manifest) ran only for **local** storage and returned early for any remote backend, discarding the computed plaintext digest. Fixed via stage-then-upload — every dump engine now writes to a local staging file, the domain executor applies hash → AES-256-GCM encrypt → manifest on that staged artifact, then uploads the (encrypted) artifact **and** a `<name>.manifest.json` sidecar through `ports.StorageBackend` and cleans up staging on both success and failure. Integrity (hash + manifest) is now produced for **every** remote backup, encrypted or not, so `sentinel backup verify` (which now fetches the remote artifact + sidecar before validating) and restore (fetch → decrypt → restore) work end-to-end. A fail-loud backstop (FR-008) refuses to upload whenever encryption is configured but cannot be applied — never degrading to a plaintext upload — including the auto-discovery `strategy: single` path, which writes a combined dump straight to the backend and cannot be staged in place (use `strategy: individual` or local storage). Backward-compatible (FR-010): pre-fix remote backups (plaintext, no sidecar) remain restorable — the S3 `Download` not-found error is now mapped to `ErrSourceObjectNotFound` so an absent manifest is tolerated, not fatal. Local-storage backups are byte-for-byte unchanged. Runbook: [`docs/runbooks/enable-encryption.md`](docs/runbooks/enable-encryption.md). (spec 047)
- **mariadb dump**: migrated MariaDB backup adapter (`pkg/backup/mariadb_dump`) from `--password=<value>` argv to `MYSQL_PWD` env injection, closing the last argv-leak path across supported engines. PostgreSQL (`PGPASSWORD`), MySQL (`MYSQL_PWD`), and MongoDB (URI) channels unchanged. Verified via argv-inspection unit tests in `pkg/backup/mariadb_dump/args_builder_test.go` and `pkg/backup/mariadb_dump/env_injection_test.go`. (Feature 015)
- **dump adapters**: redact credentials embedded in subprocess stderr before they reach returned errors, logs, the monitor SQLite store, or notifier payloads. Previously, a failing `pg_dump` / `pg_dumpall` / `mysqldump` / `mariadb-dump` / `mongodump` (incl. oplog) invocation could leak `PGPASSWORD=…`, `MYSQL_PWD=…`, `MONGO_INITDB_ROOT_PASSWORD=…`, libpq `password = …`, `--password=…`, `-p<value>`, or URI userinfo (`scheme://user:secret@host`) into operator-visible output. All eight dump-adapter callsites now route stderr through `sanitize.RedactStderr`, which caps the embedded buffer at 64 KiB with an explicit truncation marker. CI gate (`make lint-redact-stderr`, wired into `.github/workflows/integration.yml`) prevents regression. Severity: medium (local-file disclosure on failure); no behavior change on successful backups. Audit artifact: `docs/audit/stderr-redaction-audit.md`. (Feature 012)

### Added

- **`sentinel backup verify --all` (repository-wide integrity sweep)**: a new `--all` mode audits the integrity of an **entire** backup repository in one command, replacing a hand-written per-id loop. It enumerates every recorded successful backup from the monitor history, fetches each artifact from **its own** storage backend (local or remote — remote artifacts are downloaded, verified, and deleted, leaving no local copy), re-hashes it, and classifies each into one of four states: `ok`, `corrupted` (hash mismatch), `missing_artifact` (manifest recorded but object gone), or `missing_manifest` (artifact present but unverifiable — typically pre-v1.1). Prints an `ID | JOB | STATUS | HASH_MATCH | TIMESTAMP` report + a per-status summary (`N checked · A ok · B corrupted · C missing_artifact · D missing_manifest`), or a machine-readable `--output json` (a `results` array + a `summary` object) for alerting. Exit codes distinguish the failure classes: `0` all-ok, `5` integrity failure (`corrupted`/`missing_artifact`, or `missing_manifest` unless `--ignore-missing-manifest`), `4` operational error (invalid config / unreadable history / unreachable backend) — so a pipeline can tell "backups are broken" apart from "the check couldn't run". Scope flags: `--since 30d|4w|720h` (day/week/hour units) and `--job <name>`; `--all` and a `<backup-id>` are mutually exclusive. The single-id `backup verify <id>` path is unchanged (both share one `verifyExecution` body). **Read-only** — the sweep writes nothing to history; persisting results (`--record` + the `integrity_checks` schema) is owned by the sibling scheduled-integrity feature (PRD 35), which this unblocks. New `integrity.algorithm` config block (validated: `sha256` only). Runbook: [`docs/runbooks/integrity-sweep.md`](docs/runbooks/integrity-sweep.md). (spec 051 / PRD 34)
- **scheduled integrity check (cron-driven sweep + audit trail + failure notification)**: completes the "Watchdog" integrity trio (M5) — Sentinel can now run the `backup verify --all` repository sweep automatically on a cron, record every run durably, and page on corruption, with **zero** manual invocation. Add an `integrity.scheduled_check` block (`enabled`, `cron`, optional `since` recency window, optional single-`job` scope, `notify_on: failure|always|never`) and `sentinel schedule start` registers a reserved `__integrity_check` job alongside your backup/restore jobs — inheriting the same skip-if-already-running + crash-isolation behaviour (purely additive; backup/restore scheduling is unchanged). The scheduled sweep runs the **exact same** implementation as the manual command (one shared `runVerifySweep` core, so classification is identical). Each run writes **one row per verified artifact**, grouped by `run_id` and labelled by `trigger` (`scheduled`/`manual`), into a new `integrity_checks` audit table (monitor schema migration `005`, `BinarySchemaVersion` 4→5 — an existing history DB upgrades in place on first open; a newer DB is refused by an older binary with a clear forward-incompatibility error). The store enforces the outcome vocabulary at write time (`result` CHECK: `ok`/`corrupted`/`missing_artifact`/`missing_manifest`) — an out-of-vocabulary result cannot be persisted. On a sweep with any non-`ok` result the configured channels (`defaults.notifications`, the same Slack/Discord/email/webhook path backups use) receive a **failure** notification when `notify_on` is `failure` (default) or `always`; `always` also confirms clean runs; `never` stays silent — delivery is **best-effort** (a channel failure is a warning, never a scheduler crash and never a lost recorded run). Config validation rejects an enabled check with a missing/invalid cron, an unparseable `since`, an out-of-range `notify_on`, or a user job that squats the reserved `__integrity_check` name. New `ports.Recorder.RecordIntegrityCheck` (the sole interface addition; no notifier-port change — reuses `Dispatcher.Notify`). `sentinel schedule list` labels the job type `integrity`. Runbook: [`docs/runbooks/integrity-sweep.md`](docs/runbooks/integrity-sweep.md). (spec 052 / PRD 35)
- **`sentinel backup diff <id1> <id2>` (metadata comparison + security-regression detection)**: compare two recorded backups' **metadata** to diagnose an anomaly (a 3× size jump, a doubled duration, a `full` where an `incremental` was expected, a chain-depth reset) — and, the headline, to catch silent **security regressions** — without reading a single artifact byte. Reuses `backup verify`'s ID→row→manifest resolution (monitor row for size/duration/backup-type/chain, `.manifest.json` sidecar for hash + encryption) — the hash-compute step is never invoked, so no artifact is opened (remote backups fetch only the tiny sidecar, never the artifact). Compares the eight fields that exist in manifest v1 (`compression.ratio`/`database_version` are not fabricated — gated on the compression manifest fields). Any of **encryption disabled** (a backup silently became plaintext), **hash-algorithm change**, or **encryption-parameter downgrade** (algorithm/KDF change, fewer iterations, dropped envelope version) is flagged and yields a **non-zero exit** so CI/alerting can gate on it; size/duration swings get a `⚠` marker but stay exit 0. Renders a `FIELD | BEFORE | AFTER | DELTA` table of only the differing fields, or `--output json` for automation; a pre-v1.1 backup without a manifest degrades to a monitor-row-only partial diff with a warning, never a hard fail. No new port, no artifact re-download. (PRD 36)
- **`sentinel repair` (repository state reconciliation)**: a new top-level command that reconciles Sentinel's sources of truth — monitor rows, `.manifest.json` sidecars, storage artifacts, and the lock directory — across every configured job, detecting the internal-state drift a crash mid-backup, a hard-killed process, or a manual artifact deletion leaves behind. Distinct from `monitor doctor --repair` (schema-only, one file); repair is repository-wide and can delete artifacts. Detects six drift classes: `orphan_artifact` (object with no sidecar **and** no row), `orphan_manifest`, `artifact_missing`, `stale_running` (a `running` row whose job holds no live lock), `stale_lock` (dead-PID, over-threshold), and `chain_broken` (a missing/non-contiguous incremental link, via the same `ResolveOrderedChain` resolver as `restore validate-chain`). **Safe by default**: no flag / `--dry-run` reports everything and mutates nothing; `--fix` applies only the recoverable classes (finalize stale-running rows to `interrupted`, remove stale locks, mark broken chains) and **never deletes an artifact**; `--purge-orphans` (implies `--fix`) is the only delete path — interactive confirmation unless `--yes`, and an **active chain baseline is never purged** (`retention.ProtectActiveBaseline`). The stale-running finalize is **lock-guarded** (a live lock → skip; the correctness improvement over the blind reconcile used at `schedule` startup), foreign-host locks/rows are skipped with a warning, and repair **refuses** to run against a non-`current` monitor schema (`monitor.Diagnose`, deferring to `monitor doctor`). Non-zero exit while `artifact_missing`/`chain_broken` remain, so it can gate CI/cron; `--format json` + `--job <name>`. Composition-only — no new port. Runbook: [`docs/runbooks/state-repair.md`](docs/runbooks/state-repair.md). (PRD 37)
- **backup compression (gzip/zstd, all engines)**: opt-in `retention`-style `compression: {enabled, algorithm, level}` (per-job + `defaults`) adds an engine-agnostic streaming compression stage to the backup pipeline (order: dump → **compress** → hash → encrypt → upload). `zstd` (via `klauspost/compress`) or `gzip`; the compressed digest is the stored-artifact hash (ciphertext digest when also encrypted). The manifest records `compression: {algorithm, level}` and restore **auto-detects** and decompresses — no operator flag; legacy backups without the block restore unchanged. Validator rejects `enabled` alongside pg (`compress`/`pg_compression_*`) or mongo (`gzip`) native compression (no double-compress). New `ports.CompressWriter`/`DecompressReader` + `internal/adapters/compress/`; ADR-0001 clean. Runbook: [`docs/runbooks/backup-compression.md`](docs/runbooks/backup-compression.md). (PRD 33)
- **cross-platform binary distribution (GoReleaser)**: tagged releases (`v*`) now publish prebuilt binaries for linux/darwin/windows × amd64/arm64 (windows/arm64 excluded) + `checksums.txt` (SHA-256), with `internal/version.{Version,Commit,BuildDate}` stamped via ldflags. `README` makes prebuilt binaries the primary install path and adds `go install github.com/denisakp/sentinel@latest`; git-clone/build demoted to "from source". Supply-chain signing (cosign/SLSA) deferred to a fast-follow. (PRD 38)
- **`restore run --skip-hash-verify`**: a per-invocation, WARNING-gated escape hatch to proceed past a *benign* manifest SHA-256 mismatch instead of a hard abort. Flag-only (never an env/config default), off by default (verify stays on), restore-only. For encrypted artifacts the independent AES-256-GCM auth-tag remains an unbypassable integrity gate — the flag only silences the manifest-hash compare. (PRD 39)
- **GFS retention (grandfather-father-son)**: `retention.gfs` adds four independent calendar-tier retention rules — `keep_daily`, `keep_weekly` (ISO Mon–Sun), `keep_monthly`, `keep_yearly` — alongside the flat `keep_last`/`keep_days`. Each tier keeps the newest backup of each of the N most-recent **occupied** calendar buckets (UTC); empty periods are skipped, not backfilled. A backup is kept if it anchors **any** tier or is kept by **any** flat rule (union of keeps — GFS can only add protection, never delete more than a flat rule intended). Deletion candidates outside every bucket are annotated `not retained by gfs` in `retention preview`. A GFS-only job (no flat rules) is validated and processed, including the automatic post-scheduled-backup sweep. Pure-domain change in `internal/domain/retention` (`GFSPolicy` + `Policy.GFS`); no new ports, no adapter changes. Runbook: [`docs/runbooks/retention-gfs.md`](docs/runbooks/retention-gfs.md). (PRD 32)
- **mongo backups upload to remote storage**: `mongodump` now honors `--storage s3|gcs|azure|google-drive`. Archive-mode output stages under `<backup_path>/.staging/<job-id>/` and streams to the configured backend via `StorageBackend.Upload`; staging dir is cleaned on success and failure. Local-backend behaviour unchanged. (Feature 024)
- **monitor schema migration framework**: monitor history DB now carries an explicit `schema_version` integer that is gated on every open. Stale DBs are migrated under a cross-process file lock (`<db>.migrate.lock` via `internal/lock`); forward-incompatible DBs (`current > BinarySchemaVersion`) are refused before any read/write with `monitor.ErrForwardIncompatible`, surfaced at the CLI with a what/why/how block and a non-zero exit. The previous silent legacy-fallback INSERT path in `RecordRestoreExecution` is removed — restore rows always carry `restore_mode` / `planning_status`. New `sentinel monitor doctor [--repair] [--json]` inspects state with stable exit codes (`0/1/2/3/4` for current/stale/forward-incompat/missing/corrupt) and idempotent migration application. Runbook: [`docs/runbooks/monitor-schema-migration.md`](docs/runbooks/monitor-schema-migration.md). (PRD-11 / spec 017)

### Changed (breaking)

- **`additional_args` parser**: replaced the naive regex (`"[^"]*"|\S+`) in `internal/backup/args.go` and the four ad-hoc `parseCLIArgs` (`strings.Fields`) helpers in `pkg/restore/{pg,mysql,mariadb,mongo}_restore/` with `github.com/google/shlex`-backed POSIX tokenization. Quoted values with embedded whitespace (`--exclude-table-data="audit logs"`, `--where="updated_at > '2026-01-01'"`) now reach the dump/restore tool as a single argument with surrounding quotes stripped — matching how every other CLI tool handles arguments. Unterminated quotes are rejected at config-load time (via `ValidateRestoreJob`) AND at job-execution time with `backup.ErrUnterminatedQuote`; NUL bytes are rejected with `backup.ErrNULByte`. Variable expansion (`$VAR`), command substitution (`` `cmd` ``, `$(cmd)`), and glob expansion are NOT performed — those characters pass through literally. **Breaking** for operators whose configs relied on the prior regex leaking surrounding quote characters into `argv`; in practice the leaked quotes were always cosmetic on the wire and the change is a strict correction. Run `sentinel config validate` after upgrading. Reference: [`docs/runbooks/additional-args.md`](docs/runbooks/additional-args.md), [ADR 0009](docs/adr/0009-shlex-args-parser.md). (PRD-16 / spec 023)

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
