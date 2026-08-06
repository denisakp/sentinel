# PRD I09 — Restore planner: PITR, incremental, dry-run, verification

**Severity:** Critical (two features advertised in the README can never succeed)
**Status:** Open
**Issues:** #148, #150, #186, #152, #149, #185, #190
**Area:** `internal/domain/backup/pipeline.go`, `internal/domain/restore/`,
`internal/adapters/restore/runtime/`, `internal/config/validator.go`, `internal/cli/restore.go`,
`internal/adapters/restore/incremental/mysqlbinlog/`
**Needs:** the real-backup PITR/incremental fixture from I00 item 7

## Problem

This is the highest-stakes cluster. PITR and incremental restore are documented in the README and in
the tutorials. Neither can complete. The e2e suite grep-matches both words and asserts neither
(see I00).

## Confirmed defects

### #148 — `restore_mode: pitr` can never be planned

`internal/domain/backup/pipeline.go:226` writes `Capabilities` as a **literal**
`[]string{"full","incremental"}`. There is one call site, no conditional branch, and no
recoverable-window fields are set anywhere. `"pitr"` can never appear. `AdvancedRestoreMetadata`
already has the fields.

### #150 — `restore_mode: incremental` fails staging its baseline

Path double-resolution:

1. `internal/domain/backup/executor.go:268` records
   `FilePath = filepath.Join(job.LocalPath, outName)` → e.g. `backups/shop.sql`.
2. `internal/domain/backup/planner.go:98-99` stores that as `BaselineBackupID`.
3. `runtime/staging.go`'s `listSourceObjects` lists **relative to `source.LocalPath`**, which is
   already `backups/`, returning `shop.sql`. No match.
4. The boundary fallback at `internal/domain/restore/source.go:43` compares `filepath.Base(obj.Path)`
   against the full `backups/shop.sql` id. Also no match. → not found.

And in a flattened layout, `matchesBackupIDBoundary` matches **both** `shop.sql` and
`shop.sql.manifest.json` against the id `shop` — ambiguous. Two fixes needed, not one.

### #149 — `verify_after_restore: true` always fails, after the data has landed

`grep "VerifyAfterRun:"` across non-test `internal/` returns **zero hits**.
`runtime/executor.go:239` assigns `djob.VerifyAfterRun` only when `req.VerifyAfterRun != nil`, and no
CLI or scheduler call site ever sets it.

**Broader than the title:** `internal/domain/restore/executor.go:320-323` sets `requiresVerification`
for `VerifyAfterRestore == true` **OR** `RestoreMode == "pitr"` **OR** `"incremental"`. So the
synthetic "verification handler is required" failure hits all three, not just the flag.

### #186 — `restore_mode: incremental` validates for every engine, only postgres can plan it

`internal/config/validator.go:319-326` whitelists postgres/mysql/mariadb/mongodb.
`internal/domain/restore/planner.go:110-113` `planIncremental` rejects anything but postgres with
`ReasonCodeUnsupportedDatabaseType`.

**The correct pattern is in the same file**: the `pitr` branch at validator.go ~300 restricts to
postgres only. Copy it.

### #152 — `restore dry-run` does not invoke the planner

`internal/cli/restore.go:297-321` `handleRestoreDryRun` prints config fields only. No planner call;
those appear only in the real run path at `restore.go:517`.

**Reproduced live:** `restore dry-run pg_pitr_job` with `restore_mode: pitr` → exit 0, printed
"Restore Mode: pitr", no planner error — for a job that #148 makes unplannable.

### #185 — every incremental prerequisite check is unreachable or never called

`internal/config/validator.go:346` calls `ValidateMySQLIncrementalPrerequisites(true, ...)` and
:351 calls `ValidateMongoIncrementalPrerequisites(true)` — `logBinEnabled` and `replicaSetEnabled`
**hard-coded true**. `ValidateMongoOplogWindowPrerequisite` and
`ValidatePostgresIncrementalPrerequisites` have zero non-test callers. `.BinlogCheck` is parsed and
never read.

### #190 — binlog archival matches only `mysql-bin` / `mariadb-bin`

`internal/adapters/restore/incremental/mysqlbinlog/archive.go:105` and `replay.go:195` hard-code
`strings.HasPrefix(name, "mysql-bin.")` / `"mariadb-bin."`.
`grep log_bin_basename` across `internal/` returns **zero**. The server is never asked.
**Stock MySQL 8 uses `binlog`.** A default MySQL 8 install archives nothing, silently.

## Two independent chains

**(a) Manifest-writing gap.** #148 is the root cause of the PITR half of #152's false-clean dry-run.

**(b) Restore-staging gap.** #150 is why incremental restore fails *after* a successful plan.
Independent of #148.

#186 and #185 are both "config accepts what the runtime rejects" (pattern E). #149 is orthogonal but
shares the symptom class: a restore misleads *after* real work has already landed on the target.
#190 is fully separate — no shared code — and ships independently.

## Fix order

1. **#148** — populate `"pitr"` in `Capabilities` and set `RecoverableWindowStartUTC`/`EndUTC` when
   the engine and config support it. This is the required path; see the resolved decision below.
2. **#150** — store and resolve the baseline id relative to the same root the restore listing uses,
   and exclude `*.manifest.json` from candidate matching in `ResolveChainObject`.
3. **#186** — restrict the validator's incremental whitelist to postgres, matching the pitr branch.
4. **#152** — have dry-run invoke the same planner path as run and print the plan status and reason
   code. Do this **after** 1-3, so dry-run starts telling the truth about a system that works.
5. **#149** — wire a real verification handler in `runtime`. Not optional: #148's acceptance test
   cannot pass without one.
6. **#185** — probe real server state (`log_bin`, replica-set membership, oplog window) before
   accepting `incremental_backup` config, or remove the dead keys and functions.
7. **#190** — query `SHOW VARIABLES LIKE 'log_bin_basename'` (or read the binlog index) instead of
   guessing. Independent, can land at any point.

## Decisions

Two resolved, one still open.

- **#148 — RESOLVED 2026-08-06: implement.** PITR is a required feature and is not to be withdrawn
  under any circumstance. Withdrawing it, rejecting `restore_mode: pitr` at config load, or removing
  it from the README are all off the table. `Capabilities` must be populated with `"pitr"` and the
  recoverable-window fields must be set. If the plumbing outgrows this PRD, it gets its own spec —
  it does not get descoped.
- **#149 — RESOLVED by consequence: implement.** This was "implement verification, or reject the
  flag". The reject option is now unavailable: `internal/domain/restore/executor.go:320-323` sets
  `requiresVerification` for `RestoreMode == "pitr"` as well as for the flag, so **a working PITR
  restore cannot exist without a verification handler**. A real handler must be wired in
  `internal/adapters/restore/runtime/`. Sequence it before or with #148's acceptance test, since
  that test cannot pass otherwise.
- **#185 — DECISION REQUIRED: probe, or remove the keys.** `wal_summary_check` and `binlog_check`
  currently read as supported safety features and perform no check. Pairs with #155 in I12 (the same
  dead prerequisite functions) — decide both together.

## Definition of done

- **Code:** the seven fixes in order.
- **e2e** (all need the real-backup fixture from I00 item 7, not `WritePITRManifest`):
  - Backup a postgres job, then restore with `restore_mode: pitr` inside the recoverable window —
    `PlanStatusReady`, not `missing_advanced_metadata`.
  - Full backup → incremental backup → incremental restore of the chain — staging succeeds, no
    `ErrAmbiguousBackupID` / `ErrSourceObjectNotFound`.
  - `restore run` with `verify_after_restore: true` against a real DB — succeeds, not the synthetic
    "verification handler is required" failure. **NEEDS_LIVE.**
  - A mongodb job with `restore_mode: incremental` — fails at **config validate**, not at run.
  - `restore dry-run` on a job that cannot be planned — reports the planner's rejection reason,
    exit non-zero.
  - MySQL 8 container with stock defaults (`log_bin_basename=binlog`) — an incremental backup
    archives more than zero binlog files. **NEEDS_LIVE.**
  - `config validate` against MySQL with `log_bin` OFF and a standalone mongod with
    `incremental_backup.enabled: true` — validation fails. **NEEDS_LIVE.**
- **Docs:** `operations/troubleshooting.md`, `operations/chain-corruption-recovery.md`,
  `concepts/incremental-pitr.md`, `concepts/manifest.md`, `tutorials/index.md`,
  `tutorials/mongodb/pitr.md`, `reference/glossary.md`, `guides/restore-from-gcs.md`,
  `guides/index.md`, `guides/parallel-restore.md`. The README's PITR section must match whatever the
  decision above resolves.
