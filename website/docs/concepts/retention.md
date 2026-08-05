---
title: Retention
description: "How Sentinel decides which backups to delete: flat keep_last and keep_days rules, GFS calendar tiers, baseline protection, preview versus apply."
sidebar_position: 6
---

Retention is the policy that decides which of a job's past backups may be deleted. Sentinel evaluates
it against the local execution history rather than against a directory listing, works out a set of
deletion candidates, removes the surviving safety exclusions, and then deletes the artifacts from the
job's storage backend and their rows from the history database. Nothing is deleted unless a policy is
configured; an empty `retention:` block keeps everything forever.

## Why it exists

Backups accumulate. A nightly job with no policy produces 365 artifacts a year, and the cost of
storing them grows without ever being decided by anyone. The obvious fix, a `find -mtime +30 -delete`
next to the backup script, is worse than no policy at all: it does not know which artifact is the
baseline of a live incremental chain, it does not know which artifacts a restore would need, and it
leaves the history database describing files that no longer exist.

Retention exists so that "how long do we keep backups" is a statement in configuration rather than a
shell one-liner, and so that the deletion path is aware of the things a shell one-liner cannot see:
which runs actually succeeded, which artifact anchors a chain, and which rows in the execution
history have to disappear alongside the bytes.

## How it works

Evaluation is a pure calculation over history records, followed by an execution phase that touches
storage and then the database. The calculation itself performs no I/O and is deterministic: the same
history and the same policy always produce the same candidate set.

### The input

Sentinel reads the job's execution history and keeps only rows whose status is `success` and whose
file path is non-empty. Failed runs are never deletion candidates, because they have no artifact to
delete. The surviving records are sorted newest first, with the file path as a tiebreaker so that two
backups recorded in the same instant still order deterministically.

### The flat rules

`keep_last: N` means "keep the N most recent successful backups". Records at position `N` and beyond
in the newest-first ordering are marked with the reason `exceeded keep_last`.

`keep_days: N` means "keep successful backups newer than N days". The cutoff is computed from the
current UTC time at evaluation, and any record with a timestamp strictly before it is marked
`exceeded keep_days`.

The important nuance is how the two combine with each other. Within the flat family they are an
intersection of keeps: a backup survives the flat rules only if **both** rules keep it. Setting
`keep_last: 7` and `keep_days: 30` therefore means "at most 7, and none older than 30 days", not "7
or 30 days, whichever is more generous". A backup that is the tenth most recent is deleted even if it
is only a day old. A record can carry both reasons at once, in which case they are joined:
`exceeded keep_last, exceeded keep_days`.

Setting only one of the two leaves the other unconfigured, and an unconfigured rule abstains rather
than voting to delete.

### The grandfather-father-son tiers

:::info Added in v1.3.0
The `retention.gfs` block requires Sentinel v1.3.0 or later.
:::

The flat rules cannot express "one backup a month for seven years" without also keeping every
intermediate backup for seven years. The GFS block adds four independent calendar tiers that can.

| Key | Bucket | Meaning |
|---|---|---|
| `keep_daily` | UTC calendar day | Keep the newest backup of each of the last N days that has one |
| `keep_weekly` | ISO-8601 week, Monday to Sunday | Keep the newest backup of each of the last N weeks that has one |
| `keep_monthly` | UTC calendar month | Keep the newest backup of each of the last N months that has one |
| `keep_yearly` | UTC calendar year | Keep the newest backup of each of the last N years that has one |

For each enabled tier, Sentinel groups the successful records into calendar buckets, designates the
newest backup in each bucket as that bucket's anchor, and retains the anchors of the N most recent
**occupied** buckets. Two consequences follow from that word.

Empty periods are skipped, never backfilled. `keep_monthly: 12` over a history that contains only
five months of backups keeps five monthly anchors, not twelve, and it does not reach further back to
make up the difference. The count is relative to the backups that exist, not to the calendar.

Bucketing is always computed in UTC, matching how backup timestamps are stored. There is no
per-policy timezone setting, so a job that runs at 23:30 local time in a UTC+2 zone lands in the
following UTC day's bucket.

A backup that anchors several tiers at once, which the most recent backup usually does, is kept once.
Tiers are a set of retained paths, not a quota that each tier fills independently.

### How the two families combine

Between the flat family and the GFS family the rule inverts: they are a **union of keeps**. A backup
survives if the flat rules keep it **or** any enabled GFS tier keeps it. It becomes a deletion
candidate only when every configured family would discard it. A family with nothing configured
abstains and never blocks a deletion.

The practical guarantee is that adding a `gfs:` block can only ever add protection. It cannot make an
existing `keep_last`/`keep_days` policy delete more than it already did. A candidate that fell outside
every enabled tier carries the reason `not retained by gfs`, appended to any flat reason it also has.

### The safety exclusions

Two things are removed from the candidate set after the calculation, and they are the reason retention
is not simply an age filter.

**The last backup standing.** If the policy would delete every successful backup the job has, the
newest one is put back. A job can end up with exactly one backup, never zero.

**The active chain baseline.** `ProtectActiveBaseline` looks at the newest successful record. If it
carries a chain ID, Sentinel walks the records belonging to that same chain and finds the one that is
a `full` backup or sits at chain index 0. That artifact is the baseline, and it is removed from the
candidate list unconditionally, whatever the policy said.

:::danger Why the baseline matters
An incremental artifact is a delta, not a database. Restoring it means starting from the chain's full
baseline and applying every link in order. Deleting the baseline does not cost you one backup: it
makes **every incremental in that chain unrestorable**, silently, because the artifacts still exist
and still verify against their manifests. The failure only becomes visible at restore time, which is
the worst possible moment to discover it.
:::

Note the precise scope of that protection: it covers the baseline of the chain the newest backup
belongs to, meaning the chain currently being extended. Baselines of older, completed chains are not
protected. If a `keep_last` value cuts through the middle of a finished chain it can remove that
chain's baseline while leaving its incrementals in place, which orphans them. Size `keep_last`
against `incremental_backup.max_chain_depth` so that a whole chain ages out together, and check with
[`sentinel backup chain-status`](../reference/cli/backup.md) before narrowing a policy.

### Order of operations

Execution is deliberately ordered so that the history database is never ahead of storage.

1. Read the job's successful executions from the history database, newest first.
2. Calculate candidates from the policy, then apply the two safety exclusions.
3. If this is a preview, print the candidates and stop. Nothing else happens.
4. Delete each candidate's artifact from the job's storage backend, one at a time.
5. **Only if every artifact deletion succeeded**, delete the corresponding `backup_executions` rows in
   a single transaction.

Step 5 is conditional on step 4 in aggregate, not per artifact. If one deletion fails, the run stops
before touching history, and the rows for the artifacts that *were* successfully deleted remain
behind. The history then describes files that no longer exist. That is the deliberate trade: a
history row pointing at a missing file is recoverable, whereas a deleted row pointing at a surviving
file leaves an artifact nothing will ever clean up.

Only the artifact path recorded in history is deleted. The manifest sidecar written next to it, and
engine side artifacts such as an archived binary log tarball or an oplog archive, are not removed and
remain in storage.

Artifact deletion goes through the job's configured storage backend, and is implemented for `local`,
`s3`, `gcs`, and `azure`. A job stored on Google Drive fails with `retention delete not supported for
storage type 'gdrive'` as soon as it has a candidate to delete.

### Preview versus apply

`sentinel retention preview` and `sentinel retention apply` run exactly the same calculation. The
difference is what happens after it.

`preview` stops at step 3. It reads history, computes candidates, prints each one with the reason
that selected it, and exits without contacting storage or modifying the database. It is safe to run
against production at any time.

`apply` continues through steps 4 and 5 and is irreversible. It also accepts `--dry-run`, which makes
it behave identically to `preview`; the two are the same code path with a different default.

Both accept `--job` to scope the run to a single backup job. Without it, every job whose policy would
act is swept in turn, and a failure on one job is recorded and reported rather than aborting the rest.

Retention is also evaluated automatically after each **successful scheduled backup run**, for that
job only, in real deleting mode. This is how a policy stays enforced without anyone running the
command. Retention failures there are warnings: they do not turn a successful backup into a failed
one.

## Configuration

The `retention:` block sits on a backup job, or under `defaults:` where it applies to every job that
does not define its own.

```yaml
version: "1.0"
history_db_path: ~/.sentinel/history.db

defaults:
  retention:
    keep_last: 7
    keep_days: 30

databases:
  app-postgres:
    type: postgres
    host_env: APP_DB_HOST
    port: 5432
    username_env: APP_DB_USER
    password_env: APP_DB_PASSWORD
    database: app_production
    schedule: "0 2 * * *"
    output: app-postgres.sql
    storage:
      type: local
      local_path: ./backups
    incremental_backup:
      enabled: true
      max_chain_depth: 6
    retention:
      keep_last: 14
      gfs:
        keep_daily: 7
        keep_weekly: 4
        keep_monthly: 12
        keep_yearly: 3
```

Two points about that excerpt are easy to get wrong:

- Inheritance from `defaults.retention` replaces the **whole block**, it does not merge key by key. A
  job that declares `retention: { keep_last: 14 }` gets `keep_last: 14` and nothing else; the
  `keep_days: 30` from `defaults` does not carry over. Restate every key you want.
- Every `keep_*` value is optional and defaults to 0, which disables that rule. A `gfs:` block with no
  flat rules alongside it is valid and is processed normally, including by the automatic sweep. All
  GFS values must be `>= 0`; a negative one is rejected at configuration validation.

Restore jobs under `restores:` have their own smaller `retention:` block with `keep_last` and
`keep_days`. It prunes rows from the restore execution history only and never deletes an artifact;
it is a different mechanism that happens to share key names.

The full key list, with types and defaults, is in the
[configuration reference](../reference/configuration.md).

## Example

Start by seeing what a policy would do, which is always the right first move:

```bash
sentinel retention preview --config sentinel.yaml --job app-postgres
```

Each candidate is printed with its size and the rule that selected it:

```
retention preview for app-postgres
- ./backups/app-postgres_2026-06-11T02-00-00.sql (48210944 bytes) - exceeded keep_last
- ./backups/app-postgres_2026-05-28T02-00-00.sql (47993088 bytes) - exceeded keep_last, not retained by gfs
```

The reasons are the audit trail. `exceeded keep_last` alone means the flat rules discarded it and no
GFS block is configured; the combined form means both families discarded it independently. Anything
absent from this list is being kept by something, including the newest backup and the active chain
baseline, which are excluded before the list is printed.

When the output matches your intent, apply it:

:::danger Destructive and irreversible
`sentinel retention apply` permanently deletes backup artifacts from storage. Sentinel cannot undo
it and has no trash or grace period. Run `sentinel retention preview` with the same `--job` first,
read every line, and confirm with
[`sentinel backup verify`](../reference/cli/backup.md) that the backups you intend to keep are
intact before deleting the ones you do not.
:::

```bash
sentinel retention apply --config sentinel.yaml --job app-postgres
```

Across all jobs, the run reports per-job counts and a total:

```
app-postgres: deleted 2 backups
app-mysql: deleted 5 backups
total deleted: 7 backups
```

## Failure modes

**`retention preview` across all jobs says "deleted".** The multi-job summary uses the same wording
in preview and apply mode, so `sentinel retention preview --config sentinel.yaml` with no `--job`
prints `total deleted: 7 backups` while deleting nothing. Nothing was deleted; only the single-job
form labels its output `retention preview for <job>`. Use `--job` when you want the mode to be
unambiguous in the output.

**`retention delete not supported for storage type 'gdrive'`.** Artifact deletion is implemented for
local, S3, GCS, and Azure storage. A job whose artifacts live on Google Drive computes candidates
normally and then fails at the deletion step, leaving both the artifacts and the history rows in
place. Prune those artifacts outside Sentinel.

**Retention deleted artifacts but the history still lists them.** One artifact deletion failed, so the
history transaction was skipped for the whole job, including the artifacts that were removed. Fix the
underlying storage permission or path problem and re-run `retention apply`. On local storage the
delete is idempotent, so an already-removed file does not fail the second run, and the history rows
are cleared once every candidate deletion succeeds.

**A restore fails on a chain whose artifacts all exist.** Retention removed the baseline of a
completed chain while its incrementals survived, most often because `keep_last` is smaller than the
chain length. Verify with `sentinel restore validate-chain` before you need the chain, and keep
`keep_last` comfortably larger than `max_chain_depth + 1`.

**Setting `retention.dry_run: true` did not prevent deletions.** The `dry_run` key parses and passes
validation, and it participates in `defaults` inheritance, but the backup deletion path takes its
dry-run decision from the command invoked and the `--dry-run` flag, never from the configuration file.
A job configured with `dry_run: true` is still really deleted by `retention apply` and by the
automatic post-scheduled-backup sweep. Treat the key as having no effect and use `retention preview`
or `apply --dry-run` instead.

**Manifests and side artifacts remain after a deletion.** Retention deletes the artifact path recorded
in history and nothing else, so `<artifact>.manifest.json`, `<artifact>.binlogs.tar`, and
`<artifact>.oplog.archive` are left behind. They are small, but they accumulate; remove them out of
band if storage cost matters.

**A job is never swept.** In the all-jobs form, jobs whose policy would not act are skipped entirely,
which includes any job whose `retention:` block is absent, empty, or all zeroes. If retention seems
not to run for a job, confirm that at least one of `keep_last`, `keep_days`, or a positive GFS tier is
actually set on the job. Remember that inheritance is whole-block, so a job that declares any
`retention:` key at all stops inheriting `defaults.retention` entirely. Run
[`sentinel config validate`](../reference/cli/config.md) to confirm the file parses, then read the
job's own block.

## Related

- [Backup](./backup.md): where the artifacts and the history rows that retention reads come from.
- [Restore](./restore.md): why an incremental chain needs its baseline, and what a chain plan looks
  like.
- [Schedule](./schedule.md): the cron loop whose successful runs trigger the automatic sweep.
- [Incremental backup with WAL](../tutorials/postgres/incremental-wal.md): chains in practice, so that
  sizing `keep_last` against chain depth is concrete rather than abstract.
- [`sentinel retention` reference](../reference/cli/retention.md): every flag on `preview` and
  `apply`.
- [Configuration reference](../reference/configuration.md): every YAML key, including the full
  `retention:` and `gfs:` blocks.

<!-- sources: internal/domain/retention/policy.go, internal/domain/retention/gfs.go, internal/domain/retention/types.go, internal/cli/retention.go, internal/cli/retention_helpers.go, internal/cli/retention_cleaner.go, internal/cli/backup.go, internal/adapters/monitor/retention.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go, docs/runbooks/apply-retention.md, docs/runbooks/retention-gfs.md -->
