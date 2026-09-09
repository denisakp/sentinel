# PRD I01 — CLI honesty: exit codes, stdout routing, usage noise

**Severity:** High (unattended and scripted use cannot detect failure)
**Status:** Open
**Issues:** #165, #168 (= #170.4), #170.1, #170.2, #170.3 (= #167.4), #179.7, #181.1, #181.5, #170.6
**Area:** `internal/cli/{monitor,security,storage_cmd,repair,retention_helpers,backup_verify,version,root}.go`
**Blocked by:** I00 (items 1, 3 — `assert_rejected` is the helper every test here needs)
**Blocks:** I05, I14

## Problem

Across the CLI, success and failure look the same. A command refuses to act, or acts and fails, and
still exits 0 with its output on the wrong stream. In cron and CI this is silent.

This PRD lands early and on purpose: **until failures are observable, no later fix in this
directory can be verified.**

## Confirmed defects

All reproduced against the reference binary.

| # | Defect | Cause | Evidence |
|---|---|---|---|
| #165 | `monitor list/show/stats/export` write to stderr | `internal/cli/monitor.go` uses `cmd.Print*` without `SetOut`; Cobra defaults to `OutOrStderr()` | `monitor list --format json > out.json 2>err.log` → out.json **0 bytes**, err.log 930 bytes holding the JSON, exit 0 |
| #170.2 | `storage status` exits 0 with an unreachable backend | `storage_cmd.go:197` returns `nil` unconditionally | unreachable S3 endpoint → printed `reachable:false` with a populated error, exit 0 |
| #170.3 (=#167.4) | `repair --job <unknown>` reports "no drift" | `repair.go:684-718` `collectRepos` returns an empty list, no existence check | `repair --job doesnotexist` → "No drift detected." / "0 finding(s)", exit 0 |
| #168 (=#170.4) | `retention apply` without `--job` swallows per-job errors | `retention_helpers.go:91-99` prints a warning then `return nil`; the `--job` path correctly returns the error | source; the two paths differ by construction |
| #170.1 | `security init-key` exits 0 when it refuses | `security.go:30-34` `return nil` on the overwrite guard | `SENTINEL_MASTER_KEY=x security init-key` → "Warning: … already configured", exit 0 |
| #170.5 | `security init-key --output <anything>` silently falls back to text | `security.go:41` only tests `== "json"`, no validation | `--output bogusformat` printed text, exit 0, no file created |
| #181.1 | Same failure, two exit codes | `backup_verify.go:288-290` maps single-ID `missing_artifact` to `ErrVerifyInternal` (4); the `--all` sweep at 509/518 maps it to `ErrVerifyIntegrityFailed` (5) | source, both branches read |
| #181.5 | `sentinel version foo bar` exits 0 | `version.go` sets no `Args`, Cobra accepts arbitrary positionals | ran the binary, printed the version block, exit 0 |
| #179.7 | Every business-logic error prints the full usage block | `root.go:101` sets `SilenceErrors = true` but never `SilenceUsage` | `monitor stats` without `--job` printed the whole usage+flags block before the error |
| #170.6 | `retention preview` and `retention apply` print the same wording | `retention_helpers.go:98` shares `"total deleted: %d backups"` across both paths | source |

## Root cause

Pattern **B** from the index. Two habits, repeated independently:

- `return nil` is used for "I declined to act" and for "a sub-task failed", instead of a distinct
  non-nil error. The information exists (`summary.Errors`, `entry.Reachable`) and is printed, then
  discarded from the return value.
- `cmd.Print*` is used without `SetOut`, so Cobra's default sends it to stderr. The correct pattern
  already exists in this codebase — `monitor_doctor.go:37` and `repair.go` use `OutOrStdout()`.

## Fix

1. `root.go`: set `RootCmd.SilenceUsage = true`. **One line, do it first** — it makes every other
   error in this batch readable.
2. `monitor.go`: route `list`, `show`, `stats`, `export` through `cmd.OutOrStdout()`, matching
   `monitor_doctor.go`.
3. Propagate errors instead of swallowing them: non-nil return when `len(summary.Errors) > 0`
   (`retention_helpers.go`), when any `entry.Reachable == false` (`storage_cmd.go`), and on the
   `init-key` overwrite refusal (`security.go`).
4. `repair.go`: validate `--job` against `cfg.Databases` up front; error on an unknown name.
5. `security.go`: validate `--output` against `{json,text}`.
6. `backup_verify.go` + `exit_codes.go`: the single-ID path returns `ErrVerifyIntegrityFailed` for
   corrupted/missing artifacts; reserve `ErrVerifyInternal` for genuine operational failures.
7. `version.go`: add `Args: cobra.NoArgs`.
8. `retention_helpers.go`: label the dry-run path "would delete" / "total previewed".

## DECISION REQUIRED

Changing exit codes is a **breaking change for anyone scripting against them today**. Two of these
change a 0 to a non-zero (`storage status`, `retention apply`), and #181.1 changes a 4 to a 5.
Confirm whether this lands in a minor release with a release-note callout, or waits for a major.

## Definition of done

- **Code:** the eight fixes above.
- **e2e** (each ships in the same PR, using `assert_rejected` from I00):
  - `monitor list --format json > f` — `f` is non-empty, valid JSON.
  - `storage status` with one unreachable backend — exit non-zero.
  - `retention apply` (all jobs) with one job's delete forced to fail — exit non-zero, stderr names
    the job.
  - `repair --job <unknown>` — exit non-zero with an explicit "unknown job".
  - `security init-key` with the key env var set and no `--force` — exit non-zero.
  - `backup verify <id>` with the artifact deleted — exit 5, matching `--all` on the same fixture.
  - `sentinel version foo` — exit non-zero.
  - any `RunE` business-logic error — stderr does not contain `Usage:`.
- **Docs:** `website/docs/` pages citing #165 (troubleshooting.md, chain-corruption-recovery.md,
  failed-backup-triage.md, inspect-monitor-history.md, monitoring-history.md) and #170
  (troubleshooting.md, chain-corruption-recovery.md, state-repair.md, check-storage-backend.md,
  key-loss-incident.md). Remove the admonitions the fixes invalidate.
- **Runbooks:** re-grep `docs/runbooks/` for the affected commands.
