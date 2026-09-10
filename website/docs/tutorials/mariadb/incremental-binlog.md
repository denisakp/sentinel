---
title: Binary-log archival on MariaDB
description: Enable incremental backup on MariaDB, archive binary logs beside each artifact, and see why the archived logs cannot be replayed by a restore job.
sidebar_position: 4
---

By the end of this page you will have a MariaDB backup job that builds an incremental chain and packs
the server's binary logs into a tar archive beside each incremental artifact. You will be able to
read the chain out of the execution history and the archive out of the manifest.

You will also know precisely where incremental support stops in v1.4.0, which on MariaDB matters more
than the parts that work.

Budget about twenty-five minutes.

:::warning The restore half of this feature does not work
Binary-log **archival** is real and this page walks through it in full. Binary-log **replay** is not
reachable from a configured restore job: the restore planner rejects `restore_mode: incremental` for
every engine except PostgreSQL. Read
[Where incremental support stops](#where-incremental-support-stops-in-v140) before you plan any work
around this feature.
:::

## What you need

- The working directory and `sentinel.yaml` from [Restoring a MariaDB backup](./restore.md).
- A MariaDB container that actually writes binary logs, names them `mariadb-bin.NNNNNN`, and keeps
  them somewhere Sentinel can read. Step 1 covers all three.
- `MYSQL_PWD` still exported:

  ```bash
  export MYSQL_PWD=tutorial
  ```

## Step 1: Start a server whose binary logs Sentinel can find

Three conditions have to hold, and on MariaDB none of them is the default.

**Binary logging has to be switched on.** This is the first real divergence from MySQL on this
track. MySQL 8 writes binary logs out of the box; MariaDB does not log at all until `--log-bin` is
given.

**The file names no longer have to match a fixed prefix.** When Sentinel scans the log directory it
keeps every regular file with a numbered segment suffix, `<basename>.NNNNNN`, skipping directories
and any `.index` file, and it reads the server's own `.index` file when one is present. MariaDB
derives its log file names from `--log-basename` or the host name, so a server started with a bare
`--log-bin` typically produces `<hostname>-bin.NNNNNN`; that is collected. Until issue #190 was fixed
the scan matched only `mariadb-bin.` and `mysql-bin.`, and everything else was silently discarded.
Passing `--log-bin=mariadb-bin` explicitly is still worth doing for predictable file names, but it is
no longer what makes the scan work.

**The directory must be readable by Sentinel, on Sentinel's own filesystem.** `mysql.binlog_path` is
validated at configuration load: it is resolved to an absolute path and must exist and be a
directory. A path inside a container is not visible to a Sentinel process outside it, so the log
directory has to be bind-mounted out.

Recreate the container with all three satisfied:

```bash
docker rm -f sentinel-mariadb-tutorial

mkdir -p binlogs && chmod 0777 binlogs

docker run --name sentinel-mariadb-tutorial \
  -e MYSQL_ROOT_PASSWORD=tutorial \
  -e MYSQL_DATABASE=shop \
  -p 3306:3306 \
  -v "$PWD/binlogs:/var/lib/mysql-binlogs" \
  -d mariadb:lts \
  --log-bin=/var/lib/mysql-binlogs/mariadb-bin
```

The `chmod 0777` is there because the MariaDB server process inside the container writes as its own
user, which will not match yours. It is acceptable for a throwaway directory in a tutorial and is not
a pattern to carry into production, where the binary logs are already on a filesystem the backup
process can read.

Recreating the container discards the data from the earlier pages. Re-seed it:

```bash
mariadb -h 127.0.0.1 -P 3306 -u root shop -e "
CREATE TABLE customers (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(120) NOT NULL, email VARCHAR(180) NOT NULL);
CREATE TABLE orders (id INT AUTO_INCREMENT PRIMARY KEY, customer_id INT, total DECIMAL(10,2), FOREIGN KEY (customer_id) REFERENCES customers(id));
INSERT INTO customers (name, email) VALUES ('Ada Lovelace','ada@example.invalid'),('Grace Hopper','grace@example.invalid'),('Alan Turing','alan@example.invalid');
INSERT INTO orders (customer_id, total) VALUES (1, 42.00), (2, 17.50), (3, 99.99);"
```

Then confirm all three conditions:

```bash
mariadb -h 127.0.0.1 -P 3306 -u root -e "SHOW VARIABLES LIKE 'log_bin%';"
ls binlogs/
```

**What to look for**: `log_bin` reported as `ON`, `log_bin_basename` ending in `mariadb-bin`, and at
least one file named `mariadb-bin.000001` in `binlogs/` alongside a `mariadb-bin.index`. If
`binlogs/` is empty, everything below will fail with `no_binlog_files_found`.

## Step 2: Enable incremental backup on the job

Edit the `shop` entry under `databases` in `sentinel.yaml` and add two blocks:

```yaml
databases:
  shop:
    type: mariadb
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PWD
    database: shop
    output: shop.sql
    incremental_backup:
      enabled: true
      max_chain_depth: 3
      binlog_check: true
    mysql:
      binlog_path: ./binlogs
```

- **`enabled: true`** turns on chain planning. Without it every backup is a standalone full.
- **`max_chain_depth: 3`** caps how far a chain grows before Sentinel starts a fresh one. The default
  when omitted is 6. A low value here makes the reset observable inside one tutorial.
- **`mysql.binlog_path`** points at the directory to archive. A relative path is resolved against the
  working directory. This key is required whenever incremental backup is enabled on a MariaDB or
  MySQL job.
- **`binlog_check: true`** declares that binary logging must be on. Sentinel accepts and stores the
  flag, but see the caution below: it cannot currently fail.

:::note The block is called `mysql:` even on MariaDB
There is no `mariadb:` key. Both engines share one configuration block, one prerequisite check, and
one set of error messages, all of which say `mysql`. A message reading
`mysql.binlog_path is required for incremental backup (type=mariadb)` is Sentinel telling you the key
name and the job type in the same breath, not a misconfigured job type.
:::

:::caution `binlog_check` can never fire, and MariaDB is where that hurts
The prerequisite function that would report `log_bin_off` is called with its "binary logging is
enabled" argument hard-coded to `true`, so the only thing actually checked is `binlog_path`. On MySQL
that gap is mostly harmless because logging is on by default. On MariaDB, where it is off by default,
a server that logs nothing at all passes configuration validation and fails later, at the archival
step, with `no_binlog_files_found`. Treat Step 1's `SHOW VARIABLES` check as the real check.
:::

## Step 3: Watch the validator enforce `binlog_path`

These are worth provoking once so you recognise them later. All were captured by running them.

Remove the `mysql:` block entirely and validate:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
Error: invalid config "sentinel.yaml": backup 'shop': mysql.binlog_path is required for incremental backup (type=mariadb)
```

Point it at a directory that does not exist and validate again:

```text
Error: invalid config "sentinel.yaml": backup 'shop': mysql.binlog_path must exist and be locally mounted (type=mariadb)
```

Restore the working version, and validation passes:

```text
2026/08/05 20:58:09 WARN TLS not configured for database event=tls_not_configured database=shop
configuration is valid
```

The remaining codes the same check can produce are `binlog_path_invalid` (the path cannot be made
absolute), `binlog_path_unreadable` (it exists but cannot be stat'ed), and `binlog_path_not_directory`
(it is a file).

## Step 4: Build a chain

Take three backups, changing the data between them so each has something new to capture:

```bash
sentinel backup --config sentinel.yaml

mariadb -h 127.0.0.1 -P 3306 -u root shop -e \
  "INSERT INTO orders (customer_id, total) VALUES (1, 250.00);"
sentinel backup --config sentinel.yaml

mariadb -h 127.0.0.1 -P 3306 -u root shop -e \
  "INSERT INTO orders (customer_id, total) VALUES (2, 12.75);"
sentinel backup --config sentinel.yaml
```

Each prints `Backup complete !` as before. The interesting output is in the history:

```bash
sentinel monitor list --config sentinel.yaml
```

**What to look for**: the `CHAIN` and `DELTA` columns, empty until now, become populated.

- The rows from the earlier pages still show `-` in `CHAIN`. Backups taken before incremental was
  enabled belong to no chain and are never adopted into one.
- The first backup after enabling is `full`, at chain index `#0`. A chain always starts with a full.
- The next two are `incremental`, at `#1` and `#2` in the same `chain-<number>`, and they carry a
  `DELTA` byte count where the full shows `-`.

The chain identifier is printed as `chain-<id>#<index>`.

## Step 5: Find the binary-log archive

Look in the backup directory:

```bash
ls backups/
```

Alongside `shop.sql` and `shop.sql.manifest.json` there is now `shop.sql.binlogs.tar`. The name is
the artifact's file name with `.binlogs.tar` appended, and the archive is written into the same
directory as the artifact. The naming is identical on both engines; only the files inside differ.

```bash
tar -tf backups/shop.sql.binlogs.tar
```

**What to look for**: one entry per `mariadb-bin.NNNNNN` file, stored flat with no directory prefix,
in sorted file-name order. The `mariadb-bin.index` file is deliberately excluded.

The archive is recorded in the manifest, under `advanced_restore.incremental_lineage`:

```bash
cat backups/shop.sql.manifest.json
```

Three fields there are populated only for MariaDB and MySQL:

| Field | Meaning |
|---|---|
| `binlog_start_file` | the first log file in the archive |
| `binlog_end_file` | the last one |
| `binlog_artifacts` | the path of the tar archive itself |

:::note Archival happens on incremental runs only
The full backup at chain index `#0` gets no archive; only runs Sentinel classifies as `incremental`
trigger the packing step. And the archive is a copy of every binary log currently in the directory,
not the slice written since the previous backup: Sentinel does not track a starting position and does
not purge the source. A long-lived server will archive the same early logs into every incremental
artifact until you expire them server-side with `PURGE BINARY LOGS` or `expire_logs_days`.
:::

## Step 6: Watch the chain reset

`max_chain_depth: 3` lets a chain reach index 3, then starts a new one. Take two more backups:

```bash
mariadb -h 127.0.0.1 -P 3306 -u root shop -e \
  "INSERT INTO orders (customer_id, total) VALUES (3, 5.00);"
sentinel backup --config sentinel.yaml

mariadb -h 127.0.0.1 -P 3306 -u root shop -e \
  "INSERT INTO orders (customer_id, total) VALUES (1, 7.25);"
sentinel backup --config sentinel.yaml

sentinel monitor list --config sentinel.yaml
```

**What to look for**: the first of the two extends the existing chain to `#3`. The second starts a
new `chain-<number>` at `#0` with a `full` backup, because the previous index had reached
`max_chain_depth`. So a depth of 3 yields chains of four artifacts: one full and three incrementals.

## Where incremental support stops in v1.4.0

Three things this page deliberately does not tell you to do, because they do not work.

### The chain subcommands cannot be invoked

`backup chain-status`, `backup chain-list`, and `backup force-full` all require `--config`, but the
flag is registered only on the parent `backup` command's local flag set, so the subcommands never
receive it:

```text
$ sentinel backup chain-status --job shop --config sentinel.yaml
Error: unknown flag: --config

$ sentinel backup chain-status --job shop
Error: --config is required
```

There is no ordering of the arguments that satisfies both. Until this is fixed, read chain state from
`sentinel monitor list` and `sentinel monitor show` as Steps 4 and 6 do, and reset a chain by
lowering `max_chain_depth` rather than by forcing a full.

### An incremental restore job validates, dry-runs cleanly, and then fails

This is the sharpest edge on the whole track, because nothing warns you until the run itself.

Add a restore job in `incremental` mode to the `restores` block:

```yaml
  shop-chain:
    type: mariadb
    enabled: true
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PWD
    database: shop_chain
    schedule: "0 5 * * 0"
    restore_mode: incremental
    incremental_from_backup: backups/shop.sql
    backup_source:
      type: local
      local_path: ./backups
      backup_path: shop.sql
```

`incremental_from_backup` is required whenever `restore_mode: incremental`; omit it and validation
fails with `incremental_from_backup is required when restore_mode is incremental`.

Now validate:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
configuration is valid
```

The validator whitelists all four engines for `incremental` mode. It does not object.

Dry-run it:

```bash
sentinel restore dry-run shop-chain --config sentinel.yaml
```

You should see:

```text
Dry-run: Job "shop-chain"
  Type: mariadb
  Database: shop_chain
  Backup Source Type: local
  Backup Path: shop.sql
  Restore Mode: incremental
  Timeout: 0 seconds

NOTE: This is a dry-run. No data will be restored.
```

The dry run does not object either, because it never calls the planner.

Ask the planner directly and the picture changes:

```bash
sentinel restore validate-chain shop-chain --config sentinel.yaml
```

You should see:

```text
Error: chain validation failed: status=rejected reason=unsupported_database_type
```

And running it produces the same verdict, one layer further out:

```text
$ sentinel restore run shop-chain --config sentinel.yaml
Error: restore execution failed: restore planning rejected: unsupported_database_type
```

The rejection is unconditional. The incremental planner returns `unsupported_database_type` for any
engine that is not `postgres`, before it looks at your baseline, your manifest, or your chain. No
value of `incremental_from_backup` and no shape of chain changes it.

The failure is recorded, which is the one consolation:

```text
$ sentinel restore history shop-chain --config sentinel.yaml
RESTORE | DATABASE | MODE | PLAN | STATUS | DURATION | TIMESTAMP | REASON | FALLBACK
shop-chain | shop_chain | incremental | rejected | failed | 10ms | 2026-08-05T20:57:53Z | unsupported_database_type | none
```

`PLAN` is `rejected` and `REASON` carries the code, so a scheduled job that starts failing this way
is visible in the history rather than silent.

### So the binary logs cannot be replayed

The replay machinery exists, and on MariaDB it carries an extra hazard worth knowing about. Sentinel
picks its client tool by engine, so a MariaDB replay would pipe into `mariadb` rather than `mysql`.
But the tool producing the SQL is hard-coded to the name `mysqlbinlog` for both engines, and MariaDB
11 ships that program as `mariadb-binlog`. A MariaDB host with only the modern names would therefore
fail with `required_tool_missing: mysqlbinlog` even if the planner allowed the restore.

It does not allow it, so that failure is unreachable, and so is the replay. To recover data on
MariaDB today, restore an artifact with a plain `full` job as on the [restore page](./restore.md),
and if you need the binary logs applied on top, run `mariadb-binlog` yourself against the archive
Sentinel produced. The archive is a plain tar of standard binary-log files, so nothing about it is
Sentinel-specific.

:::note Per-engine differences
Incremental backup is configured through the same `incremental_backup` block on every engine, but
the mechanism differs: PostgreSQL uses WAL summarisation and is the only engine whose incremental
restore is planned, MariaDB and MySQL use binary logs, and MongoDB uses the oplog and requires a
replica set. See [Incremental and PITR](../../concepts/incremental-pitr.md).
:::

## What just happened

Enabling `incremental_backup` changed what Sentinel records around each dump and added one step after
it; it did not change how the dump is produced. Before each run Sentinel reads the job's recent
successful executions, finds the newest one carrying a chain ID, and decides whether to start a new
chain or extend the existing one. When the decision is "extend", it packs the binary-log directory
into a tar archive beside the artifact and records the archive in the manifest. The restore planner
reads that manifest back, and, on MariaDB, declines to act on it.

## Next

- **[Point-in-time recovery](./pitr.md)**: the third restore mode, and the two separate walls that
  stop it on MariaDB.

{/* sources: internal/config/types.go, internal/config/validator.go, internal/config/marshal.go, internal/cli/backup.go, internal/cli/backup_factory.go, internal/cli/monitor.go, internal/cli/restore.go, internal/domain/backup/incremental/prerequisites.go, internal/domain/backup/pipeline.go, internal/domain/restore/planner.go, internal/domain/restore/executor.go, internal/adapters/restore/incremental/mysqlbinlog/archive.go, internal/adapters/restore/incremental/mysqlbinlog/replay.go, internal/ports/manifest.go, infra/docker/docker-compose.yml */}
