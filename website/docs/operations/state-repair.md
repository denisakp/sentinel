---
title: Repairing repository state drift
description: "Reconcile Sentinel's three records of a backup: history rows, manifest sidecars, and the artifacts actually in storage."
sidebar_position: 2
---

Sentinel records a backup in three places, and a crash or a manual deletion can leave them
disagreeing. `sentinel repair` finds the disagreement, fixes what is safe to automate, and refuses to
guess about the rest.

## Symptoms

You are in the right place if any of these are true.

A history row is stuck at `running` long after the process that wrote it died:

```text
ID        JOB      TYPE  CHAIN  STATUS   TIMESTAMP            DURATION  DELTA  ERROR
run-aa11  pg-demo  full  -      running  2026-08-05 09:00:00  0ms       -
```

A restore fails because the artifact a history row names is not in storage, and `repair` reports it:

```text
artifact_missing  ./backups/2026-07-29.sql  recorded artifact absent from storage  → manual action required
```

An incremental chain no longer resolves, which blocks every incremental restore from it:

```text
chain_broken  chain=ab12: broken_lineage_chain: rule_1_baseline_manifest_missing  → manual action required (blocks incremental restore)
```

Storage keeps growing with files no history row and no manifest refers to:

```text
orphan_artifact   2026-07-30.sql  → report (use --purge-orphans to delete)
orphan_manifest   2026-07-29.sql.manifest.json  referenced artifact absent  → report (kept; no artifact to purge)
```

`sentinel repair` exits `5` and your cron or CI job fails, with the message:

```text
Error: repair: unresolved inconsistencies remain: repair: unresolved inconsistencies require manual action
```

Or `repair` refuses to start at all, because it will not reconcile against a schema it cannot trust:

```text
Error: monitor schema is "missing"; repair refuses to run.
  Fix: the file does not exist; the next backup or restore run will create it.
```

## Before you start

**Capture the report before you change anything.** The JSON form is the record of what was wrong:

```bash
sentinel repair --dry-run --format json --config sentinel.yaml > repair-before.json
```

**Know which history database you are reconciling.** `repair` reads `history_db_path` from the
configuration file you pass. Nothing warns you if that is not the file you meant.

**The monitor schema must be current.** `repair` calls the same diagnosis as
[`sentinel monitor doctor`](../guides/monitor-schema-migration.md) first and exits non-zero on any
status other than `current`: `1` for `stale-pending`, `2` for `forward-incompatible`, `3` for
`missing`, `4` for `corrupt`. Fix the schema first, then come back.

**`repair` is host-local.** It never finalises a row whose job lock names another host, and it never
removes another host's lock file. In a multi-host deployment, run it on each host.

:::note Backups take a lock, since the fix for issue #163
A `stale_lock` finding can come from a backup job, a restore job, or the history database's
`monitor.migrate` lock.

**On v1.4.0 and earlier, backups took no file lock at all**: the factory was wired with no lock
manager, and the scheduler helper meant to supply one had no callers. On those versions a
`stale_lock` finding never came from a backup, which changes what `--fix` can be reasoning about
below. Fixed by [issue #163](https://github.com/denisakp/sentinel/issues/163).
:::

## Resolution

### 1. Report first, always

```bash
sentinel repair --dry-run --config sentinel.yaml
```

```text
Repository: ./backups
  orphan_manifest   2026-07-29.sql.manifest.json  referenced artifact absent            → report (kept; no artifact to purge)
  orphan_artifact   2026-07-30.sql                                                      → report (use --purge-orphans to delete)
  artifact_missing  ./backups/2026-07-29.sql  recorded artifact absent from storage     → manual action required
  chain_broken      chain=ab12: broken_lineage_chain: rule_1_baseline_manifest_missing  → manual action required (blocks incremental restore)
  stale_running     run-aa11  no live lock                                              → would mark interrupted
  stale_lock        locks/pg-demo-restore.lock  dead pid=999999 age=3h0m0s              → report (use --fix to remove)

6 finding(s): 1 orphan_artifact · 0 untracked · 1 orphan_manifest · 1 artifact_missing · 1 stale_running · 1 stale_lock · 1 chain_broken
Nothing was modified (report-only; use --fix / --purge-orphans to apply).
```

Findings are grouped by storage backend under a `Repository:` heading. Anything with no repository,
such as a lock belonging to a job that is not in this configuration, is listed under `General:`.
`No drift detected.` means the three records agree.

Read every line before going further. `--dry-run` mutates nothing, even when combined with `--fix`
or `--purge-orphans`.

### 2. Apply the recoverable fixes

:::danger This can mark a running backup as interrupted
`--fix` finalises any `running` row that has no live lock for its job. Because backup jobs take no
lock at all ([issue #163](https://github.com/denisakp/sentinel/issues/163)), a backup that is
genuinely in flight right now looks exactly like a crashed one, and its row is rewritten to
`interrupted` while the process keeps running. The artifact itself is untouched; only the history
row is wrong afterwards.

Confirm no backup is running before you continue. The dump processes are the reliable signal, not
the history:

```bash
pgrep -af 'pg_dump|mysqldump|mariadb-dump|mongodump'
```

If any are running, wait for them, or scope the run with `--job` to a job that is idle.
:::

```bash
sentinel repair --fix --config sentinel.yaml
```

```text
  stale_running  run-aa11  no live lock                                → marked interrupted
  stale_lock     locks/pg-demo-restore.lock  dead pid=999999 age=3h0m0s  → removed

1 change(s) applied.
```

Exactly two classes are mutated: `stale_running` rows become `interrupted`, and removable lock files
are deleted. Nothing in storage is touched.

:::caution `--fix` does not mark broken chains, whatever its help text says
The flag's help reads `Apply recoverable fixes: finalize stale running rows, remove stale locks,
mark broken chains`. The third clause is not implemented: a `chain_broken` finding is reported with
`manual action required` under every flag combination, and no chain state is ever written. Tracked
as [issue #179](https://github.com/denisakp/sentinel/issues/179). Treat every broken chain as manual
work.
:::

Two more scoping rules to keep in mind:

- `--job <name>` narrows the storage and history sweep to one job, but **not** the lock scan. Lock
  files for other jobs are still evaluated, and still removed under `--fix`.
- `--job <name>` with a name that is not in the configuration matches nothing and prints
  `No drift detected.` with exit `0`. That is not a clean repository, it is a typo. Confirm the name
  is a key under `databases:` in your configuration before trusting the result
  ([issue #170](https://github.com/denisakp/sentinel/issues/170)).

### 3. Resolve `artifact_missing` by hand

A successful history row names an artifact that is no longer in storage. `repair` will not invent
one, and it exits `5` for as long as the finding remains.

Restore the file from another copy if one exists. If the backup is genuinely gone, the row and its
sidecar are all that is left, and the job has no usable recovery point at that timestamp any more.
Take a fresh backup of the job so it has one again:

```bash
sentinel backup --config sentinel.yaml
```

Then verify that what remains actually matches its manifest. `repair` never checks this, because its
detection is structural rather than hash-based:

```bash
sentinel backup verify --all --config sentinel.yaml
```

:::caution `backup force-full` cannot be invoked
The obvious next step, forcing a fresh full backup to reset chain state, is not reachable from the
CLI. `sentinel backup force-full`, `backup chain-status`, and `backup chain-list` all require
`--config`, but none of them registers the flag: passing it fails with `Error: unknown flag:
--config`, and omitting it fails with `Error: --config is required`. There is no invocation that
works. A plain `sentinel backup --config sentinel.yaml` still runs the job; it just does not force
the chain reset.
:::

### 4. Resolve `chain_broken` by hand

The detail names the rule that failed, for example `rule_1_baseline_manifest_missing`,
`rule_2_intermediate_missing`, or `rule_4_non_contiguous_chain_index`. Confirm it against the
restore-side check, which runs the same lineage contract and takes the restore job name as a
positional argument:

```bash
sentinel restore validate-chain pg-demo-restore --config sentinel.yaml
```

A broken chain cannot be repaired into a working one; the missing link is missing. The recovery is a
new chain, started from a new full backup. See
[Chain corruption recovery](./chain-corruption-recovery.md) for what to preserve first, and note the
caution above about `backup force-full`.

### 5. Purge orphan artifacts, deliberately

An `orphan_artifact` is an object with no history row **and** no manifest sidecar. That includes
files put in the repository by something other than Sentinel.

:::danger Purging deletes artifacts permanently
`--purge-orphans` calls the storage backend's delete for each orphan. On S3, GCS, Azure, or Google
Drive that is a remote deletion, with nothing staged or archived first. Re-read the `orphan_artifact`
lines from step 1, and copy anything you are unsure about out of the repository before you run this:

```bash
sentinel repair --dry-run --format json --config sentinel.yaml | grep -A2 orphan_artifact
```

Note also that `--purge-orphans` implies `--fix`. Answering `n` at the prompt stops the deletions
only; the `stale_running` and `stale_lock` fixes have already been applied by then.
:::

```bash
sentinel repair --purge-orphans --config sentinel.yaml
```

```text
Delete 1 orphan artifact(s) (10 bytes)? [y/N]
```

Answering anything other than `y` or `yes` labels every candidate `kept (not confirmed)`. Add
`--yes` only in automation whose dry-run output a human has already read.

The baseline of an active incremental chain is protected even here, and is labelled
`kept (protected active baseline)`, so a purge cannot decapitate a chain later backups depend on.

## Verify recovery

Re-run the report and compare it against the one you captured:

```bash
sentinel repair --dry-run --format json --config sentinel.yaml > repair-after.json
diff <(jq .summary repair-before.json) <(jq .summary repair-after.json)
```

Then check the exit code, which is the machine-readable answer:

```bash
sentinel repair --dry-run --config sentinel.yaml; echo "exit=$?"
```

| Exit | Meaning |
|---|---|
| 0 | No `artifact_missing` and no `chain_broken` remain. This is the goal state. |
| 1, 2, 3, 4 | The monitor schema was refused, or an operational failure stopped the sweep. |
| 5 | Manual-action findings remain. Go back to step 3 or step 4. |

Confirm the rows you finalised now read `interrupted` rather than `running`:

```bash
sentinel monitor list --last 7d --config sentinel.yaml
```

```text
ID        JOB      TYPE  CHAIN  STATUS       TIMESTAMP            DURATION  DELTA  ERROR
run-aa11  pg-demo  full  -      interrupted  2026-08-05 09:00:00  0ms       -      execution interrupted by process restart
```

That error text is fixed, and is identical whether `repair` or the scheduler's own startup
reconciliation finalised the row.

Finally, confirm the lock directory is clear:

```bash
ls -la /var/lib/sentinel/locks/
```

## Prevent recurrence

**Run the report on a schedule and alert on exit 5.** It is cheap, mutates nothing, and turns silent
drift into a ticket:

```bash
sentinel repair --dry-run --format json --config sentinel.yaml > /var/log/sentinel/repair.json
```

**Stop the scheduler with a signal, not a kill.** Most `stale_running` rows come from `SIGKILL`, an
OOM kill, or a container stopped without a grace period. See
[Recovering after a scheduler crash](./scheduler-crash-recovery.md) for the shutdown that avoids
them.

**Do not delete artifacts out from under Sentinel.** Use
[`sentinel retention`](../guides/apply-retention.md), which deletes the artifact and its history
rows together. Manual `rm` in the repository is what produces `artifact_missing`.

**Verify integrity separately.** `repair` reconciles existence, not content: it never re-hashes an
artifact. Pair it with a periodic
[integrity sweep](../guides/integrity-sweep.md).

**Run it per host.** Lock reconciliation is host-local by design, so a single central run silently
skips every other host's findings.

## Related

- [Locking and concurrency](../concepts/locking.md): which code paths take a lock, and why backup
  jobs currently do not.
- [`sentinel repair` reference](../reference/cli/repair.md): every flag, drift class, and exit code.
- [Recovering from a stale lock](./stale-lock-recovery.md): when the only finding is a lock file.
- [Recovering after a scheduler crash](./scheduler-crash-recovery.md): the event that usually
  produces this drift.
- [Chain corruption recovery](./chain-corruption-recovery.md): what to do about `chain_broken`.
- [Diagnosing and repairing the monitor schema](../guides/monitor-schema-migration.md): clearing the
  schema refusal.
- [Sweeping a repository for corruption](../guides/integrity-sweep.md): the hash-level check `repair`
  deliberately skips.
- [Inspecting execution history](../guides/inspect-monitor-history.md): reading the rows `repair`
  rewrites.
- [Incremental backup and point-in-time recovery](../concepts/incremental-pitr.md): what a chain is
  and why a break blocks restores.

{/* sources: internal/cli/repair.go, internal/cli/exit_codes.go, internal/cli/monitor.go, internal/cli/backup_factory.go, internal/adapters/lock/lock.go, internal/adapters/lock/state.go, internal/adapters/monitor/init.go, internal/adapters/monitor/recorder.go, internal/adapters/monitor/queries.go, internal/domain/restore/incremental/chain_resolver.go, internal/domain/retention/policy.go, internal/config/types.go, internal/config/loader.go, docs/runbooks/state-repair.md */}
