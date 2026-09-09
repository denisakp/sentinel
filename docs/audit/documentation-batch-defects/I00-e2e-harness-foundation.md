# PRD I00 — e2e harness foundation

**Severity:** Critical (this is why 73 defects shipped undetected)
**Status:** Items 1, 3 and 5 DELIVERED by spec 061 (2026-09-09). Items 2, 4, 6 and 7 remain, and land
with I09, I14, I04 and I09 respectively.
**Issues:** #175 (root cause of the whole batch), context in #176
**Area:** `scripts/e2e.sh`, `tests/integration/`, `Makefile`, `.github/workflows/integration.yml`
**Blocks:** every other PRD in this directory

## Problem

The e2e suite reported success across surfaces that were, at the same time, entirely broken. This
is not a coverage shortfall to be topped up. Three structural mechanisms make whole defect classes
invisible.

### 1. Part of the suite never executes

`test_unit` in `scripts/e2e.sh` runs `go test ./...`. That command **silently skips every file
behind `//go:build integration`**. Those files run only via `.github/workflows/integration.yml`
(`go test -tags integration ./tests/integration/...`). A local `make e2e` therefore reports green
having never compiled, let alone run, the integration suite.

### 2. The incremental coverage is scenery

`grep incremental scripts/e2e.sh` returns 0. `grep` across `tests/integration/incremental/` returns
22 hits — and **every scenario in that directory is a `t.Skip("...scaffold...")`**. Chain reset,
fallback confirmation, mysql binlog replay, postgres chain backup+restore: all dead code that reads
as coverage to anyone grepping for it.

### 3. The one PITR test cannot see the PITR bug

`tests/integration/postgres_pitr_restore_test.go` builds its own manifest fixture via
`WritePITRManifest` with `RestoreMode: "pitr"`, then exercises the planner. It never traverses the
backup-time manifest-writing path. #148 — `Capabilities` hard-coded to `{"full","incremental"}` at
`internal/domain/backup/pipeline.go:226` — lives exactly there. The test is structurally incapable
of failing on the bug it appears to cover.

### 4. The default assertion is exit-code-only

`scripts/e2e.sh` is 1295 lines with 28 `assert_exit_ok` calls against ~30 outcome-based checks
(7 `assert_file_nonempty`, 4 `assert_output_contains`, ~19 hand-rolled magic-byte / `sqlite3`
rowcount / data-match blocks). Roughly 48% / 52% by call count — but the outcome checks cluster in
three late-added suites (verify_all, compression, remote-encrypted-backup, gfs). The backup,
restore-dryrun, retention-preview, monitor, storage-status, s3, gcs and azure suites are
exit-code-only.

**A command that validates a config key, silently ignores it, and exits 0 is indistinguishable from
success under `assert_exit_ok`.** That is the dominant shape of this defect batch.

## Uncovered surfaces

| Surface | Hits in `scripts/e2e.sh` | Would have caught |
|---|---|---|
| `pitr` | 0 | #148 |
| `restore_mode` | 0 | #148, #186 |
| `chain-status` / `chain-list` / `force-full` | 0 each | #136 |
| `incremental` | 0 (22 in `tests/integration/`, all `t.Skip`) | #150, #185, #190 |
| `binlog` / `oplog` | 0 (skip-scaffolds only) | #190, #191 |
| `dry_run` | 0 (`--dry-run`: 8, exit-code-only except gfs) | #157 |
| `lock` | 4, none a concurrency test | #163, #196 |
| `notification` | 0 | #187 |
| `monitor export` | 3, but via `--output`, never a shell `>` redirection | #165 |
| `azure` | 14, connectivity only — the script's own comment admits no backup is exercised | #161 |
| `gcs` | 17, no delete-verification | #182 |
| `gdrive` | 0 | #169 |

## Delivery status

Spec 061 delivered the first slice on 2026-09-09. What shipped, and three findings that corrected
this PRD's own assumptions:

| Item | Status |
|---|---|
| 1, make the integration suite run | **Delivered.** `scripts/e2e.sh` gained a `test_integration` stage that always compiles the integration-tagged tests module-wide, runs them when the four database client binaries are present, and reports the stage as *did not run* when they are not. |
| 3, `assert_rejected` | **Delivered.** Matches the error stream alone, so a refusal printed to stdout fails as a named stream fault. All four failure cases verified. |
| 5, config-key reachability | **Delivered.** `tests/config_census/` accounts for all 228 settable paths: 77 connected with verified evidence anchors, 151 recorded `unverified` under a ratcheting budget. |
| 2, the `t.Skip` scaffolds | Remains with I09. The six scaffolds are recorded in the skip allowlist meanwhile, so they no longer read as coverage. |
| 4, shell-redirection assertion | Remains with I14. |
| 6, concurrency test | Remains with I04. |
| 7, real-backup PITR fixtures | Remains with I09. |

**Three corrections to this PRD, found by measurement:**

1. **The schema is roughly twice the size implied here.** There are 204 schema-tagged fields across
   three files, not one: `types.go` has 133, `restore_types.go` has **68**, and
   `mongo_secrets_file.go` has 3. Those resolve to 228 settable paths from the root. The restore-side
   keys were missing from every earlier estimate.
2. **The continuous-integration job has the same blind spot it was meant to compensate for.** It runs
   `go test -tags integration ./tests/integration/...`, and two integration-tagged files live outside
   that path, under `internal/adapters/storage/`. Demonstrated: a compile error placed in one of them
   is invisible to plain `go vet ./...` **and** to the path-scoped command CI uses, and is caught only
   by the module-wide form the new stage runs.
3. **Making the suite run is not sufficient, because the tests exclude themselves.** Thirteen skip at
   runtime: 8 on absent credentials, 5 of the 6 scaffolds plus one more. They were absorbed into a
   passing result. The allowlist now makes each one declared, named and non-growable.

**A fourth finding, from the census rather than the harness:** the reachability check independently
reproduced issue **#143**. `RestoreConfiguration`, a parallel "extends Configuration" struct, is
constructed by nothing, and it is the only route to `RestoreDefaults`. Every key under either is
unsettable by any operator.

## Fix

Ordered. Items 1, 3 and 5 are the cheap half and must land before the bug-fix batch starts; they
require no new fixtures and cover the widest defect class.

1. **Make the integration suite run.** Either have `test_unit` also run
   `go test -tags integration ./tests/integration/...`, or document explicitly why it stays in a
   separate workflow — and then ensure that workflow is a required check.
2. **Make the five `t.Skip` scaffolds in `tests/integration/incremental/` fail loudly.**
   *RESOLVED 2026-08-06.* Implementing them needs the real PITR/incremental fixtures, which belong
   to I09. Rather than deleting the scaffolding or leaving it as a false signal, each one fails with
   a message pointing at I09. That removes the "grep finds coverage" illusion without discarding the
   skeleton work. They convert to real assertions when I09 lands its fixtures.
3. **Add `assert_rejected`** — the mirror of `assert_exit_ok`: requires non-zero exit **and** a
   `grep` match on stderr. Every command that validates and rejects input gets one. This is what
   catches the "validated then discarded" class (#172, #168, #170.x).
4. **Add a real shell-redirection assertion** for `monitor export ... > file` (not `--output`),
   which is the only form that detects stdout/stderr misrouting (#165).
5. **Add a config-key-reachability test**: for every YAML key the validator accepts, assert it
   changes *some* observable runtime behaviour. Fixture-diff, not "validates ok". This is the
   generic catcher for the entire dead-config class (#141–#155, #172, #194).
6. **Add one concurrency test**: two `backup` runs on the same job, assert the second is blocked or
   serialised rather than silently racing (#163).
7. **Add PITR / chain / force-full assertions that traverse the real
   backup → manifest → restore pipeline**, not a hand-built manifest fixture.

## Minimal shared scaffold

So that each later PRD adds an assertion in a few lines rather than building its own fixture:

- `assert_rejected <cmd> <expected-stderr-pattern>` — one-line use per negative test.
- `assert_config_key_changes_behavior <key> <value> <observable>` — ~3 lines per dead-config fix.
- A **real-backup** PITR/incremental fixture builder that runs `sentinel backup` (not
  `WritePITRManifest`) against the containers already up in `docker-compose.yml`, producing a
  genuine manifest chain other tests can extend.
- `assert_blocks_when_concurrent <cmd>` — run a command twice concurrently, assert one blocks.

## Acceptance

- `make e2e` executes the integration-tagged tests, or a required CI check does and the split is
  documented in `scripts/e2e.sh`.
- `grep -c "t.Skip" tests/integration/incremental/` is 0 — each scenario now fails with a message
  naming I09, or is a real assertion.
- The four scaffold helpers exist and are used by at least one test each.
- A PITR fixture exists that is produced by a real `sentinel backup` invocation.
- `go vet -tags=integration ./...` passes (integration files compile).

## Sequencing

Items **1, 3, 5** land before any bug-fix PRD. Items **2, 6, 7** land alongside the PRDs that need
them: I09 (incremental/PITR fixtures), I04 (concurrency), I05 (retention deletion verification).
