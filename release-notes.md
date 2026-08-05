# Sentinel Release Notes

## [v1.4.0] - August 5, 2026

### Added

- **MySQL/MariaDB credential loading from a my.cnf defaults file** (`defaults_file`):
  a new per-job config field, valid only for `mysql`/`mariadb`, that seeds
  `host`/`user`/`password`/`port` from a standard option file's `[client]`
  section for the **entire** backup pipeline — the pre-backup connectivity
  check, `database: "*"` auto-discovery, and the dump itself — not just the
  dump subprocess (closing GitHub issue #28). Previously, the existing
  `additional_args: "--defaults-extra-file=..."` passthrough only reached
  `mysqldump`/`mariadb-dump` directly; Sentinel's own Go-side preflight and
  discovery never saw those credentials, so `database: "*"` auto-discovery
  was entirely blocked when credentials lived only in a my.cnf file.
  Precedence: explicit job fields (`host`/`host_env`, `username`/
  `username_env`, `password_env`) always win; `defaults_file` only fills
  fields left unset. A missing, unreadable, or malformed `defaults_file`
  fails at **configuration-load time**, never partway through a live backup
  run. A group- or world-readable file emits the same permission warning as
  the existing `--password-file` channel. Only the `[client]` section is
  parsed (no `[mysqldump]`-style tool sections, no `!include`/`!includedir`
  directives) — a documented v1 limitation, not a gap. Rejected by config
  validation on any non-mysql/mariadb job. No new port; zero behavior change
  for jobs that don't set `defaults_file`. Runbook:
  [`docs/runbooks/credentials.md`](docs/runbooks/credentials.md).
  (spec 056 / PRD 44)
- **MongoDB credential loading from a secrets file** (`mongo_secrets_file`):
  a new per-job config field, valid only for `mongodb`, supplying a password,
  a full connection URI, and/or a TLS private-key passphrase from one small
  Sentinel-native YAML file, composed into the job's connection string for
  the **entire** pipeline — the pre-backup connectivity check, database
  discovery, and the dump itself (closing GitHub issue #27). Precedence is
  per-field: an explicit `uri`/`uri_env` always wins and the file's `uri` is
  simply unused when present; the file's `password` only fills a URI that
  has a username but no password — a URI that already has one, or no
  resolvable username at all, is a **configuration-load-time error**, never
  a silently-guessed connection. A missing, unreadable, or malformed file
  fails at config-load time, never partway through a live backup run. Same
  group/world-readable permission warning as the other credential-file
  channels. Rejected by config validation on any non-mongodb job. The
  `ssl_pem_key_password` field is resolved through the same mechanism as the
  existing `tls.client_key_password_env` option, but — documented honestly —
  neither currently reaches a live MongoDB TLS connection on the
  config-driven backup path; that is a separate, pre-existing wiring gap,
  unrelated to and not fixed by this change. No new port; zero behavior
  change for jobs that don't set `mongo_secrets_file`. Runbook:
  [`docs/runbooks/credentials.md`](docs/runbooks/credentials.md).
  (spec 057 / PRD 45)
- **Environment-variable indirection for secrets-file paths** (`defaults_file_env`,
  `mongo_secrets_file_env`): the location of the MySQL/MariaDB defaults file
  and the MongoDB secrets file can now be supplied via an environment
  variable instead of a literal path in configuration — for containerized
  and Kubernetes deployments where a secret's mount location is only known
  at runtime, closing GitHub issue #29. Same precedence as every existing
  `*_env` field: when set, the environment-resolved path overwrites the
  literal one; an `_env` field referencing an unset or empty variable fails
  immediately at configuration-load time, naming the job and the variable —
  never a silent fallback or a delayed failure during a live backup run.
  Reuses the existing, already-proven env-override resolution unchanged; no
  new machinery, no new port, zero behavior change for jobs that don't set
  either `_env` field. (spec 058 / PRD 46)
- **Optional at-rest encryption for DB-credential secrets files**
  (`security encrypt-secrets-file`): the MySQL/MariaDB `defaults_file` and the
  MongoDB `mongo_secrets_file` can now be stored **encrypted at rest** and are
  decrypted **in memory** at configuration-load time, closing the standing
  "plaintext database passwords on disk" exposure (GitHub issue #30). A new
  `sentinel security encrypt-secrets-file <path> --out <path>` command produces
  the encrypted file, writing to a **new path** and never modifying or deleting
  the plaintext input. Encryption is **auto-detected** from the file's content
  (a self-contained `SSEC` container that embeds its own decrypt material — no
  sidecar), so a plaintext file keeps working unchanged and needs no key. The
  key is resolved from a dedicated `secrets_key_env`/`secrets_key_file`, falling
  back to the backup-artifact `encryption_key_env`/`encryption_key_file`, so the
  two keys can rotate independently. Reuses the shipped AES-256-GCM crypto
  verbatim — no new cipher, key format, or generator; a key from
  `security init-key` works directly. A wrong/missing key or a
  corrupt/truncated/unsupported file fails at config-load time with a single
  clear error naming the file — never a partial parse, never a silent fallback,
  never a delayed failure during a live run. The decrypted plaintext never
  touches disk. The group/world-readable permission warning now fires only on
  **plaintext** secrets files (an encrypted file is ciphertext). Backup-side
  only; unrelated to and does not alter backup-artifact encryption. Runbook:
  [`docs/runbooks/credentials.md`](docs/runbooks/credentials.md).
  (spec 059 / PRD 47)

### Security / Supply chain

- **signed release checksums (cosign keyless)**: each release now signs its
  `checksums.txt` with [cosign](https://github.com/sigstore/cosign) keyless
  signing (GitHub Actions OIDC → Fulcio short-lived cert, logged in Rekor) and
  publishes a single sigstore bundle `checksums.txt.sigstore.json` alongside
  the existing assets. Downloaders can now cryptographically verify a release
  originated from Sentinel's release workflow — not merely that an archive
  matches an otherwise-unsigned checksums list — with
  `cosign verify-blob --certificate-identity-regexp '…/release.yml@refs/tags/.*' --certificate-oidc-issuer https://token.actions.githubusercontent.com --bundle checksums.txt.sigstore.json checksums.txt`
  (full command in README → Installation → "Verify the release signature").
  One signature over `checksums.txt` transitively covers every archive (verify
  the bundle, then `sha256sum -c`). Signing is **fail-closed**: if signing
  cannot complete, the release publishes nothing, and a post-publication CI
  step re-verifies the bundle against the pinned workflow identity so a broken
  config fails the run. The existing `checksums.txt` (SHA-256) and its
  `sha256sum -c` path are **unchanged** and remain valid for users without
  cosign; releases published **before** this change ship no bundle and stay
  checksum-only. No product/`internal` code change — release pipeline + docs
  only. (spec 053 / PRD 42)
- **SLSA build provenance**: each release now also publishes a
  [SLSA](https://slsa.dev) build-provenance attestation (`multiple.intoto.jsonl`),
  complementary to (not a replacement for) the cosign signature above:
  signing proves *who published* a release, provenance proves *how it was
  built* — the exact source commit, repository, and build workflow, attested
  by a process with its own identity, independent of the job that produced
  the binaries, so a compromise of the build job alone cannot forge its own
  attestation. Generated via the pinned `slsa-framework/slsa-github-generator`
  reusable workflow as a **second, separate** release-pipeline job
  (`needs: [goreleaser]`); the attested artifact set exactly mirrors
  `checksums.txt` (verbatim base64 of the file — no independent re-hash).
  Verify with
  `slsa-verifier verify-artifact <archive> --provenance-path multiple.intoto.jsonl --source-uri github.com/denisakp/sentinel --source-tag v<version>`
  (full walkthrough in README → Installation → "Verify build provenance").
  **Fails closed**: a tampered artifact or an attestation claiming an
  unexpected source repository/tag is rejected. If provenance generation
  fails after the binaries/checksums/signature already published
  successfully, those already-valid assets remain published unchanged — a
  provenance-only failure is visibly flagged (a failed job on the run) but
  never retroactively unpublishes anything. Existing release outputs
  (binaries, `checksums.txt`, cosign signature) are byte-for-byte unchanged;
  releases published **before** this change ship no attestation. No
  product/`internal` code change — release pipeline + docs only.
  (spec 055 / PRD 48)

### Security

- **`sentinel security reencrypt` (guided envelope migration + key rotation)**: a
  new command that re-encrypts existing backups, closing the forward-reference
  the v1.3.0 Envelope-v2 advisory opened ("a dedicated `sentinel security
  reencrypt` helper is tracked under a separate PRD"). Two modes: **legacy → v2
  migration** (`--mode legacy`, default) re-wraps a pre-v2 (legacy) artifact into
  the current envelope so it restores/verifies under the default refuse-legacy
  policy **without** `--allow-legacy-envelope` — for operators who cannot
  re-backup from source (source DB gone, PITR window closed, artifact is the only
  copy); and **key rotation** (`--mode rotate --new-key-env <VAR>`) re-encrypts a
  current-format backup under a new master key (compromised/retired key), new key
  read **env-only**, never from argv. Addressable by single `<backup-id>` or
  `--all` (scoped by `--job` / `--since`); `--all` requires `--yes`; `--dry-run`
  classifies (legacy / current / unencrypted / unmigratable) and mutates nothing;
  `--output json` for automation. **Safety**: write-new → verify (decrypt-back +
  SHA-256) → swap — the original artifact is never destroyed until a verified
  replacement exists (`--keep-original` retains it), the operation is idempotent,
  and a manifest-less backup is skipped with "cannot migrate — re-backup from
  source" rather than corrupted. In `--all`, one backup's failure doesn't abort
  the batch (exit `5` if any failed, `4` for invalid invocation, `0` otherwise).
  Each success emits a `security.reencrypt` audit log event and updates the
  monitor row. **Honest scope**: legacy migration fixes format/availability, it
  does **NOT** remediate the legacy envelope's confidentiality weakness — a loud
  caveat prints on every legacy run and re-backup-from-source remains the true
  remediation. Pure composition of existing crypto/storage/manifest/monitor code
  — no new port, no change to the backup/restore executors. Runbook:
  [`docs/runbooks/recover-legacy-envelope.md`](docs/runbooks/recover-legacy-envelope.md).
  (spec 054 / PRD 43)

## [v1.3.0] - August 3, 2026

### Performance

- **backup hashing**: compute the plaintext manifest hash inline with the dump write, eliminating a second full read of the artefact. Each engine adapter (`pg_dump`, `pg_dumpall`, `mysqldump` single + `--all-databases`, `mariadb-dump` single + `--all-databases`, `mongodump` local) now returns `(digest, error)` where `digest` is `hex(sha256(payload))` of the bytes handed to storage; the orchestrator stores it in `.manifest.json` as-is. The encrypted path (AES-256-GCM) and `sentinel backup verify` are unchanged. (PRD 19)

### Deprecated

- **`sentinel backup --password` / `-p`**: deprecated; will be removed in the next minor release. Passing a password on the command line exposes it via `ps`, `/proc/<pid>/cmdline`, and shell history. Use one of three safe channels instead: `--password-env <VAR>`, `--password-file <PATH>` (first line, right-trimmed; warns on group/world-readable mode), or the existing config field `databases.<id>.password_env`. CLI flag precedence over config is preserved (silent override). Conflicts among `--password`, `--password-env`, `--password-file` are hard errors before any DB I/O. Migration recipes: [`docs/runbooks/credentials.md`](docs/runbooks/credentials.md). (Feature 015)

### Security

- **remote-storage backups bypassed encryption + integrity (silent plaintext upload)**: config-driven backups to a remote backend (S3, GCS, Azure Blob, Google Drive) previously uploaded **plaintext, unhashed, unmanifested** artifacts even when `encryption_key_env` was configured — a silent security bypass on exactly the backends where at-rest encryption matters most: the run reported success while the object landing in the bucket was readable SQL. Root cause: the post-dump security step (hash → encrypt → manifest) ran only for **local** storage and returned early for any remote backend, discarding the computed plaintext digest. Fixed via stage-then-upload — every dump engine now writes to a local staging file, the domain executor applies hash → AES-256-GCM encrypt → manifest on that staged artifact, then uploads the (encrypted) artifact **and** a `<name>.manifest.json` sidecar through `ports.StorageBackend` and cleans up staging on both success and failure. Integrity (hash + manifest) is now produced for **every** remote backup, encrypted or not, so `sentinel backup verify` (which now fetches the remote artifact + sidecar before validating) and restore (fetch → decrypt → restore) work end-to-end. A fail-loud backstop (FR-008) refuses to upload whenever encryption is configured but cannot be applied — never degrading to a plaintext upload — including the auto-discovery `strategy: single` path, which writes a combined dump straight to the backend and cannot be staged in place (use `strategy: individual` or local storage). Backward-compatible (FR-010): pre-fix remote backups (plaintext, no sidecar) remain restorable — the S3 `Download` not-found error is now mapped to `ErrSourceObjectNotFound` so an absent manifest is tolerated, not fatal. Local-storage backups are byte-for-byte unchanged. Runbook: [`docs/runbooks/enable-encryption.md`](docs/runbooks/enable-encryption.md). (spec 047)
- **mariadb dump**: migrated MariaDB backup adapter (`pkg/backup/mariadb_dump`) from `--password=<value>` argv to `MYSQL_PWD` env injection, closing the last argv-leak path across supported engines. PostgreSQL (`PGPASSWORD`), MySQL (`MYSQL_PWD`), and MongoDB (URI) channels unchanged. Verified via argv-inspection unit tests in `pkg/backup/mariadb_dump/args_builder_test.go` and `pkg/backup/mariadb_dump/env_injection_test.go`. (Feature 015)
- **dump adapters**: redact credentials embedded in subprocess stderr before they reach returned errors, logs, the monitor SQLite store, or notifier payloads. Previously, a failing `pg_dump` / `pg_dumpall` / `mysqldump` / `mariadb-dump` / `mongodump` (incl. oplog) invocation could leak `PGPASSWORD=…`, `MYSQL_PWD=…`, `MONGO_INITDB_ROOT_PASSWORD=…`, libpq `password = …`, `--password=…`, `-p<value>`, or URI userinfo (`scheme://user:secret@host`) into operator-visible output. All eight dump-adapter callsites now route stderr through `sanitize.RedactStderr`, which caps the embedded buffer at 64 KiB with an explicit truncation marker. CI gate (`make lint-redact-stderr`, wired into `.github/workflows/integration.yml`) prevents regression. Severity: medium (local-file disclosure on failure); no behavior change on successful backups. Audit artifact: `docs/audit/stderr-redaction-audit.md`. (Feature 012)

### Added

- **`verify_after_upload` (post-upload re-download + re-hash)**: opt-in integrity guard that closes the silent-corruption blind spot for storage-side write faults (a truncated S3 multipart PUT, a dropped connection mid-upload, a bit-flip, a bucket that silently drops the object). After a backup artifact is written/uploaded, Sentinel re-downloads it from **its own** storage backend and re-hashes it against the manifest SHA-256, failing the job with reason `verify_after_upload_failed` on mismatch — so a corrupt upload surfaces **at backup time**, not weeks later at the next restore (by when retention may have pruned the last good copy). Enable via a new `integrity.verify_after_upload` default with a per-job `verify_after_upload` override (`*bool`, retention-style inheritance; nil = inherit). Default **off** — when unset the backup Executor keeps a `nil` storage port and performs no re-download, so zero behaviour and zero added cost. On mismatch the stored object is **left in place** for forensics (never auto-deleted) and its path is surfaced in the error; the temp download is always removed on both the match and mismatch paths. The comparison hash covers the uploaded bytes for remote backends via the spec-047 stage-then-upload path (`sec.HashValue` already reflects the staged/encrypted artifact); the auto-discovery `strategy: single` remote dump-all path has no manifest hash and degrades to a no-op rather than a false failure. Allowed for local storage too (off by default; its real value is remote). Domain reaches storage only through `ports.StorageBackend` (ADR-0001 clean). Runbook: [`docs/runbooks/verify-backup-integrity.md`](docs/runbooks/verify-backup-integrity.md). (PRD 40)
- **published performance benchmarks + reproducible harness**: a committed, re-runnable benchmark harness and a published `docs/benchmarks/` matrix so an evaluator finds a close-to-their-case size/wall-clock/RAM figure in under 30 seconds — closing the long-standing "Sentinel publishes no performance numbers" gap. `scripts/benchmark.sh` (+ `make bench-small` / `bench-matrix`) reuses the `infra/docker` DB services and reads `duration_ms` / `file_size_bytes` from `sentinel monitor export`, wrapping the CLI in `/usr/bin/time -v` for peak-RAM/CPU (the engine binary runs as a subprocess, so the whole process tree is measured); `--tier small` (~1GB, CI-reproducible) vs `--tier large` (10/50GB, operator-run). Deterministic dataset generator `infra/dataset/generate.sh` (bash+awk, no Python/venv — byte-identical across runs). `docs/benchmarks/{README.md,v1.3.0.md}` document methodology (hardware, DB versions, storage=local) and an initial **measured** small-tier matrix (pg/mysql/mariadb/mongo backup-full + zstd + restore-full), with incremental/PITR rows explicitly `skipped` for the documented prerequisite each needs and honest caveats footnoted (peak-RAM scales with artifact size; synthetic-payload compression ratios are inflated vs real-world). README gains a Performance section; a `workflow_dispatch`-only `.github/workflows/benchmark.yml` runs the small tier report-only (does not gate existing push/PR CI). The docs explicitly distinguish these real-DB benchmarks from the `tests/benchmarks/` Go `testing.B` micro-benchmarks. Near-zero product-code change (docs + harness + fixtures). (PRD 41)
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
