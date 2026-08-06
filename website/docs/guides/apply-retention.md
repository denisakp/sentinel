---
title: Applying a retention policy
description: "Preview and apply backup retention safely, including the flat-rule trap and the dry-run key that does nothing."
sidebar_position: 9
---

Delete old backup artifacts under a configured policy, having first confirmed exactly which artifacts
that policy selects.

## When to use this

Use this when you are enforcing a policy by hand: after writing or narrowing one, when a storage
backend is filling up, or when you want to see what the automatic sweep will do before it does it.

Do not use it to explore what a policy *means*. `sentinel retention preview` tells you what will be
deleted, not why the rules combine the way they do; [Retention](../concepts/retention.md) explains
that. If you want long-horizon calendar tiers rather than a simple "keep the last N", start at
[Configuring GFS retention tiers](./retention-gfs.md) and come back here to apply it.

:::danger Deletion is permanent
`sentinel retention apply` removes backup artifacts from storage. There is no trash, no grace period,
and no undo. Sentinel will not stop you deleting the only usable restore point for a database.
Always run `sentinel retention preview` with the same `--job` first and read every line it prints.
:::

## Before you start

- A `retention:` block on the job, or under `defaults:`, with at least one of `keep_last`,
  `keep_days`, or a positive `gfs:` tier. A block containing none of those does nothing at all, and
  the job is skipped entirely by an all-jobs run.
- Delete permission on the job's storage backend, not just read. Deletion is implemented for `local`,
  `s3`, `gcs`, and `azure`; see the failure notes below for `google-drive`.
- A readable history database at `history_db_path`. Retention evaluates the recorded execution
  history, never a directory listing, so an artifact with no history row is invisible to it and will
  never be deleted.
- An understanding of how the two flat rules combine, because it is the opposite of what most people
  expect. Read step 1 before you write a policy.

## Steps

### 1. Write the policy, knowing that the flat rules intersect

```yaml
version: "1.0"
history_db_path: ./.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: ./backups
  retention:
    keep_last: 7
    keep_days: 30
```

:::caution `keep_last` and `keep_days` narrow each other
Together these mean "at most 7, **and** none older than 30 days". They do not mean "whichever keeps
more". Each rule contributes to a delete set, so a backup that either rule discards is deleted even
when the other rule would have kept it. With `keep_last: 7`, the eighth most recent backup is deleted
regardless of `keep_days`, even if it is an hour old.

This is [issue #158](https://github.com/denisakp/sentinel/issues/158). Sentinel's own README and
operator runbooks describe these two rules as a union; they are wrong. Size `keep_last` for the
retention you actually want, and treat `keep_days` as a further ceiling on age rather than as a
floor on coverage.
:::

Only the flat family and the GFS family combine as a union of keeps. Adding a `gfs:` block can only
ever protect more; it can never make an existing `keep_last` delete more than it already did.

Two inheritance rules bite here. `defaults.retention` is inherited as a **whole block**, so a job
declaring `retention: { keep_last: 14 }` gets exactly that and loses the `keep_days: 30` from
defaults. And a job whose only retention key is `dry_run: true` counts as declaring a policy, blocks
the inheritance, and then fails validation with a message that says nothing about inheritance:

```
backup 'app-postgres': keep_last, keep_days, or gfs is required when retention is enabled
```

### 2. Preview, scoped to one job

```bash
sentinel retention preview --config sentinel.yaml --job app-postgres
```

Each candidate is printed with its size and the rule that selected it:

```
retention preview for app-postgres
- ./backups/app-postgres_2026-06-11T02-00-00.sql (48210944 bytes) - exceeded keep_last
- ./backups/app-postgres_2026-05-28T02-00-00.sql (47993088 bytes) - exceeded keep_last, not retained by gfs
```

Preview reads history, computes candidates, prints them, and stops. It contacts no storage backend
and modifies no database, so it is safe against production at any time.

:::note This output goes to standard error
Retention prints its report on stderr, not stdout. `sentinel retention preview ... > report.txt`
captures an empty file. Use `2>&1 > report.txt` or `... > report.txt 2>&1` depending on which stream
you want.
:::

Anything absent from the list is being kept by something. Two exclusions are applied before printing:
the single most recent successful backup is always retained, and so is the full baseline of the chain
that the most recent backup belongs to.

Always pass `--job`. Without it the summary uses the word "deleted" in preview mode too, so
`sentinel retention preview --config sentinel.yaml` prints `total deleted: 3 backups` while deleting
nothing whatsoever. Only the single-job form labels its output `retention preview for <job>`.

### 3. Apply, scoped to the same job

Once the preview matches your intent:

```bash
sentinel retention apply --config sentinel.yaml --job app-postgres
```

The same calculation runs, then each candidate's artifact is deleted from storage one at a time, and
only if **every** artifact deletion succeeded are the matching history rows removed in a single
transaction. That ordering is deliberate: a history row pointing at a missing file is recoverable,
whereas a deleted row pointing at a surviving file leaves an artifact nothing will ever clean up
again.

`sentinel retention apply --dry-run` is the same code path as `preview`, with the header reading
`retention apply for <job>` instead.

:::danger The `dry_run` key in YAML is not a safety net
`retention.dry_run: true` parses, validates, and participates in inheritance, and then is never
consulted by the deletion path. Dry-run is decided entirely by the subcommand and the `--dry-run`
flag. A job configured with `keep_last: 7` and `dry_run: true` is really deleted by
`sentinel retention apply`, and by the automatic post-scheduled-backup sweep, which passes false
unconditionally.

This is [issue #157](https://github.com/denisakp/sentinel/issues/157). Delete the key from your
configuration so nobody trusts it, and use `sentinel retention preview` instead.
:::

### 4. Let the scheduler take over

After each **successful scheduled** backup, Sentinel evaluates that one job's policy in real deleting
mode, so a policy stays enforced without anyone running a command. A one-shot
`sentinel backup --config` never triggers it. Retention failures there are warnings and do not turn a
successful backup into a failed one, which also means a repeatedly failing sweep is easy to miss.

## Verify

Confirm the artifact count dropped and that the backend is still healthy:

```bash
sentinel storage status --config sentinel.yaml --output text
```

Confirm the history agrees with storage, since the two can diverge if a deletion failed partway:

```bash
sentinel monitor list --config sentinel.yaml --job app-postgres --last 90d
```

Every remaining row should name a file that still exists. Then re-run the preview: on a policy that
has just been applied it should report nothing further to delete.

Finally, if the job produces incremental chains, confirm you did not cut through one. This is the
failure that stays invisible until a restore. The check is named after a configured **restore** job
rather than the backup job, and takes it as a positional argument:

```bash
sentinel restore validate-chain app-postgres-restore --config sentinel.yaml
```

## If it goes wrong

**`retention delete not supported for storage type 'google-drive'`.** Deletion is implemented for
local, S3, GCS, and Azure only. A Google Drive job computes candidates normally and fails at the
deletion step, leaving both the artifacts and the history rows in place. Worse, `preview` gives no
hint: it returns before storage is ever consulted, so it lists candidates for a backend that cannot
delete them. This is [issue #169](https://github.com/denisakp/sentinel/issues/169). Prune Google
Drive artifacts outside Sentinel.

**An all-jobs run reported errors and still exited 0.** Without `--job`, per-job failures are
collected, summarised as a single `retention completed with errors` line on stderr, and then
discarded; the command returns success. A cron entry wrapping it will never alert. This is
[issue #168](https://github.com/denisakp/sentinel/issues/168). Run one job at a time in automation,
where a failure does propagate as a non-zero exit, or grep the stderr for that line.

**Artifacts are gone but history still lists them.** One deletion in the batch failed, so the history
transaction was skipped for the entire job, including the artifacts that were removed. Fix the
underlying permission or path problem and re-run `retention apply`. On local storage deletion is
idempotent, so an already-removed file does not fail the second attempt and the rows clear once every
candidate succeeds.

**Manifests and sidecars are still there.** Retention deletes the artifact path recorded in history
and nothing else, so `<artifact>.manifest.json` survives, along with engine side artifacts such as an
archived binary log tarball or an oplog archive. They are small but they accumulate, and a stranded
manifest makes the storage listing misleading. This is
[issue #159](https://github.com/denisakp/sentinel/issues/159). Remove them out of band.

**A restore fails on a chain whose artifacts all appear to be present.** Retention removed the
baseline of a *completed* chain while its incrementals survived. Only the baseline of the chain
currently being extended is protected. Keep `keep_last` comfortably larger than
`incremental_backup.max_chain_depth + 1` so a chain ages out as a unit.

**A job is never swept.** An all-jobs run skips any job whose policy would not act, which includes an
absent, empty, or all-zero `retention:` block. Confirm the job's own block, remembering that any
positive retention key on the job stops `defaults.retention` being inherited at all.

## Related

- [Retention](../concepts/retention.md): how candidates are calculated, the safety exclusions, and
  the order of operations.
- [Configuring GFS retention tiers](./retention-gfs.md): calendar tiers for long-horizon policies.
- [Checking a storage backend](./check-storage-backend.md): confirming reachability and permissions
  before deleting.
- [Inspecting monitor history](./inspect-monitor-history.md): the records retention evaluates.
- [Incremental backup with WAL](../tutorials/postgres/incremental-wal.md): why a chain baseline
  matters, made concrete.
- [`sentinel retention` reference](../reference/cli/retention.md): every flag on `preview` and
  `apply`.
- [Configuration reference](../reference/configuration.md): the full `retention:` block.

{/* sources: internal/domain/retention/policy.go, internal/domain/retention/types.go, internal/cli/retention.go, internal/cli/retention_helpers.go, internal/cli/retention_cleaner.go, internal/cli/backup.go, internal/adapters/monitor/retention.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go, docs/runbooks/apply-retention.md */}
