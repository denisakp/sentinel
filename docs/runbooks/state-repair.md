# Runbook — Repository state repair (`sentinel repair`)

- **Audience**: ops / SRE
- **Last reviewed**: 2026-08-03
- **Related**: [stale-lock-recovery](./stale-lock-recovery.md), [scheduler-crash-recovery](./scheduler-crash-recovery.md), [chain-corruption-recovery](./chain-corruption-recovery.md), [integrity-sweep](./integrity-sweep.md), [monitor-schema-migration](./monitor-schema-migration.md)

## When to use

Sentinel's state lives in three places that can silently drift apart after a
crash, a hard-killed process, or a manual artifact deletion:

1. **Monitor SQLite** — execution-history rows.
2. **Manifests** — the `<artifact>.manifest.json` sidecars.
3. **Artifacts** — the files on the storage backend.

Use `sentinel repair` when:

- a job is stuck showing `running` long after the process died,
- restore fails because a recorded artifact is missing,
- storage bills grow from artifacts with no history/manifest,
- an incremental chain is broken and you want a proactive audit.

`sentinel repair` is **not** `monitor doctor --repair` (that is schema-only,
one file). Repair is repository-wide and can delete artifacts — always run
`--dry-run` first.

## Drift classes detected

| Class | Meaning | Fixed by |
|---|---|---|
| `orphan_artifact` | storage object with no sidecar and no monitor row | `--purge-orphans` (guarded) |
| `untracked_artifact` | sidecar present but no monitor row (soft) | never auto-purged; reported only |
| `orphan_manifest` | sidecar present, referenced artifact absent | reported (no artifact to purge) |
| `artifact_missing` | monitor/manifest references an artifact that is gone | **manual** (see below) |
| `stale_running` | `running` row with no live lock for its job | `--fix` → `interrupted` |
| `stale_lock` | dead-PID lock older than the threshold | `--fix` → removed |
| `chain_broken` | incremental chain with a missing/non-contiguous link | **manual** |

## Modes (report-only by default; destructive actions opt-in)

| Invocation | Behaviour |
|---|---|
| `sentinel repair` / `--dry-run` | Report every drift class. **Mutates nothing.** |
| `sentinel repair --fix` | Apply the recoverable classes: finalize `stale_running` rows, remove `stale_lock` files, mark `chain_broken`. **Never deletes an artifact.** |
| `sentinel repair --purge-orphans` | Everything `--fix` does, **plus** delete `orphan_artifact` objects. Requires confirmation unless `--yes`. Implies `--fix`. |
| add `--dry-run` to any of the above | Forces report-only again. |

Other flags: `--job <name>` narrows to one backup job; `--format text|json`;
`--yes` skips the purge confirmation (for automation).

## Preconditions

- The monitor schema must be **current**. Repair calls `monitor.Diagnose`
  first and refuses (non-zero exit, doctor hint) against a
  `forward-incompatible` / `corrupt` / `missing` / `stale-pending` database —
  it will not reconcile against a schema it cannot trust. Fix the schema with
  [`monitor doctor`](./monitor-schema-migration.md) first.
- Read access to the storage backend(s) and the lock directory.

## Steps

### 1. Always dry-run first

```bash
sentinel repair --dry-run --config sentinel.yaml
```

```
Repository: prod-mysql (s3://backups/prod-mysql)
  stale_running     run-8f3a...   no live lock            → would mark interrupted
  stale_lock        prod-mysql.lock  dead pid=41 age=3h0s → report (use --fix to remove)
  orphan_artifact   2026-07-30.sql   1.9 GB               → report (use --purge-orphans to delete)
  artifact_missing  s3://.../2026-07-29.sql               → manual action required
  chain_broken      chain=ab12: broken_lineage_chain: rule_4_non_contiguous_chain_index → manual action required (blocks incremental restore)

5 finding(s): 1 orphan_artifact · 0 untracked · 0 orphan_manifest · 1 artifact_missing · 1 stale_running · 1 stale_lock · 1 chain_broken
Nothing was modified (report-only; use --fix / --purge-orphans to apply).
```

For tooling: `sentinel repair --dry-run --format json`.

### 2. Apply the safe, recoverable fixes

```bash
sentinel repair --fix --config sentinel.yaml
```

This finalizes stale `running` rows to `interrupted` (only when **no live lock**
is held for the job — the guard the scheduler's blind startup reconcile lacks),
removes dead-PID stale locks (host-local; foreign-host locks are skipped with a
warning), and marks broken chains. **No artifact is deleted.**

### 3. Purge orphan artifacts (deliberate, guarded)

```bash
sentinel repair --purge-orphans --config sentinel.yaml          # prompts before deleting
sentinel repair --purge-orphans --yes --config sentinel.yaml    # automation
```

Only `orphan_artifact` objects (no sidecar **and** no monitor row) are deleted.
The **active chain baseline is always protected** (via
`retention.ProtectActiveBaseline`) even under `--purge-orphans` — a live chain's
`full` / `ChainIndex==0` baseline is never removed.

## Manual-action classes

`artifact_missing` and `chain_broken` are **not** auto-fixed (repair marks and
reports them, it never fabricates lineage or re-derives a missing artifact).
Repair exits **non-zero** while any remain, so it can gate CI/cron.

- `artifact_missing`: recover the artifact from another copy, or delete the
  stale monitor row / manifest if the backup is genuinely gone.
- `chain_broken`: see [chain-corruption-recovery](./chain-corruption-recovery.md);
  force a fresh full backup (`sentinel backup force-full`) to start a new chain.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | No manual-action inconsistencies remain |
| 4 | Operational failure (config/backend/db) — `ErrRepairInternal` |
| 5 | `artifact_missing` / `chain_broken` remain — `ErrRepairInconsistent` |
| 1/2/3/4 | Schema refusal (reuses the `monitor doctor` exit-code contract) |

## Host-scoping caveat

Lock reconciliation is **host-local**. Repair never finalizes a `running` row
whose job lock is held on another host, and never removes a foreign-host lock —
it warns and skips. In multi-host deployments, run repair per host.

## References

- `internal/cli/repair.go`
- Reuses: `internal/adapters/monitor` (`GetStaleRunningExecutions`,
  `RecordInterrupted`, `Diagnose`), `internal/adapters/lock` (`Inspect`,
  `ListLockFiles`, `EvaluateLockState`, `ScanStale`),
  `internal/adapters/manifest_store` (`ReadManifest`),
  `internal/domain/manifest` (`ValidateIncrementalLineageContract`),
  `internal/domain/restore/incremental` (`ResolveOrderedChain`),
  `internal/domain/retention` (`ProtectActiveBaseline`).
- ADR 0001 — hexagonal layering (repair is a driving/composition command).
