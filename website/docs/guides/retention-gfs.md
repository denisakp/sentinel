---
title: Configuring GFS retention tiers
description: "Set up grandfather-father-son calendar tiers so a policy can keep one backup per month for years without keeping everything."
sidebar_position: 10
---

Add calendar-tiered retention to a job, so a long compliance horizon costs you one artifact per
period instead of every artifact in it.

## When to use this

Use this when the requirement is expressed in calendar terms: one backup per month for seven years,
a weekly restore point for a quarter, a yearly audit anchor. The flat rules cannot express those.
`keep_days: 2555` would satisfy a seven-year mandate by keeping every nightly backup for seven years,
which is roughly 2,500 artifacts where you needed 84.

Do not add a `gfs:` block hoping to reduce what you currently store. Between the flat family and the
GFS family the rules are a union of keeps, so a `gfs:` block can only ever protect more. If your
storage bill is the problem, the lever is `keep_last`.

Do not use GFS for a job that is backed up irregularly. Tiers count *occupied* buckets, not calendar
periods, so a job that runs twice a year and sets `keep_monthly: 12` keeps two anchors and thinks it
is satisfied.

:::info Added in v1.3.0
The `retention.gfs` block requires Sentinel v1.3.0 or later. On an older binary the key is unknown
and the tiers are silently absent.
:::

## Before you start

- A backup job that already runs on a schedule dense enough to populate the tiers you intend to
  configure. A daily job can anchor daily, weekly, monthly, and yearly tiers; a weekly job cannot
  meaningfully anchor a daily one.
- Enough execution history for a preview to be informative. On a new job the tiers are correct but
  the output is nearly empty, which tells you nothing.
- Familiarity with how retention is previewed and applied, from
  [Applying a retention policy](./apply-retention.md). Everything there applies here unchanged,
  including the flat-rule trap and the `dry_run` key that does nothing.

## Steps

### 1. Understand what a tier keeps

For each enabled tier, Sentinel groups the job's successful backups into UTC calendar buckets,
designates the newest backup in each bucket as that bucket's anchor, and retains the anchors of the
N most recent **occupied** buckets.

| Key | Bucket | Retains |
|---|---|---|
| `keep_daily` | UTC calendar day | The newest backup of each of the last N days that has one |
| `keep_weekly` | ISO-8601 week, Monday to Sunday | The newest backup of each of the last N weeks that has one |
| `keep_monthly` | UTC calendar month | The newest backup of each of the last N months that has one |
| `keep_yearly` | UTC calendar year | The newest backup of each of the last N years that has one |

Three consequences follow, and each of them surprises somebody.

**Empty periods are skipped, never backfilled.** `keep_monthly: 12` over a history containing five
months of backups keeps five anchors, not twelve, and does not reach further back to make up the
difference. The count is relative to the backups that exist.

**Bucketing is always UTC**, matching how timestamps are stored, and there is no per-policy timezone
setting. A job running at 23:30 local time in a UTC+2 zone lands in the following UTC day's bucket,
so its "daily" anchor is the next day's.

**One backup can anchor several tiers and is kept once.** The most recent backup is usually the
daily, weekly, monthly, and yearly anchor simultaneously. Tiers are a set of retained paths, not four
independent quotas, so `keep_daily: 7` plus `keep_monthly: 12` does not mean 19 artifacts.

### 2. Add the block

`gfs:` nests inside `retention:`, on a job or under `defaults:`.

```yaml
databases:
  app-postgres:
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: sentinel
    password_env: SENTINEL_PG_PASSWORD
    database: app_production
    schedule: "0 2 * * *"
    output: app-postgres.sql
    storage:
      type: local
      local_path: ./backups
    retention:
      keep_last: 14
      gfs:
        keep_daily: 7
        keep_weekly: 4
        keep_monthly: 12
        keep_yearly: 3
```

Every `keep_*` is optional and defaults to 0, which disables that tier. A negative value is rejected
at validation:

```
retention.gfs keep_daily/keep_weekly/keep_monthly/keep_yearly must be >= 0
```

A `gfs:` block with no flat rules alongside it is valid and is processed normally, including by the
automatic post-scheduled-backup sweep.

:::caution `keep_last` still cuts, whatever the tiers say
The flat rules and the GFS tiers are a union of keeps, but `keep_last` and `keep_days` are an
intersection *with each other*. A policy carrying both `keep_last` and `keep_days` narrows itself
before GFS is consulted, which is [issue #158](https://github.com/denisakp/sentinel/issues/158). If
you want the tiers to be the whole policy, use `keep_last` alone alongside them, or omit both flat
rules.
:::

Remember that inheriting `defaults.retention` is all-or-nothing. A job that declares
`retention: { gfs: { keep_monthly: 12 } }` loses the `keep_last` and `keep_days` it was inheriting
from `defaults:`. Restate every key you want.

### 3. Shape the policy to the requirement

Compliance, seven years of monthly and yearly points:

```yaml
retention:
  keep_last: 3
  gfs:
    keep_monthly: 84
    keep_yearly: 7
```

Disaster recovery, dense recent coverage thinning into a tail:

```yaml
retention:
  gfs:
    keep_daily: 14
    keep_weekly: 8
    keep_monthly: 12
```

Cost control, a minimal long tail:

```yaml
retention:
  gfs:
    keep_daily: 3
    keep_weekly: 2
    keep_monthly: 6
```

### 4. Preview before the tiers ever delete anything

```bash
sentinel retention preview --config sentinel.yaml --job app-postgres
```

Backups that anchor no enabled tier are listed with the reason `not retained by gfs`, appended to any
flat reason they also carry:

```
retention preview for app-postgres
- ./backups/app-postgres_2026-05-28T02-00-00.sql (47993088 bytes) - exceeded keep_last, not retained by gfs
```

A bare `exceeded keep_last` with no GFS clause means no `gfs:` block reached this job at all, which
is almost always the whole-block inheritance rule biting. The combined form means both families
discarded the backup independently.

Preview output goes to standard error, so redirect with `2>&1` if you want to keep it.

## Verify

The honest test of a calendar policy is a calendar's worth of history, which you will not have on the
day you write it. Two checks are available before then.

Confirm the block parsed and reached the job, since a mistyped key or a lost inheritance is silent:

```bash
sentinel config validate --config sentinel.yaml
```

Then confirm the tiers are actually being evaluated by triggering the automatic sweep with a
scheduled run. The run prints the policy it resolved, including every tier, before it evaluates:

```
Retention: evaluating backup 'app-postgres' (keep_last=14, keep_days=0, gfs=[daily=7 weekly=4 monthly=12 yearly=3])
```

If that line shows `gfs=[daily=0 weekly=0 monthly=0 yearly=0]`, or omits the `gfs` clause entirely,
the block did not reach the job.

Once you have a few weeks of history, re-run the preview and check the kept set by hand: exactly one
surviving backup per occupied day for the last `keep_daily` days, one per occupied week, and so on.

## If it goes wrong

**The tiers keep fewer backups than the numbers suggest.** Expected when history is shorter than the
horizon, or when the job did not run in some periods. Tiers count occupied buckets, and empty periods
are never backfilled.

**A backup you expected to be a daily anchor is not.** Check the UTC boundary. A late-evening job in
a zone ahead of UTC anchors the following UTC day, so two consecutive local nights can land in
buckets that do not look consecutive.

**Adding `gfs:` deleted more, not less.** GFS never deletes more on its own. What happened is that
declaring `retention: { gfs: ... }` on the job stopped it inheriting `defaults.retention`, or
replaced a broader flat rule. Compare the resolved policy in the sweep's `Retention: evaluating` line
against what you thought the job had.

**Nothing is ever deleted for this job.** An all-jobs run skips a job whose policy would not act. A
`gfs:` block where every tier is 0 counts as not acting, exactly like an empty `retention:` block.

**The tiers behaved correctly and a restore still failed.** Only the baseline of the chain currently
being extended is protected from deletion. GFS tiers select individual anchors, not whole chains, so
a policy that keeps one backup per month can retain an incremental while discarding the full baseline
it depends on. For jobs using `incremental_backup:`, keep a `keep_last` comfortably above
`max_chain_depth + 1` alongside the tiers.

## Related

- [Retention](../concepts/retention.md): how the two families combine, the safety exclusions, and the
  order of operations.
- [Applying a retention policy](./apply-retention.md): previewing, applying, and the known defects in
  the deletion path.
- [Inspecting monitor history](./inspect-monitor-history.md): the successful executions the tiers
  bucket.
- [Incremental backup with WAL](../tutorials/postgres/incremental-wal.md): chains in practice, so
  sizing a policy against chain depth is concrete.
- [`sentinel retention` reference](../reference/cli/retention.md): every flag on `preview` and
  `apply`.
- [Configuration reference](../reference/configuration.md): the full `retention:` and `gfs:` blocks.

<!-- sources: internal/domain/retention/gfs.go, internal/domain/retention/policy.go, internal/domain/retention/types.go, internal/cli/retention_helpers.go, internal/cli/backup.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go, docs/runbooks/retention-gfs.md -->
