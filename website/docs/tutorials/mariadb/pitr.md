---
title: Point-in-time recovery on MariaDB
description: "Why a MariaDB restore job cannot currently perform time-bounded recovery: two independent walls, and what to do instead."
sidebar_position: 5
---

This page is short, because the step it would walk you through cannot be completed. By the end of it
you will know exactly which two mechanisms block time-bounded recovery on MariaDB, how to recognise
each one, and what to do instead.

Budget about ten minutes.

:::warning This page does not end in a recovered database
Every command below was run against a MariaDB-typed configuration, because every one of them stops at
configuration validation or at the restore planner without touching a database. The outputs are
transcripts, with the job name adjusted where the page names a job differently from the configuration
they were captured against. The final state is a refusal, not a recovery.
:::

## What you need

- The working directory and `sentinel.yaml` from
  [Binary-log archival](./incremental-binlog.md), including at least one backup in `./backups`.
- No running server is required: nothing on this page reaches one.

## Wall 1: `restore_mode: pitr` is refused for every engine except PostgreSQL

Add a PITR restore job to the `restores` block in `sentinel.yaml`:

```yaml
  shop-pitr:
    type: mariadb
    enabled: true
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PWD
    database: shop_pitr
    schedule: "0 6 * * 0"
    restore_mode: pitr
    pitr_timestamp: "2026-08-05T17:46:30Z"
    backup_source:
      type: local
      local_path: ./backups
      backup_path: shop.sql
```

Validate it:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
Error: invalid config "sentinel.yaml": restore 'shop-pitr': restore_mode pitr is currently supported only for postgres
```

That is the whole of wall 1, and as failures go it is a good one: immediate, at validation time, with
a message that names the supported engine. The job never loads, so nothing downstream can act on it.
Remove the block before continuing.

## Wall 2: the replay selectors only apply to a mode that cannot run

MariaDB does not express time-bounded recovery through `pitr_timestamp`. It uses the same pair of
binary-log selectors as MySQL, under a `mysql:` block on the restore job. There is no `mariadb:`
block; the key name is shared, as it is on the backup side.

| Key | Type | What it would do |
|---|---|---|
| `mysql.binlog_target_time` | RFC 3339 timestamp with timezone | stop replaying binary logs at this moment (`--stop-datetime`) |
| `mysql.binlog_target_position` | `{file, pos}` | stop at this position in this log file (`--stop-position`) |

They are mutually exclusive, and both are validated properly. Set both at once:

```yaml
    mysql:
      binlog_target_time: "2026-08-05T17:46:30Z"
      binlog_target_position:
        file: mariadb-bin.000002
        pos: 4711
```

```text
Error: invalid config "sentinel.yaml": restore 'shop-drill': mysql.binlog_target_time and mysql.binlog_target_position are mutually exclusive
```

Write the timestamp without a `T` separator and timezone, as `"2026-08-05 17:46:30"`:

```text
Error: invalid config "sentinel.yaml": restore 'shop-drill': mysql.binlog_target_time must be RFC3339 with timezone: parsing time "2026-08-05 17:46:30" as "2006-01-02T15:04:05Z07:00": cannot parse " 17:46:30" as "T"
```

Setting either one on a job whose type is not `mysql` or `mariadb` is rejected too.

So the selectors are real and carefully checked. The problem is where they are consumed: the binary
log replay step runs only after an **incremental** restore has applied its base artifact, and, as the
[binary-log page](./incremental-binlog.md) shows, an incremental restore on MariaDB never gets past
the planner. The selectors are validated, carried into the request object, and then never reached.

:::danger A `binlog_target_time` on a `full` job is accepted and ignored
This is the trap worth remembering. Leave a job in its default `restore_mode: full` and add
`mysql.binlog_target_time`, and validation passes with no warning at all:

```text
2026/08/05 21:08:02 WARN TLS not configured for database event=tls_not_configured database=shop
configuration is valid
```

The job will then run to completion, restore the base artifact, and replay nothing, while its
configuration reads as though a recovery target were being honoured. There is no combination of
`restore_mode` and selector on MariaDB that both validates and replays.
:::

## Wall 3, for completeness: the manifest never advertises PITR

Even on PostgreSQL, where `restore_mode: pitr` gets past validation, the planner requires the
artifact's manifest to advertise a `pitr` capability and a recoverable window. The backup pipeline
writes the capability list as a fixed pair, `["full", "incremental"]`, for every engine and every
artifact, and never populates a window. So no artifact Sentinel produces can satisfy a PITR plan.

You saw that list on the [first page](./first-backup.md) of this track. The
[PostgreSQL PITR page](../postgres/pitr.md) shows the rejection it produces,
`missing_advanced_metadata`, on the one engine that gets far enough to see it.

## What to do instead

- **For a full restore**, use the plain `full` job from the [restore page](./restore.md). That path
  works end to end and is the one to build a recovery procedure on.
- **For time-bounded recovery today**, use MariaDB's own tooling. Sentinel's binary-log archive is a
  plain tar of standard `mariadb-bin.NNNNNN` files, so you can extract it and pipe
  `mariadb-binlog --stop-datetime=…` into the `mariadb` client yourself. Doing it by hand also sidesteps
  the tool-name mismatch described on the [binary-log page](./incremental-binlog.md), where Sentinel's
  own replay path looks for a program called `mysqlbinlog`.
- **Keep using Sentinel for what it does do here**: dumps, integrity checking, restore drills,
  retention, and the execution history that proves all of it ran.

## What just happened

You configured the two mechanisms MariaDB offers for time-bounded recovery and watched each of them
stop for a different reason: `restore_mode: pitr` at the validator, because the mode is PostgreSQL
only; and `mysql.binlog_target_time` at the planner, because the mode that would consume it is itself
PostgreSQL only. The configuration surface is honest about types and formats, and silent about
reachability. That gap is the thing to remember from this page.

For the surrounding model see [Incremental and PITR](../../concepts/incremental-pitr.md), and for
every key mentioned here, the [configuration reference](../../reference/configuration.md).

## Clean up

This is the end of the MariaDB track. Remove everything it created:

```bash
docker rm -f sentinel-mariadb-tutorial
cd .. && rm -rf sentinel-mariadb-tutorial
```

## Next

- **[Concepts](../../concepts/index.md)**: the model behind everything in this track, including how
  [scheduling](../../concepts/schedule.md) turns these one-off commands into a running system.
- **[The MySQL track](../mysql/index.md)**: the same ground on the other engine in this family. Most
  of it transfers directly; the differences are marked on both sides.

{/* sources: internal/config/restore_types.go, internal/config/validator.go, internal/config/marshal.go, internal/cli/restore.go, internal/domain/restore/planner.go, internal/domain/restore/executor.go, internal/domain/backup/pipeline.go, internal/adapters/restore/incremental/mysqlbinlog/replay.go, internal/ports/manifest.go, docs/runbooks/restore-pitr-and-incremental.md */}
