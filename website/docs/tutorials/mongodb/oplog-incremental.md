---
title: Oplog archival and incremental chains on MongoDB
description: "Enable chain-based incremental backup on MongoDB, capture the oplog beside each artifact, and see exactly where the restore half stops."
sidebar_position: 4
---

By the end of this page you will have a MongoDB backup job that builds an incremental chain and
captures the oplog into an archive beside each incremental artifact, and you will be able to read
that lineage out of both the execution history and the manifest.

You will also know precisely where MongoDB incremental support stops in v1.4.0, which matters more
than the parts that work: the backup half is real, and the restore half cannot be reached from a
configured job at all.

Budget about twenty-five minutes.

:::info Requires a replica set
Incremental backup on MongoDB is built on the oplog, which lives in the `local.oplog.rs` collection.
That collection exists only on a replica-set member. On a standalone `mongod` there is nothing to
capture. The container from [Your first MongoDB backup](./first-backup.md) was started with
`--replSet rs0` for this reason.
:::

## What you need

- The working directory and `sentinel.yaml` from
  [Restoring a MongoDB backup](./restore.md).
- The `sentinel-mongo-tutorial` container from
  [Your first MongoDB backup](./first-backup.md), started with `--replSet rs0` and initiated.
- `mongodump` on your `PATH`.
- `MONGO_URI` and `MONGO_DRILL_URI` still exported:

  ```bash
  export MONGO_URI='mongodb://127.0.0.1:27017/?directConnection=true'
  export MONGO_DRILL_URI='mongodb://127.0.0.1:27018/?directConnection=true'
  ```

If you are starting a server from scratch, it must be a replica-set member:

```bash
docker run --name sentinel-mongo-tutorial \
  -p 27017:27017 -d mongo:noble --replSet rs0

docker exec sentinel-mongo-tutorial mongosh --quiet --eval \
  'rs.initiate({_id: "rs0", members: [{_id: 0, host: "127.0.0.1:27017"}]})'
```

The repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
starts its `mongo` service as a standalone with no `--replSet`, so it cannot be used for this page.

## Step 1: Confirm the oplog exists

```bash
docker exec sentinel-mongo-tutorial mongosh --quiet --eval \
  'db.getSiblingDB("local").oplog.rs.countDocuments()'
```

A number, however small, means the oplog is there. An error mentioning that the collection does not
exist means the node is not a replica-set member; go back and initiate it before continuing.

While you are here, look at how much history that oplog actually holds:

```bash
docker exec sentinel-mongo-tutorial mongosh --quiet --eval \
  'db.getSiblingDB("admin").serverStatus().oplogTruncation'
```

The `oplogMinRetentionHours` value, and the span between the oldest and newest entry, are your
recovery window. This is the single most important number in MongoDB incremental backup, and the
next section explains why Sentinel does not check it for you.

## Step 2: Enable incremental backup on the job

Edit the `catalog` entry under `databases` in `sentinel.yaml` and add an `incremental_backup` block:

```yaml
databases:
  catalog:
    type: mongodb
    uri_env: MONGO_URI
    database: catalog
    output: catalog.archive
    database_options:
      archive: true
    incremental_backup:
      enabled: true
      max_chain_depth: 3
      oplog_window_warn_hours: 24
```

- **`enabled: true`** turns on chain planning. Without it every backup is a standalone full.
- **`max_chain_depth: 3`** caps how far a chain grows before Sentinel starts a fresh one. The default
  when omitted is 6. A low value here makes the reset observable inside one tutorial.
- **`oplog_window_warn_hours: 24`** looks like a guard rail. It is not one yet; see the warning
  below.

Validate:

```bash
sentinel config validate --config sentinel.yaml
```

You should see the usual TLS warning followed by `configuration is valid`.

:::warning Two MongoDB prerequisite checks that do not run

Both of these are worth knowing before you build a recovery procedure on this feature.

**The replica-set requirement is never checked.** The validator calls the MongoDB incremental
prerequisite function with its `replicaSetEnabled` argument hard-coded to `true`, so the
`oplog_unavailable_standalone` failure it can return is unreachable. Point an incremental MongoDB
job at a standalone `mongod` and the configuration validates cleanly; the failure arrives later,
when `mongodump` tries to read a `local.oplog.rs` that is not there.

**`oplog_window_warn_hours` does nothing at all.** The key is parsed, defaulted to `24` when
omitted, and rejected when negative
(`incremental_backup.oplog_window_warn_hours must be greater than or equal to 0`). The function that
would compare an observed oplog window against it exists in the domain layer and has no caller
anywhere in the binary. Sentinel will not warn you when the oplog window is shorter than your backup
interval, which is precisely the failure mode this feature has: once entries age out of the oplog,
the gap between two backups is unrecoverable and nothing in the tool says so.

Treat Step 1 as the real check, and monitor the oplog window yourself.
:::

## Step 3: Build a chain

Take three backups, changing the data between them so each has something new to capture:

```bash
sentinel backup --config sentinel.yaml

docker exec sentinel-mongo-tutorial mongosh --quiet catalog --eval \
  'db.products.insertOne({ sku: "A-1004", name: "Difference engine handle", price: 610 })'
sentinel backup --config sentinel.yaml

docker exec sentinel-mongo-tutorial mongosh --quiet catalog --eval \
  'db.products.insertOne({ sku: "A-1005", name: "Paper tape reel", price: 25 })'
sentinel backup --config sentinel.yaml
```

Each prints `Backup complete !` as before. The interesting output is in the history:

```bash
sentinel monitor list --config sentinel.yaml
```

Reading newest first, you should see four rows: the two most recent are `incremental` at chain
indexes `#2` and `#1` of the same `chain-<id>`, below them a `full` at `#0` that opened the chain,
and at the bottom the backup you took on the first page with an empty `CHAIN`. That oldest row
belongs to no chain and is never adopted into one. The incremental rows carry a `DELTA` size where
the full shows `-`.

## Step 4: See what oplog archival produced

This is the MongoDB-specific part, and it happens only from the second backup in a chain onwards.

```bash
ls backups/
```

Alongside `catalog.archive` and `catalog.archive.manifest.json` there is now a third file,
`catalog.archive.oplog.archive`. Sentinel produced it with a second, separate `mongodump`
invocation:

```text
mongodump --uri=<uri> --db=local --collection=oplog.rs --archive=<path> --quiet
```

The name is derived from the artifact's own name with `.oplog.archive` appended, and the file is
written into the same directory as the artifact.

The path is recorded in the manifest, under the incremental lineage:

```bash
cat backups/catalog.archive.manifest.json
```

Look for `oplog_artifact_path` inside `advanced_restore.incremental_lineage`, alongside the chain
identity (`chain_id`, `chain_index`, `max_chain_depth`, `baseline_backup_id`) and
`"engine": "mongodb"`.

:::note What "incremental" means here, exactly

Three things about that archive are easy to assume and wrong.

**It is not a delta.** The capture is an unfiltered dump of the whole `local.oplog.rs` collection.
There is no query narrowing it to entries since the previous backup, so each run captures whatever
the oplog currently holds, from its oldest surviving entry onwards.

**It is overwritten on every run.** The name is derived from the artifact name, and the artifact
name is fixed by `output:`, so every incremental run in this configuration writes the same
`catalog.archive.oplog.archive`. Only the most recent capture survives on disk.

**The main artifact is still a full dump.** As on PostgreSQL, a Sentinel "incremental" MongoDB
backup is a complete `mongodump` with chain metadata recorded around it. What you gain is lineage:
chain identity, ordering, depth policy, and an oplog capture beside the artifact. What you do not
gain is a smaller artifact.
:::

:::danger Remote storage destroys the oplog archive

This one costs you data rather than disk.

When a MongoDB job targets S3, GCS, Google Drive, or Azure, Sentinel redirects the dump into a
transient staging directory and the backup executor uploads from there. Oplog archival writes its
archive into the same directory as the artifact it is capturing beside, which in that case is the
staging directory. The executor uploads exactly two objects, the artifact and its manifest sidecar,
and then removes the staging directory in a deferred cleanup.

The oplog archive is neither uploaded nor retained. The manifest still records an
`oplog_artifact_path` pointing at a directory that no longer exists. Only local storage keeps the
capture. See [MongoDB staging directories](../../reference/mongo-staging.md) for the staging
mechanics.
:::

## Step 5: Watch the chain reset

`max_chain_depth: 3` lets a chain reach index 3, then starts a new one. Take two more backups:

```bash
docker exec sentinel-mongo-tutorial mongosh --quiet catalog --eval \
  'db.products.insertOne({ sku: "A-1006", name: "Card sorter brush", price: 44 })'
sentinel backup --config sentinel.yaml

docker exec sentinel-mongo-tutorial mongosh --quiet catalog --eval \
  'db.products.insertOne({ sku: "A-1007", name: "Ferrite core plane", price: 1300 })'
sentinel backup --config sentinel.yaml

sentinel monitor list --config sentinel.yaml
```

The first of the two extends the existing chain to index 3. The second starts a new chain at index 0
with a `full` backup, because the previous index had reached `max_chain_depth`. So a depth of 3
yields chains of four artifacts: one full and three incrementals.

:::note The chain commands cannot be invoked

`sentinel backup chain-status`, `backup chain-list`, and `backup force-full` all require `--config`,
but the flag is registered only on the parent `backup` command's local flag set, so the subcommands
never receive it:

```text
$ sentinel backup chain-status --job catalog --config sentinel.yaml
Error: unknown flag: --config

$ sentinel backup chain-status --job catalog
Error: --config is required
```

There is no ordering of the arguments that satisfies both. Until this is fixed, read chain state
from `sentinel monitor list` and `sentinel monitor show <id>` as Steps 3 and 4 do, and reset a chain
by lowering `max_chain_depth` rather than by forcing a full.
:::

## Where incremental restore stops on MongoDB

Everything above is the backup side, and it works. The restore side does not, and the way it fails
is worse than a flat refusal because the refusal arrives so late.

Add an incremental restore job to the `restores` block of `sentinel.yaml`:

```yaml
  catalog-chain:
    type: mongodb
    enabled: true
    uri_env: MONGO_DRILL_URI
    schedule: "0 5 * * 0"
    restore_mode: incremental
    incremental_from_backup: backups/catalog.archive
    restore_options:
      archive: true
    backup_source:
      type: local
      local_path: ./backups
      backup_path: catalog.archive
```

`incremental_from_backup` is required whenever `restore_mode: incremental`; omit it and validation
fails with `incremental_from_backup is required when restore_mode is incremental`.

Now watch how far this gets.

**Configuration validation passes.**

```bash
sentinel config validate --config sentinel.yaml
```

```text
configuration is valid
```

The validator whitelists all four engines for `restore_mode: incremental`, MongoDB included. Nothing
here warns you.

**The dry run passes too, and reports the mode you asked for.**

```bash
sentinel restore dry-run catalog-chain --config sentinel.yaml
```

```text
Dry-run: Job "catalog-chain"
  Type: mongodb
  Database: 
  Backup Source Type: local
  Backup Path: catalog.archive
  Restore Mode: incremental
  Timeout: 0 seconds

NOTE: This is a dry-run. No data will be restored.
```

A dry run never invokes the planner, so it cannot see the problem.

**The run fails.**

```bash
sentinel restore run catalog-chain --config sentinel.yaml
```

```text
Error: restore execution failed: restore planning rejected: unsupported_database_type
```

By the time you see that, Sentinel has already taken the job lock and staged the artifact and its
manifest into `./staging`; planning happens after staging, not before. The failed attempt is
recorded:

```bash
sentinel restore history catalog-chain --config sentinel.yaml
```

```text
RESTORE | DATABASE | MODE | PLAN | STATUS | DURATION | TIMESTAMP | REASON | FALLBACK
catalog-chain |  | incremental | rejected | failed | 5ms | 2026-08-05T21:00:38Z | unsupported_database_type | none
```

The chain validator gives the same verdict without executing anything, and is the quickest way to
confirm it:

```bash
sentinel restore validate-chain catalog-chain --config sentinel.yaml
```

```text
Error: chain validation failed: status=rejected reason=unsupported_database_type
```

### Why

The restore planner's incremental branch accepts one engine. Its first test is whether the target
engine is `postgres`; everything else returns `unsupported_database_type` before the manifest, the
lineage, or the baseline is examined. Changing the baseline, the timestamp, or the source makes no
difference: the rejection is a property of the engine, not of the request.

That is what makes the MongoDB oplog replay code unreachable from a configured job. The executor
replays oplog archives only when the planned mode is `incremental` and the engine is `mongodb`, and
the planner can never produce that combination. `mongodb.oplog_target_timestamp` is validated as
RFC 3339 with a timezone, and is then carried to a code path nothing reaches. Even if the planner
accepted MongoDB, an incremental restore additionally demands a post-restore verification handler
that the restore runtime does not supply, and would fail with
`verification handler is required for restore mode "incremental"`.

### What to do instead

- **Recover with a plain `full` job**, as on the [restore page](./restore.md). The most recent
  artifact in a chain is a complete dump, so a full restore of it loses nothing except the changes
  since it was taken.
- **Apply an oplog archive by hand** when you need the tail. The archive Sentinel captured is an
  ordinary `mongodump` archive of `local.oplog.rs`, and `mongorestore --oplogReplay --archive=<path>`
  is the same command Sentinel would run. Rehearse it before you need it.
- **Keep the chain metadata for what it is good at**: telling you which artifacts belong together
  and in what order.

## The oplog window is a real constraint regardless

None of the above changes the engine-level fact, so it is worth stating on its own.

MongoDB's oplog is a capped collection. Once it is full, the oldest entries are discarded to make
room, whether or not anything has consumed them. Your recovery granularity is bounded by how far
back that collection still reaches, which is a function of write volume and the collection's size,
not of anything Sentinel does.

Two consequences for planning:

- **Your backup interval must be shorter than your oplog window**, with margin. If the window is six
  hours and you back up daily, the eighteen hours in between are gone the moment they age out.
- **The window shrinks under load**, exactly when you can least afford it. A bulk import can burn
  through a day of oplog in minutes.

Size the oplog for the recovery granularity you need, and watch it. Sentinel's
`oplog_window_warn_hours` is not doing this for you today.

## What just happened

Enabling `incremental_backup` changed what Sentinel records around each dump, not how the dump is
produced. Before each run it reads the job's recent successful executions, finds the newest one
carrying a chain ID, and decides: start a new chain, or extend the existing one. On MongoDB it
additionally shells out to `mongodump` a second time to capture the oplog beside the artifact, and
records that path in the manifest. The restore planner then reads that manifest back and declines
to act on it.

See [Incremental backups and PITR](../../concepts/incremental-pitr.md) for the model, and the
[PostgreSQL chain page](../postgres/incremental-wal.md) for how the same configuration behaves on
the one engine the planner accepts.

## Next

- **[Point-in-time recovery](./pitr.md)**: the third restore mode, refused for MongoDB at
  configuration time, and refused for every engine one layer deeper.

{/* sources: internal/config/types.go, internal/config/validator.go, internal/config/marshal.go, internal/cli/backup.go, internal/cli/backup_factory.go, internal/cli/monitor.go, internal/cli/restore.go, internal/domain/backup/planner.go, internal/domain/backup/pipeline.go, internal/domain/backup/incremental/prerequisites.go, internal/domain/backup/executor.go, internal/domain/restore/planner.go, internal/domain/restore/executor.go, internal/adapters/dump/mongo/oplog.go, internal/adapters/restore/mongo/oplog_replay.go, internal/adapters/restore/runtime/executor.go, internal/ports/manifest.go, infra/docker/docker-compose.yml */}
