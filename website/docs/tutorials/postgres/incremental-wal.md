---
title: Incremental backup chains on PostgreSQL
description: Enable WAL-summarised incremental backup, watch Sentinel build and reset a chain, and validate the chain's lineage before a restore.
sidebar_position: 4
---

By the end of this page you will have a PostgreSQL backup job that builds an incremental chain, you
will be able to read that chain out of the execution history and out of the artifact's manifest, and
you will have watched Sentinel reset the chain when it hits its configured depth.

You will also know precisely where incremental support stops in v1.4.0, which matters more than the
parts that work.

Budget about twenty minutes.

:::info Requires PostgreSQL 17 or later
Incremental backup on PostgreSQL is built on WAL summarisation, introduced in PostgreSQL 17. On
PostgreSQL 16 and earlier there is nothing for Sentinel to chain against.
:::

## What you need

- The working directory and `sentinel.yaml` from [Restoring a PostgreSQL backup](./restore.md).
- The `sentinel-pg-tutorial` container from
  [Your first PostgreSQL backup](./first-backup.md), started with `-c summarize_wal=on`.
- `PGPASSWORD` still exported:

  ```bash
  export PGPASSWORD=tutorial
  ```

If you are starting a server from scratch, WAL summarisation must be on at the server level:

```bash
docker run --name sentinel-pg-tutorial \
  -e POSTGRES_PASSWORD=tutorial \
  -e POSTGRES_DB=shop \
  -p 5432:5432 -d postgres:17 \
  -c summarize_wal=on
```

The repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
pins its PostgreSQL service to 17 for exactly this reason, though it does not set `summarize_wal`
itself; add `-c summarize_wal=on` to the service command if you use it here.

## Step 1: Confirm WAL summarisation is on

```bash
psql -h 127.0.0.1 -U postgres -d shop -c "SHOW summarize_wal;"
```

You should see:

```text
 summarize_wal
---------------
 on
(1 row)
```

If this reports `off`, stop and fix the server before continuing. `summarize_wal` is not something
Sentinel can turn on for you; it is a server GUC, and on a managed provider it may be a parameter
group setting rather than a command-line flag.

## Step 2: Enable incremental backup on the job

Edit the `shop` entry under `databases` in `sentinel.yaml` and add an `incremental_backup` block:

```yaml
databases:
  shop:
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: PGPASSWORD
    database: shop
    output: shop.sql
    incremental_backup:
      enabled: true
      max_chain_depth: 3
      wal_summary_check: true
```

- **`enabled: true`** turns on chain planning. Without it every backup is a standalone full.
- **`max_chain_depth: 3`** caps how far a chain grows before Sentinel starts a fresh one. The default
  when omitted is 6. A low value here makes the reset observable inside one tutorial.
- **`wal_summary_check: true`** declares that WAL summarisation must be available. Sentinel accepts
  and stores this flag, but in v1.4.0 nothing probes the live server for it; the check function
  exists in the domain layer and is not wired into the backup path. Treat Step 1 as the real check.

Validate:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
2026/08/05 17:46:16 WARN TLS not configured for database event=tls_not_configured database=shop
configuration is valid
```

## Step 3: Build a chain

Take three backups, changing the data between them so each has something new to capture:

```bash
sentinel backup --config sentinel.yaml

psql -h 127.0.0.1 -U postgres -d shop -c \
  "INSERT INTO orders (customer_id, total) VALUES (1, 250.00);"
sentinel backup --config sentinel.yaml

psql -h 127.0.0.1 -U postgres -d shop -c \
  "INSERT INTO orders (customer_id, total) VALUES (2, 12.75);"
sentinel backup --config sentinel.yaml
```

Each prints `Backup complete !` as before. The interesting output is in the history:

```bash
sentinel monitor list --config sentinel.yaml
```

You should see:

```text
ID                                    JOB   TYPE         CHAIN               STATUS   TIMESTAMP            DURATION  DELTA  ERROR
55c3fe32-4447-42f6-b254-5b694718f751  shop  incremental  chain-1785951976#2  success  2026-08-05 17:46:16  53ms      3989
7bdc33d4-4a27-41f5-ba51-fc808437f3b8  shop  incremental  chain-1785951976#1  success  2026-08-05 17:46:16  57ms      3949
f590c7fc-560e-477e-85a8-6fceb011d7a2  shop  full         chain-1785951976#0  success  2026-08-05 17:46:16  77ms      -
20591f75-8d9c-4c7a-9af4-435013b28c0a  shop  full         -                   success  2026-08-05 17:43:57  79ms      -
```

Three things changed relative to the previous pages:

- The oldest row still shows `-` in `CHAIN`. That is the backup you took before enabling incremental
  backup; it belongs to no chain and is never adopted into one.
- The first backup after enabling is `full` at `#0`. A chain always starts with a full.
- The next two are `incremental` at `#1` and `#2`, in the same `chain-1785951976`, and they carry a
  `DELTA` size where the full shows `-`.

## Step 4: Read the lineage out of a single execution

```bash
sentinel monitor show 55c3fe32-4447-42f6-b254-5b694718f751 --config sentinel.yaml
```

Substitute your own newest execution ID. You should see:

```text
Execution Details
=================

ID: 55c3fe32-4447-42f6-b254-5b694718f751
Backup Job: shop
Database Type: postgres
Started: 2026-08-05 17:46:16
Duration: 53ms

Status: success
Backup Type: incremental
Chain ID: chain-1785951976
Chain Index: 2
File: backups/shop.sql
Size: 3989
Delta Size: 3989
```

The same lineage is written into the artifact's manifest, which is what a restore reads:

```bash
cat backups/shop.sql.manifest.json
```

You should see, under `advanced_restore`:

```json
{
  "capabilities": ["full", "incremental"],
  "incremental_lineage": {
    "enabled": true,
    "chain_id": "chain-1785951976",
    "chain_index": 2,
    "max_chain_depth": 3,
    "baseline_backup_id": "backups/shop.sql",
    "required_backup_ids": ["backups/shop.sql"],
    "delta_size_bytes": 3989,
    "engine": "postgres",
    "execution_supported": true
  }
}
```

:::note What "incremental" means here, exactly

`Size` and `Delta Size` are equal, and `baseline_backup_id` points at the same file the incremental
was written to. That is not a display bug. In v1.4.0 a PostgreSQL "incremental" backup is a full
`pg_dump` with chain metadata recorded around it; Sentinel does not invoke
`pg_basebackup --incremental`, and there is no reference to it anywhere in the codebase.

What you gain from enabling it is lineage: chain identity, ordering, depth policy, and a manifest
that a restore planner can reason about. What you do not gain, yet, is a smaller artifact. Plan your
storage accordingly.
:::

## Step 5: Watch the chain reset

`max_chain_depth: 3` lets a chain reach index 3, then starts a new one. Take two more backups:

```bash
psql -h 127.0.0.1 -U postgres -d shop -c \
  "INSERT INTO orders (customer_id, total) VALUES (3, 5.00);"
sentinel backup --config sentinel.yaml

psql -h 127.0.0.1 -U postgres -d shop -c \
  "INSERT INTO orders (customer_id, total) VALUES (1, 7.25);"
sentinel backup --config sentinel.yaml

sentinel monitor list --config sentinel.yaml
```

You should see:

```text
ID                                    JOB   TYPE         CHAIN               STATUS   TIMESTAMP            DURATION  DELTA  ERROR
128554ba-d89c-4f0e-a146-b7395138e985  shop  full         chain-1785952000#0  success  2026-08-05 17:46:40  46ms      -
2a4e1f06-4f8c-4fc8-a04b-72e1248643f6  shop  incremental  chain-1785951976#3  success  2026-08-05 17:46:40  61ms      4028
55c3fe32-4447-42f6-b254-5b694718f751  shop  incremental  chain-1785951976#2  success  2026-08-05 17:46:16  53ms      3989
7bdc33d4-4a27-41f5-ba51-fc808437f3b8  shop  incremental  chain-1785951976#1  success  2026-08-05 17:46:16  57ms      3949
f590c7fc-560e-477e-85a8-6fceb011d7a2  shop  full         chain-1785951976#0  success  2026-08-05 17:46:16  77ms      -
```

The first of the two extended `chain-1785951976` to index 3. The second started `chain-1785952000`
at index 0 with a full backup, because the previous index had reached `max_chain_depth`. So a depth
of 3 yields chains of four artifacts: one full and three incrementals.

## Step 6: Validate the chain before you rely on it

A chain is only useful if a restore can walk it. Add a restore job in `incremental` mode to the
`restores` block of `sentinel.yaml`:

```yaml
  shop-chain:
    type: postgres
    enabled: true
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: PGPASSWORD
    database: shop_drill
    schedule: "0 5 * * 0"
    restore_mode: incremental
    incremental_from_backup: backups/shop.sql
    backup_source:
      type: local
      local_path: ./backups
      backup_path: shop.sql
```

`incremental_from_backup` is required whenever `restore_mode: incremental`, and it must match the
`baseline_backup_id` in the manifest **byte for byte**: that is the value you read in Step 4.

Now validate the lineage without executing anything:

```bash
sentinel restore validate-chain shop-chain --config sentinel.yaml
```

You should see:

```text
Incremental chain is valid for restore job "shop-chain"
  Baseline: backups/shop.sql
  Target: shop
  Depth: 2
  Artifacts: backups/shop.sql, shop
```

Run this immediately after Step 5's chain reset and you get the opposite verdict:

```text
Error: chain validation failed: status=rejected reason=missing_incremental_baseline
```

That is correct behaviour, not a bug, and it is worth understanding. `validate-chain` reads the
manifest of the artifact the job points at. Right after a reset that artifact is a `full` at index 0,
which has no baseline; there is no chain to walk. Take one more backup so the newest artifact is an
incremental again, and validation passes.

## Where incremental support stops in v1.4.0

Two things this page deliberately does not tell you to do, because they do not work.

**`backup chain-status`, `backup chain-list`, and `backup force-full` cannot be invoked.** All three
require `--config`, but the flag is registered only on the parent `backup` command's local flag set,
so the subcommands never receive it:

```text
$ sentinel backup chain-status --job shop --config sentinel.yaml
Error: unknown flag: --config

$ sentinel backup chain-status --job shop
Error: --config is required
```

There is no ordering of the arguments that satisfies both. Until this is fixed, read chain state from
`sentinel monitor list` and `sentinel monitor show` as Steps 3 and 4 do, and reset a chain by
lowering `max_chain_depth` rather than by forcing a full.

**`sentinel restore run` in `incremental` mode does not complete.** Planning succeeds; the same
planning `validate-chain` exercises; but staging the chain's baseline fails, because the baseline is
recorded as a repository-relative path (`backups/shop.sql`) and then resolved again relative to the
source root:

```text
$ sentinel restore run shop-chain --config sentinel.yaml
Error: backup "shop.sql" not found in local source: restore source object not found: backups/shop.sql
```

To recover data from a chain today, restore the artifact with a plain `full` job as on the
[restore page](./restore.md). The chain metadata still earns its keep: it tells you which artifacts
belong together and in what order.

:::note Per-engine differences
Incremental backup is configured through the same `incremental_backup` block on every engine, but the
mechanism differs: PostgreSQL uses WAL summarisation, MySQL and MariaDB use binary logs (and require
`mysql.binlog_path` to point at a locally readable directory, which Sentinel *does* validate at
config load), and MongoDB uses the oplog and requires a replica set. See
[Backup](../../concepts/backup.md).
:::

## What just happened

Enabling `incremental_backup` changed what Sentinel records around each dump, not how the dump is
produced. Before each run it reads the job's recent successful executions, finds the newest one
carrying a chain ID, and decides: start a new chain (no previous chain, or the previous index has
reached `max_chain_depth`) or extend the existing one. That decision is written into both the history
row and the artifact's manifest, and the restore planner reads it back from the manifest.

## Next

- **[Point-in-time recovery](./pitr.md)**: the third restore mode, its configuration, and an honest
  account of how far it gets in v1.4.0.

{/* sources: internal/config/types.go, internal/config/validator.go, internal/cli/backup.go, internal/cli/monitor.go, internal/cli/restore.go, internal/domain/backup/planner.go, internal/domain/backup/incremental/chain.go, internal/domain/backup/incremental/prerequisites.go, internal/domain/backup/pipeline.go, internal/domain/restore/planner.go, internal/domain/restore/source.go, internal/ports/manifest.go, infra/docker/docker-compose.yml */}
