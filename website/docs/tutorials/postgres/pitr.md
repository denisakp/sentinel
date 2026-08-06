---
title: Point-in-time recovery on PostgreSQL
description: Configure a PITR restore job, see how Sentinel validates and plans it, and understand the manifest metadata it needs before it will execute.
sidebar_position: 5
---

By the end of this page you will have a `pitr` restore job that passes configuration validation, you
will have seen exactly what Sentinel's restore planner does with it, and you will know the one piece
of metadata that stands between a validated PITR job and an executed one.

Budget about fifteen minutes.

:::warning This page does not end in a recovered database
Every step below was run against a real PostgreSQL 17 container, and the outputs are what that
container produced. The final step is a rejection, not a recovery. Read
[Where PITR stops in v1.4.0](#where-pitr-stops-in-v140) before you plan any work around this mode.
:::

## What you need

- The working directory and `sentinel.yaml` from
  [Incremental backup chains](./incremental-wal.md), including at least one backup in `./backups`.
- The `sentinel-pg-tutorial` container from
  [Your first PostgreSQL backup](./first-backup.md), or any PostgreSQL 17 instance; the repository's
  [`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
  provides one.
- `PGPASSWORD` still exported:

  ```bash
  export PGPASSWORD=tutorial
  ```

:::note PostgreSQL only
`restore_mode: pitr` is rejected at config-validation time for every other engine:
`restore_mode pitr is currently supported only for postgres`. MySQL and MariaDB express
time-bounded recovery through `mysql.binlog_target_time` instead, and MongoDB through
`mongodb.oplog_target_timestamp`, both under `restore_mode: incremental`. MongoDB's recovery
granularity is bounded by the oplog window rather than an arbitrary timestamp.
:::

## Step 1: Add a PITR restore job

Append to the `restores` block in `sentinel.yaml`:

```yaml
  shop-pitr:
    type: postgres
    enabled: true
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: PGPASSWORD
    database: shop_pitr
    schedule: "0 6 * * 0"
    restore_mode: pitr
    pitr_timestamp: "2026-08-05T17:46:30Z"
    pitr_target_timeline: latest
    backup_source:
      type: local
      local_path: ./backups
      backup_path: shop.sql
```

Set `pitr_timestamp` to a moment a minute or two in the past; somewhere between two of the backups
you took on the previous page.

Three keys are specific to this mode:

| Key | Required | What it does |
|---|---|---|
| `restore_mode: pitr` | Yes | Selects the PITR planner. PostgreSQL only. |
| `pitr_timestamp` | Yes | The recovery target. Must be RFC 3339 **with a timezone**. Parsed and converted to UTC at load time. |
| `pitr_target_timeline` | No | Forwarded to the planner as an opaque string. Not validated. |

`incremental_from_backup` and `confirm_full_fallback` belong to `restore_mode: incremental` and are
rejected here.

## Step 2: Validate the configuration

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
2026/08/05 17:47:15 WARN TLS not configured for database event=tls_not_configured database=shop
configuration is valid
```

The validator is strict about this mode, and its messages are worth provoking once so you recognise
them later. Remove `pitr_timestamp` and revalidate:

```text
Error: invalid config "sentinel.yaml": restore 'shop-pitr': pitr_timestamp is required when restore_mode is pitr
```

Write the timestamp without a `T` separator and timezone, `"2026-08-05 17:46:30"`, and revalidate:

```text
Error: invalid config "sentinel.yaml": restore 'shop-pitr': pitr_timestamp must be RFC3339 with timezone: parsing time "2026-08-05 17:46:30" as "2006-01-02T15:04:05Z07:00": cannot parse " 17:46:30" as "T"
```

Add `incremental_from_backup` alongside the PITR keys and revalidate:

```text
Error: invalid config "sentinel.yaml": restore 'shop-pitr': incremental_from_backup is only valid when restore_mode is incremental
```

Restore the working version before continuing.

## Step 3: Confirm the job's mode

```bash
sentinel restore status shop-pitr --config sentinel.yaml
```

You should see:

```text
Restore Job: shop-pitr
  Type: postgres
  Database: shop_pitr
  Schedule: 0 6 * * 0
  Status: enabled
  Restore Mode: pitr
  Verify After Restore: false
  Timeout: 0 seconds
  Keep File: false
```

## Step 4: Dry-run the job

```bash
sentinel restore dry-run shop-pitr --config sentinel.yaml
```

You should see:

```text
Dry-run: Job "shop-pitr"
  Type: postgres
  Database: shop_pitr
  Backup Source Type: local
  Backup Path: shop.sql
  Restore Mode: pitr
  Timeout: 0 seconds

NOTE: This is a dry-run. No data will be restored.
```

:::caution A dry run does not plan
This is the one place in the track where a dry run is weaker than it looks. It echoes the resolved
job configuration and stops; it does not invoke the restore planner. A PITR job that a dry run
reports cleanly can still be rejected the moment you run it; which is what Step 5 demonstrates. Use
the dry run to check connection details and source resolution, not to predict whether the restore
will proceed.
:::

## Step 5: Attempt the restore

:::danger Destructive
`restore run` writes into the target database. On this page the run is rejected before anything is
applied, but do not point it at a database you care about while experimenting. Verify your artifacts
first with `sentinel backup verify --all --config sentinel.yaml`.
:::

```bash
sentinel restore run shop-pitr --config sentinel.yaml
```

You should see:

```text
Error: restore execution failed: restore planning rejected: missing_advanced_metadata
```

That reason code is the whole story, and the next section explains it.

## Where PITR stops in v1.4.0

The PITR planner requires three things from the manifest of the artifact the job points at:

1. `advanced_restore.capabilities` must contain `"pitr"`.
2. `advanced_restore.recoverable_window_start_utc` and `recoverable_window_end_utc` must both be set.
3. The requested `pitr_timestamp` must fall inside that window.

Look back at the manifest you read in
[Your first PostgreSQL backup](./first-backup.md):

```json
"advanced_restore": {
  "capabilities": ["full", "incremental"],
  "incremental_lineage": { "engine": "postgres", "execution_supported": true }
}
```

The backup pipeline writes that capability list as a fixed pair; `full` and `incremental`. It never
emits `pitr`, and it never populates a recoverable window. So requirement 1 fails on every artifact
Sentinel produces, and the planner returns `missing_advanced_metadata` before it ever looks at your
timestamp. Changing the timestamp, the timeline, or the source makes no difference; the rejection is
a property of the artifact, not of the request.

What this means in practice:

- **The configuration surface is real and worth learning.** `restore_mode`, `pitr_timestamp`, and
  `pitr_target_timeline` are validated, parsed, normalised to UTC, and carried into a planner that
  implements window checking and timeline selection. None of it is a stub.
- **The backup side has not caught up.** PITR needs a backup that captures a recoverable WAL range
  and records it. Sentinel's PostgreSQL backup is a `pg_dump`, which has no such range, and the
  manifest reflects that honestly rather than claiming a capability it cannot back.
- **Do not build a recovery procedure on this mode yet.** For time-bounded recovery on PostgreSQL
  today, use PostgreSQL's own base backup and WAL archiving outside Sentinel, and use Sentinel for
  the logical dumps, integrity checking, and restore drills covered by the earlier pages in this
  track.

The other rejection codes the planner can return for a PITR job; `pitr_window_unavailable` when the
capability is present but the window is not, and `pitr_outside_recoverable_window` when the target
falls outside it; become reachable once the manifest carries that metadata.

## What just happened

You configured a restore job in the third and last restore mode, watched the validator enforce the
mode's field rules, and watched the planner refuse the run for a reason that came from the artifact
rather than the configuration. That refusal is the system working as designed: Sentinel would rather
reject a recovery it cannot honour than half-perform one.

For the surrounding model, see [Restore](../../concepts/restore.md); for every key mentioned here,
[Configuration reference](../../reference/configuration.md).

## Clean up

This is the end of the PostgreSQL track. Remove everything it created:

```bash
docker rm -f sentinel-pg-tutorial
cd .. && rm -rf sentinel-postgres-tutorial
```

## Next

- **[Concepts](../../concepts/index.md)**: the model behind everything in this track, including how
  [scheduling](../../concepts/schedule.md) turns these one-off commands into a running system.
- **[Tutorials](../index.md)**: the other engine tracks, as they land.

{/* sources: internal/config/restore_types.go, internal/config/validator.go, internal/config/marshal.go, internal/cli/restore.go, internal/domain/restore/planner.go, internal/domain/restore/plan_types.go, internal/domain/backup/pipeline.go, internal/ports/manifest.go, docs/runbooks/restore-pitr-and-incremental.md */}
