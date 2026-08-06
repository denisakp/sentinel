# PRD I13 — Concurrent staging disk budget + parallel bound

**Severity:** Medium (N parallel restores can exhaust the disk mid-restore)
**Status:** Open
**Issues:** #177, #179.6 (duplicate of #177's secondary claim)
**Area:** `internal/adapters/restore/runtime/staging.go`, `internal/cli/restore.go`

## Problem

### #177 — parallel restores each check disk capacity alone

`runtime/staging.go:327-344` `ensureStagingCapacity(dir, required)` calls `availableStagingBytes(dir)`
(`statfs`, `diskspace_unix.go:9`), which reports the **full remaining free space on the filesystem**,
and compares it against that single job's `required` bytes. There is no shared or reserved budget
across concurrently staging jobs.

Both call sites (`staging.go:86` and `:150`) are per-job, invoked once per job inside the `--all`
fan-out. Five jobs each needing 30 GB against 100 GB free: all five preflight checks pass, and the
disk fills mid-restore.

`handleRestoreRunAll` fans out over a `chan struct{}` semaphore reusing `runOneRestoreJob`
(spec 045 / PRD 31) — the concurrency mechanism exists; the capacity accounting does not
participate in it.

### #179.6 — `--parallel` has no upper bound

`internal/cli/restore.go:416-422` `effectiveRestoreConcurrency`: `if parallelFlag > 0 { return parallelFlag }`.
No clamp anywhere in the file. `internal/config/validator.go:53-54` bounds
`max_concurrent_restores` to [1,100]; the flag that overrides it is unbounded.

Same duplicate confirmed independently by two agents. `restore run --all --parallel 100000` fans out
without complaint.

## Related, from I04

`restore run --all` reads the **top-level** `MaxConcurrentRestores` (`restore.go:420-421`) while the
scheduler reads `cfg.Scheduler.MaxConcurrentRestores` (`schedule.go:93`). Two keys for one concept —
the same shape as #141 in I12. Worth reconciling in this PRD since it is the same function.

## Fix

1. Reserve and decrement a **shared, mutex-guarded capacity budget** for the duration of staging in
   `handleRestoreRunAll`'s fan-out, **or** serialise the staging step while keeping the apply step
   parallel. The second is simpler and costs wall-clock; the first is correct and needs care around
   the release path on error.
2. Apply the same [1,100] bound to `--parallel` that `max_concurrent_restores` gets in
   `validator.go:53-54`.
3. Reconcile the two `MaxConcurrentRestores` keys, or document why `restore run --all` and the
   scheduler read different ones.

## DECISION REQUIRED

**Shared budget versus serialised staging.** Serialising staging is a few lines and cannot deadlock;
it makes `--parallel N` mean "N parallel applies" rather than "N parallel everything", which is a
user-visible semantics change worth a release note. A shared budget preserves the current semantics
and is the more invasive change. Pick before implementing — they are not compatible designs.

## Definition of done

- **Code:** the three items.
- **e2e:**
  - `--all --parallel N` with N jobs whose staged sizes individually fit but collectively exceed
    free space — a clean **pre-flight** failure, not a mid-restore `ENOSPC`. **NEEDS_LIVE** (needs a
    constrained filesystem; a small tmpfs mount is enough).
  - `restore run --all --parallel 100000` — rejected with a bound error, matching
    `max_concurrent_restores`.
- **Docs:** `guides/parallel-restore.md`. Neither issue has an admonition today; the parallel-restore
  page describes the capacity check as a safety net.
