---
title: Recovering a broken incremental chain
description: "Detect a broken incremental lineage with validate-chain and repair, then start a clean chain: the three chain subcommands cannot be invoked."
sidebar_position: 6
---

An incremental chain has a gap, a mismatched baseline, or a missing link, and lineage validation now rejects it.

:::warning Read this before you plan around a chain
Two things constrain every option on this page.

**The three chain subcommands cannot be invoked at all.** `sentinel backup chain-status`, `chain-list` and `force-full` each require `--config` but never register it, so they fail whether you pass the flag or not (issue #136). The project README documents them with `--config`; that has never worked.

**An incremental chain still cannot be restored to completion, intact or not.** On PostgreSQL the planner accepts the request, staging now resolves the whole chain correctly (issue #150, fixed), the engine restore runs, and the job then fails at the post-restore verification gate because no call site supplies a verification handler (issue #149). The target has been written to by the time that failure appears. On MySQL, MariaDB and MongoDB the planner rejects incremental mode outright. So repairing a chain does not buy you a chain restore; it buys you correct lineage bookkeeping and a clean starting point.

The honest recovery for a broken chain is therefore: confirm the break, start a new chain from a fresh full backup, and cull the dead artifacts.
:::

## Symptoms

Chain validation rejects the job:

```text
Error: chain validation failed: status=rejected reason=missing_incremental_baseline
```

```text
Error: chain validation failed: rule_4_non_contiguous_chain_index
```

Or repository reconciliation reports a `chain_broken` finding:

```text
  chain_broken  shop  chain=chain-1785951976: rule_2_intermediate_missing  → manual action required (blocks incremental restore)

1 finding(s): 0 orphan_artifact · 0 untracked · 0 orphan_manifest · 0 artifact_missing · 0 stale_running · 0 stale_lock · 1 chain_broken
```

The `rule_*` codes come from the chain resolver and each names a distinct break:

| Code | What is wrong |
|---|---|
| `rule_1_baseline_missing` | No artifacts resolved, or the first one is not at chain index 0. |
| `rule_1_baseline_manifest_missing` | The baseline artifact has no `.manifest.json` sidecar. |
| `rule_2_intermediate_missing` | A link has no backup ID or no manifest. |
| `rule_3_baseline_mismatch` | A link names a different baseline from the one at index 0. |
| `rule_4_non_contiguous_chain_index` | The indices have a gap, so a link has been deleted. |
| `rule_6_target_not_in_chain` | The requested target is not among the resolved artifacts. |
| `rule_7_hash_verification_failed` | A link no longer hashes to its manifest value. |

A retention sweep that removed a mid-chain artifact is by far the most common cause, and it surfaces as `rule_4_non_contiguous_chain_index`.

Two codes that look like corruption and are not:

- `missing_incremental_baseline` immediately after a chain reset is correct. The newest artifact is a full at index 0 and there is no chain to walk yet.
- `unsupported_database_type` means the restore job is asking for incremental mode on MySQL, MariaDB or MongoDB. The configuration validator accepts those engines and the planner does not. Nothing is broken; the mode is unavailable.

## Before you start

- The source database must still be reachable, because the recovery is a fresh full backup taken from it.
- The storage backend must be writable, and must have room for a full artifact alongside the existing chain. Do not delete anything first.
- Capture the current chain state before changing configuration. `sentinel monitor list` is the only working view of it:

```bash
sentinel monitor list --config sentinel.yaml --job shop --last 30d 2>&1 | tee chain-before.txt
```

```text
ID                                    JOB   TYPE         CHAIN               STATUS   TIMESTAMP            DURATION  DELTA  ERROR
2a4e1f06-4f8c-4fc8-a04b-72e1248643f6  shop  incremental  chain-1785951976#3  success  2026-08-05 17:46:40  61ms      4028
55c3fe32-4447-42f6-b254-5b694718f751  shop  incremental  chain-1785951976#2  success  2026-08-05 17:46:16  53ms      3989
f590c7fc-560e-477e-85a8-6fceb011d7a2  shop  full         chain-1785951976#0  success  2026-08-05 17:46:16  77ms      -
```

The `CHAIN` column is `<chain-id>#<index>`. The gap at index 1 above is the break, and it is visible here without any chain subcommand.

:::caution `monitor list` prints to stderr
`sentinel monitor list … > chain-before.txt` produces an empty file. Use `2>&1 | tee`, as above. Tracked as issue #165.
:::

## Resolution

### 1. Confirm the break with the two tools that work

Repository-wide, across every job and repository:

```bash
sentinel repair --config sentinel.yaml --dry-run
echo "exit=$?"
```

Exit `5` means at least one `chain_broken` or `artifact_missing` finding needs manual action. Exit `0` means the lineage is structurally sound. `--job <name>` narrows the sweep.

:::caution `repair --job` does not validate the name
An unknown job name reports `No drift detected.` and exits `0`, which is indistinguishable from a healthy job. Check your spelling against `sentinel monitor list` before trusting a clean result. Tracked as issue #170.
:::

For one restore job, against its configured backup path:

```bash
sentinel restore validate-chain shop-chain --config sentinel.yaml
```

```text
Incremental chain is valid for restore job "shop-chain"
  Baseline: backups/shop.sql
  Target: shop
  Depth: 2
  Artifacts: backups/shop.sql, shop
```

The job must be configured with `restore_mode: incremental`, or the command refuses with `restore job "shop-chain" is not configured for incremental mode`.

:::caution Neither check re-hashes anything
`validate-chain` and `repair` both assert hash verification as already passed and check structure only, so `rule_7_hash_verification_failed` will not fire from either. Corruption of a link's bytes is invisible to both. Run `sentinel backup verify --all --job shop --config sentinel.yaml` alongside them.

`sentinel restore dry-run` is not a third check: it prints the job's configured fields without invoking the planner, so it passes for jobs that cannot run. Tracked as issue #152.
:::

### 2. Start a new chain

The next backup run is planned as a fresh full at index 0 whenever the current chain index has reached `max_chain_depth`. That comparison uses the value in your **current** configuration, not the value the chain was created with, so lowering it forces the reset that `backup force-full` cannot deliver.

Set `max_chain_depth` to a value at or below the current index shown in the `CHAIN` column:

```yaml
databases:
  shop:
    type: postgres
    # ...
    output: shop.sql
    incremental_backup:
      enabled: true
      max_chain_depth: 1      # was 6; current index is 3
```

:::danger This run overwrites the artifact at `output:`
A job with a fixed `output:` writes to the same path every time and the write truncates, so the new full replaces the old chain's baseline file on disk. Copy the existing artifact and its `.manifest.json` sidecar aside first, or give the job a distinct `output:` for this run so the old chain stays readable while you confirm the new one.
:::

```bash
sentinel backup --config sentinel.yaml
```

Do not use `incremental_backup.enabled: false` to force a full. It does produce a plain full backup, but that run records no chain ID at all, so when you re-enable incremental the planner walks past it, finds the last chained row, and resumes the **broken** chain.

Once the new full exists, restore your intended `max_chain_depth`.

### 3. Cull the broken artifacts

Only after the new chain has a baseline and at least one increment. Preview first:

```bash
sentinel retention preview --config sentinel.yaml --job shop
```

:::danger `retention apply` deletes artifacts permanently
Deletion goes straight to the storage backend and the matching history rows are removed. There is no undo. Confirm the preview lists only artifacts from the old chain, and confirm the new chain's baseline is not among them, before running:

```bash
sentinel retention apply --config sentinel.yaml --job shop --dry-run
sentinel retention apply --config sentinel.yaml --job shop
```
:::

Retention protects the baseline of the **newest** chain automatically. It does not protect intermediate links, and it does not protect an older chain's baseline. That asymmetry is what breaks chains in the first place, and here it is what lets the old one be cleaned up.

Alternatively, leave the old artifacts alone and delete them by hand from the storage backend once you have confirmed no restore job references them.

## Verify recovery

A new chain ID at index 0, and a `full` type:

```bash
sentinel monitor list --config sentinel.yaml --job shop --last 1h 2>&1 | tee chain-after.txt
```

```text
ID                                    JOB   TYPE  CHAIN               STATUS   TIMESTAMP            DURATION  DELTA  ERROR
128554ba-d89c-4f0e-a146-b7395138e985  shop  full  chain-1785952000#0  success  2026-08-05 17:46:40  46ms      -
```

The new full is verifiable, which the old chain's links may not have been:

```bash
sentinel backup verify 128554ba-d89c-4f0e-a146-b7395138e985 --config sentinel.yaml
echo "exit=$?"
```

Exit `0` is a hash match. Exit `3` is `missing_manifest`, which means the job has no `output:` set and no manifest was written. A chain whose links have no manifests cannot be validated at all, so fix that before taking further increments (issue #151).

Repository-wide, the finding should be gone:

```bash
sentinel repair --config sentinel.yaml --dry-run
echo "exit=$?"
```

Exit `0` and `No drift detected.`

`repair --fix` will not close a `chain_broken` finding. Its recorded action is `manual action required`; the fixes it applies are stale `running` rows and stale locks only.

Once a second run has produced an increment, lineage validation should accept the new chain again:

```bash
sentinel restore validate-chain shop-chain --config sentinel.yaml
```

Do not read that success as "the chain can be restored". It cannot, per the warning at the top of this page. To recover data from any artifact in a chain, point a restore job at that artifact with `restore_mode: full`.

## Prevent recurrence

- **Size retention against chain depth.** Retention protects only the newest chain's baseline; every intermediate link is an ordinary deletion candidate. Set `keep_last` to at least `max_chain_depth + 2` so a whole chain plus its successor's baseline survives a sweep.
- **Keep chains short.** The default `max_chain_depth` is 6. A shorter chain bounds how many artifacts one missing link can invalidate, and a PostgreSQL incremental is a full `pg_dump` with lineage metadata around it, so a short chain costs you nothing in storage.
- **Set `output:` on every job**, so every link gets a manifest. A link without one is `rule_2_intermediate_missing` waiting to happen.
- **Run the structural and byte-level checks together**, on a schedule: `sentinel repair --config sentinel.yaml --dry-run` for lineage, `sentinel backup verify --all --since 30d --config sentinel.yaml` for bytes. Neither substitutes for the other.
- **Do not build a recovery plan on chain restore or on point-in-time recovery** in this release. Both are described honestly in [Incremental backup and point-in-time recovery](../concepts/incremental-pitr.md).

## Related

- [Incremental backup and point-in-time recovery](../concepts/incremental-pitr.md): how chains are planned, and the full state of the restore half.
- [Manifests and integrity](../concepts/manifest.md): the lineage block and every chain rejection rule.
- [Retention](../concepts/retention.md): why deleting an artifact breaks a chain, and what baseline protection does cover.
- [Repairing repository state drift](./state-repair.md): the other drift classes `sentinel repair` reports.
- [Triaging a failed backup](./failed-backup-triage.md): if the new full backup itself fails.
- [Verify backup integrity](../guides/verify-backup-integrity.md) and [Integrity sweep](../guides/integrity-sweep.md): the byte-level half of the check.
- [Apply retention](../guides/apply-retention.md): preview and apply in detail.
- [`sentinel repair` reference](../reference/cli/repair.md), [`sentinel restore` reference](../reference/cli/restore.md), [`sentinel backup` reference](../reference/cli/backup.md).
- [Configuration reference](../reference/configuration.md): `incremental_backup`, `max_chain_depth`, `retention`, `output`.

{/* sources: internal/domain/backup/incremental/chain.go, internal/domain/backup/incremental/planner.go, internal/domain/backup/planner.go, internal/domain/restore/planner.go, internal/domain/restore/incremental/chain_resolver.go, internal/domain/retention/policy.go, internal/adapters/restore/chain_assembler/adapter.go, internal/adapters/restore/runtime/staging.go, internal/cli/backup.go, internal/cli/restore.go, internal/cli/repair.go, internal/cli/retention_helpers.go, internal/cli/exit_codes.go, internal/cli/monitor.go, internal/utils/file.go, internal/config/types.go, docs/runbooks/chain-corruption-recovery.md */}
