# Grandfather-Father-Son (GFS) Retention

Long-horizon, calendar-tiered retention that keeps one representative backup per
day / week / month / year — so you can satisfy multi-year compliance or DR
mandates without hoarding every backup.

## When to use it

The flat rules (`keep_last`, `keep_days`) answer "keep the last N backups" or
"keep everything newer than N days". They cannot express "one per month for a
year" without retaining hundreds of intermediate backups. GFS adds four
independent calendar tiers for exactly that.

## How it works

For each enabled tier, Sentinel groups your **successful** backups into calendar
buckets (in **UTC**) and keeps the **newest backup in each of the N most-recent
buckets that actually contain a backup**:

| Tier          | Bucket                     | `keep_*`       |
|---------------|----------------------------|----------------|
| Daily (Son)   | calendar day               | `keep_daily`   |
| Weekly        | ISO-8601 week (Mon–Sun)    | `keep_weekly`  |
| Monthly (Father) | calendar month          | `keep_monthly` |
| Yearly (Grandfather) | calendar year        | `keep_yearly`  |

Rules:

- A backup is **kept** if it is the newest backup ("anchor") of **any** enabled
  tier's retained bucket.
- A backup is a **deletion candidate** only if it anchors **zero** buckets.
- **Empty periods are skipped**, never backfilled: `keep_monthly: 12` over a
  history with only 5 months of backups keeps 5 monthly anchors, not 12.
- One backup can anchor several tiers at once (the newest backup is usually the
  daily, weekly, monthly, and yearly anchor) — it is kept once.
- The single most-recent backup and an active incremental-chain baseline are
  **never** deleted, regardless of policy.

### Combining with `keep_last` / `keep_days`

When both flat and GFS rules are set they combine as a **union of keeps**: a
backup survives if **either** the flat rules **or** any GFS tier wants to keep
it. Adding a GFS block can therefore only ever *add* protection — it never makes
retention more aggressive than your existing flat rule.

### Timezone

All bucketing is computed in **UTC**, matching how backup timestamps are stored.
There is no per-policy timezone setting.

## Configuration

Attach `gfs:` under a job's `retention:` block, or under `defaults.retention:`
to apply it to every job that doesn't override it:

```yaml
databases:
  app-postgres:
    type: postgres
    # ...
    retention:
      gfs:
        keep_daily: 7
        keep_weekly: 4
        keep_monthly: 12
        keep_yearly: 3
```

Each `keep_*` is optional (default 0 = tier disabled) and must be `>= 0`. A job
with only a `gfs:` block (no `keep_last`/`keep_days`) is valid and is processed
normally — it is not skipped.

## Example policies

### Compliance — 7 years of monthly + yearly points

```yaml
retention:
  keep_last: 3          # always keep the 3 most recent, for fast recovery
  gfs:
    keep_monthly: 84    # one per month for 7 years
    keep_yearly: 7      # one per year for 7 years (fiscal audit anchors)
```

### Disaster recovery — dense recent, thinning tail

```yaml
retention:
  gfs:
    keep_daily: 14      # two weeks of daily restore points
    keep_weekly: 8      # ~two months of weekly points
    keep_monthly: 12    # a year of monthly points
```

### Cost control — minimal long-tail footprint

```yaml
retention:
  gfs:
    keep_daily: 3
    keep_weekly: 2
    keep_monthly: 6
```

## Operating

Preview before applying (no deletions):

```bash
sentinel retention preview --config sentinel.yaml --job app-postgres
```

Each deletion candidate is printed with its reason; GFS-excluded backups show
`not retained by gfs`. Apply (dry-run first is recommended):

```bash
sentinel retention apply --config sentinel.yaml --job app-postgres --dry-run
sentinel retention apply --config sentinel.yaml --job app-postgres
```

Scheduled backups run this same retention automatically after each successful
backup, so a GFS policy is enforced continuously.

## Verifying behaviour

Because bucketing is calendar-based, use `retention preview` after seeding a few
backups across different days/weeks/months to confirm the kept set matches your
intent before enabling non-dry-run `apply`. The pure calculation is covered by
deterministic unit tests in `internal/domain/retention/gfs_test.go`.

## See also

- [Apply retention](./apply-retention.md) — the general retention runbook
- [Inspect monitor history](./inspect-monitor-history.md) — what feeds retention
