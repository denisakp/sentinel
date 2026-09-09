# PRD I04 — Backup file locking, lock plumbing and job timeouts

**Severity:** High (concurrent backups corrupt each other; a live job can be marked dead)
**Status:** Open
**Issues:** #163, #142, #154, #194, #195, #196, #197
**Area:** `internal/cli/{backup_factory,schedule,repair}.go`, `internal/adapters/lock/`,
`internal/scheduler/`, `internal/config/loader.go`, `internal/adapters/monitor/`
**Blocks:** the reasoning in I05 and I10 about what "a job is running" means

## Problem

Two independent root causes, tangled.

**(a) Backups take no file lock at all.** `internal/cli/backup_factory.go:178` wires `nil` for
`ports.LockManager`. The domain Executor is ready for it — `internal/domain/backup/executor.go:87-100`
acquires and releases when `e.locks != nil` — it is simply never given one. And the helpers written
for this purpose, `RunWithLock` / `RunWithTimeout` in `internal/scheduler/lock_integration.go`, have
**zero callers anywhere, tests included**.

This is load-bearing: every downstream component that reasons "a lock file means the job is live, no
lock file means it is safe to reconcile" is wrong for the backup axis.

**(b) The lock primitives and their consumers each reimplement a different partial safety check**,
and three config keys that would tune them never reach the code.

## Confirmed defects

| # | Defect | Cause |
|---|---|---|
| #163 | Backups take no lock | `backup_factory.go:178` passes `nil`; `RunWithLock`/`RunWithTimeout` have zero callers |
| #142 | `scheduler.lock_dir` never reaches the scheduler, so the stale-lock scan never runs | `scheduler.go:56` `SetLockDir` has zero non-test callers; `schedule start` never calls it. `scheduler.go:69` uses the unconfigured `s.lockDir` with a hardcoded `defaultStaleLockThreshold` (60m, line 13) |
| #154 | `lock_dir` defaults to `/var/run/sentinel`, which non-root restores cannot create | `loader.go:20` `defaultLockDir`, applied at 84-85 → `restore.go:384` → `runtime/executor.go:143` `lock.NewManager` → `lock.go:68` `os.MkdirAll` on first acquire |
| #194 | `job_timeout_minutes` is never read | `types.go:45` set and defaulted to 180 at `loader.go:78-79`, no read site anywhere; `RunWithTimeout` (`lock_integration.go:60`) has zero callers |
| #195 | Starting the scheduler marks in-flight backups as interrupted | `monitor/init.go:101-127` `ReconcileStaleExecutions` has no age guard and no liveness check; `queries.go:296-321` `GetStaleRunningExecutions` has no age filter. Called at `schedule.go:56` |
| #196 | A restore can steal a live lock held by another host | `lock/state.go:30-47` `EvaluateLockState` ignores `jl.Hostname` entirely — it checks only PID liveness. Hostname is checked only in the separate `ScanStale` path (`lock.go:319-327`), not in `TryAcquire` |
| #197 | `repair` cannot see or safely clear several classes of lock — 5 sub-defects, all verified | see below |

### #197 sub-defects

1. `repair.go:517` `ListLockFiles()` walks the **entire** lock dir; the loop never filters by
   `jobFilter`, and the batch `ScanStale` at line 561 removes across all jobs. **`--job X` does not
   scope it.**
2. `repair.go:543-546` — `state.Removable` requires `age > staleThreshold`, so a **dead-PID but
   young** lock is `continue`d with no finding at all.
3. `repair.go:540-542` — foreign-host locks are `continue`d **silently**. The sibling
   `reconcileStaleRunning` at 468-469 *does* emit "skipped (foreign-host lock)" for the
   execution-row class, so the gap is specific to the lock-file class.
4. `monitor/migrate.go:62-63` builds its lock manager from `filepath.Dir(dbPath)`, while
   `repair.go:159` scans `cfg.Scheduler.LockDir`. **`migrate.lock` is structurally outside repair's
   scan root** — no configuration can bring it in.
5. Both `ScanStale` (`lock.go:315-318`) and `reconcileStaleLocks` (`repair.go:536-538`) treat a
   `ReadLock` parse error or empty file as `continue`: no finding, no removal.

## Correction to the issue text

**#195 is PARTIAL.** The symptom is real, but the claim that "`repair --fix` carries the same
hazard" is **false**. `repair.go:437-509` does not call `ReconcileStaleExecutions` at all — it has
its own lock-aware `reconcileStaleRunning` / `classifyStaleRunning` that checks `lm.ReadLock` +
`EvaluateLockState` + hostname and skips live and foreign-host locks.

The fix is therefore **not** "correct both paths". It is: make `schedule start` use the logic repair
already has. Changing repair would damage working code.

Note the ordering constraint this creates: while #163 stands, even the lock-aware check sees
`jl == nil` for a live backup and finalises it anyway. **#163 must land first or #195's fix is
inert.**

## Extra findings

- `internal/domain/restore/executor.go:98` hardcodes `time.Hour` as the stale threshold instead of
  reading `cfg.Scheduler.StaleLockThreshold` — that key reaches only `repair.go:960`.
- `restore run --all` reads top-level `MaxConcurrentRestores` (`restore.go:420-421`) while the
  scheduler reads `cfg.Scheduler.MaxConcurrentRestores` (`schedule.go:93`). Two different keys for
  one concept. Same shape as #141 in I12.

## Fix — strict order

1. **#163** — wire a real `lock.Manager` into `backup_factory.go`. Everything else assumes it.
2. **#142, #154** — call `SetLockDir(cfg.Scheduler.LockDir)` in `schedule start`; change the default
   to a user-writable location, or fall back when `MkdirAll` fails.
3. **#196** — make `EvaluateLockState` (or its `TryAcquire` caller) treat any lock with a foreign
   hostname as live regardless of age; thread `StaleLockThreshold` into the restore executor in
   place of the literal `time.Hour`.
4. **#195** — point `schedule start` at repair's lock-aware reconciliation.
5. **#197** — scope the file loop by `jobFilter`; emit findings for dead-PID-but-young, foreign-host
   and parse-error locks; bring `migrate.lock` into repair's scan root.
6. **#194** — wrap each scheduled job execution in `RunWithTimeout` (or `ctx.WithTimeout`) using the
   configured value, in `internal/scheduler/executor.go`.

## DECISION REQUIRED

**#154's new default.** `/var/run/sentinel` is correct for a root-run daemon and wrong for everyone
else. Options: XDG state dir, alongside `history_db_path`, or keep `/var/run/sentinel` and fall back
on `MkdirAll` failure. This changes where locks live for existing installs — a fallback is the
safest, a new default is the cleanest. Product call.

## Definition of done

- **Code:** the six fixes in order.
- **e2e** (uses `assert_blocks_when_concurrent` from I00):
  - Two `backup run` for the same job concurrently — the second blocks or is skipped with a lock
    signal, not silent interleaving.
  - Non-root `restore` with no `lock_dir` override — no "permission denied". **NEEDS_LIVE.**
  - A lock file with a foreign hostname and age > threshold — `TryAcquire` rejects it.
  - A long-running backup, scheduler restarted mid-run — the history row stays `running`, not
    `interrupted`. **NEEDS_LIVE.**
  - `repair --job A --fix` with stale locks for A and B — only A's is removed.
  - `repair` with a foreign-host lock — a **reported** finding, not silence.
  - A job whose dump hangs past `job_timeout_minutes` — killed and marked failed. **NEEDS_LIVE.**
- **Docs:** `operations/stale-lock-recovery.md`, `operations/state-repair.md`,
  `operations/scheduler-crash-recovery.md`, `operations/index.md`, `concepts/locking.md`,
  `guides/index.md`, `reference/glossary.md`.
