---
title: Point-in-time recovery on MongoDB
description: "Why restore_mode pitr is refused for MongoDB at configuration time, what the oplog window offers instead, and the second wall behind the first."
sidebar_position: 5
---

This page is short, because there is not much to do. `restore_mode: pitr` is rejected for MongoDB
before Sentinel gets anywhere near a database, and behind that rejection sits a second one that
applies to every engine. By the end you will know both walls, and what MongoDB actually gives you in
place of an arbitrary recovery timestamp.

Budget about ten minutes.

## What you need

- The working directory and `sentinel.yaml` from
  [Oplog archival and incremental chains](./oplog-incremental.md), including at least one backup in
  `./backups`.
- The `sentinel-mongo-tutorial` container from
  [Your first MongoDB backup](./first-backup.md), or any MongoDB instance you can point a URI at:

  ```bash
  docker run --name sentinel-mongo-tutorial \
    -p 27017:27017 -d mongo:noble --replSet rs0

  docker exec sentinel-mongo-tutorial mongosh --quiet --eval \
    'rs.initiate({_id: "rs0", members: [{_id: 0, host: "127.0.0.1:27017"}]})'
  ```

  The repository's
  [`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
  also brings up a MongoDB service, though it is a standalone with no published port.

- `MONGO_URI` and `MONGO_DRILL_URI` still exported:

  ```bash
  export MONGO_URI='mongodb://127.0.0.1:27017/?directConnection=true'
  export MONGO_DRILL_URI='mongodb://127.0.0.1:27018/?directConnection=true'
  ```

Strictly speaking no server is needed for this page. Nothing here reaches one.

## The first wall: validation refuses the mode

Add a PITR restore job to the `restores` block in `sentinel.yaml`:

```yaml
  catalog-pitr:
    type: mongodb
    enabled: true
    uri_env: MONGO_DRILL_URI
    schedule: "0 6 * * 0"
    restore_mode: pitr
    pitr_timestamp: "2026-08-05T12:00:00Z"
    restore_options:
      archive: true
    backup_source:
      type: local
      local_path: ./backups
      backup_path: catalog.archive
```

Validate it:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
Error: invalid config "sentinel.yaml": restore 'catalog-pitr': restore_mode pitr is currently supported only for postgres
```

The check is one line in the validator: for `restore_mode: pitr`, any engine other than `postgres`
is refused. It runs before the planner, before staging, and before any connection is opened, so
nothing else about the job is examined. The `pitr_timestamp` and `pitr_target_timeline` keys are
never parsed for a MongoDB job.

Remove the `catalog-pitr` block before continuing; nothing later in this track uses it.

## The second wall: PITR cannot be planned for any engine

Switching `type` to `postgres` gets you past validation and no further. The backup pipeline writes
the artifact's advanced-restore capability list as a fixed pair, `full` and `incremental`, on every
manifest it produces, and it never populates a recoverable window. The PITR planner requires the
capability `pitr` to be present before it will look at a timestamp, so it rejects every artifact
Sentinel produces with `missing_advanced_metadata`. This is tracked as issue 148, and it is
engine-independent.

The [PostgreSQL PITR page](../postgres/pitr.md) walks that rejection in full, with the configuration
surface, the validator's field rules, and the planner's reason codes. If you want to understand the
mechanism, read it there; nothing in it is MongoDB-specific and there is no point repeating it.

The practical summary for MongoDB is: even if the engine restriction were lifted tomorrow, no
Sentinel artifact carries the metadata a PITR restore needs.

## What MongoDB offers instead

MongoDB's time-bounded recovery does not work like PostgreSQL's. There is no write-ahead log to
replay to an arbitrary instant. There is the oplog, a capped collection of recent operations, and
your recovery granularity is bounded by how far back it still reaches.

Sentinel's expression of that is `mongodb.oplog_target_timestamp`, which belongs under
`restore_mode: incremental` rather than `pitr`:

```yaml
    restore_mode: incremental
    incremental_from_backup: backups/catalog.archive
    mongodb:
      oplog_target_timestamp: "2026-08-05T12:00:00Z"
```

That key is validated properly. It is accepted only on a `mongodb` restore job, and it must be
RFC 3339 with a timezone:

```text
Error: invalid config "sentinel.yaml": restore 'catalog-drill': mongodb.oplog_target_timestamp must be RFC3339 with timezone: parsing time "2026-08-05 12:00:00" as "2006-01-02T15:04:05Z07:00": cannot parse " 12:00:00" as "T"
```

And then nothing consumes it, because `restore_mode: incremental` on MongoDB is rejected by the
planner with `unsupported_database_type`, as the [oplog page](./oplog-incremental.md) demonstrates.
The value is parsed, validated, and carried to a branch of the executor that no configured MongoDB
job can reach.

:::note Recovery granularity is bounded by the oplog window
This is a property of MongoDB, not of Sentinel, and it survives every fix above. The oplog is
capped: once it is full, the oldest entries are discarded to make room whether or not anything has
consumed them. No tool can replay operations the server no longer has. Size the oplog for the
granularity you need, keep your backup interval well inside the window, and remember that a bulk
write can shorten the window sharply at exactly the wrong moment.
:::

## What to do today

- **Recover with a `full` restore** of the most recent artifact, as on the
  [restore page](./restore.md). This is the supported path and it works.
- **Keep the oplog archives** that incremental backup captures on local storage, and apply them by
  hand with `mongorestore --oplogReplay --archive=<path>` when you need the tail. That command is
  what Sentinel would run; rehearse it before you need it.
- **Do not build a recovery procedure on `restore_mode: pitr` or `incremental` for MongoDB.** Both
  are refused, one early and one late.
- **Use MongoDB's own point-in-time facilities** where your deployment provides them: a managed
  service's continuous backup, or filesystem snapshots coordinated with the oplog.

## What just happened

You configured the third restore mode, watched the validator refuse it on engine grounds alone, and
learned that the engine restriction is not even the binding constraint. That refusal is the system
working as designed: Sentinel would rather reject a recovery it cannot honour than half-perform one.

For the surrounding model, see
[Incremental backups and PITR](../../concepts/incremental-pitr.md); for every key mentioned here,
the [Configuration reference](../../reference/configuration.md).

## Clean up

This is the end of the MongoDB track. Remove everything it created:

```bash
docker rm -f sentinel-mongo-tutorial sentinel-mongo-drill
cd .. && rm -rf sentinel-mongodb-tutorial
```

## Next

- **[Concepts](../../concepts/index.md)**: the model behind everything in this track, including how
  [scheduling](../../concepts/schedule.md) turns these one-off commands into a running system.
- **[Tutorials](../index.md)**: the other engine tracks.
- **[PostgreSQL](../postgres/index.md)**: the track that was executed end to end against a live
  server, if you want to see the same machinery with captured output.

<!-- sources: internal/config/validator.go, internal/config/restore_types.go, internal/config/marshal.go, internal/domain/restore/planner.go, internal/domain/restore/plan_types.go, internal/domain/backup/pipeline.go, internal/ports/manifest.go, internal/cli/restore.go, docs/runbooks/restore-pitr-and-incremental.md -->
