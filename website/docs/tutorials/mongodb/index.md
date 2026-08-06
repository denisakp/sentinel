---
title: MongoDB
description: "A guided track from an empty configuration to a verified MongoDB backup, a restore drill, and an honest account of where oplog recovery stops."
sidebar_position: 2
---

Four pages, followed in order, against throwaway MongoDB containers you create as you go. By the end
you will have taken a verified backup of a MongoDB database, restored it into a second server and
confirmed the documents came back, and enabled oplog archival on an incremental chain.

## How this track was written

:::warning Verified against the source, not executed end to end
Every command, flag, and configuration key on these pages was checked against the code that
implements it, and every Sentinel error message and rejection quoted here was produced by running
the real binary. But unlike the [PostgreSQL track](../postgres/index.md), this track was **not**
executed against a live MongoDB server from start to finish.

The practical consequence: where the PostgreSQL pages show a captured terminal transcript, these
pages either quote output derived from a format string in the code, or describe in prose what you
should observe. Nothing here is a fabricated transcript. If a step behaves differently for you,
that is worth reporting rather than working around.
:::

## The track

| Page | What you do |
|---|---|
| [Your first backup](./first-backup.md) | Start a container, seed it, write a configuration, take a backup, and verify its recorded hash. |
| [Restoring a backup](./restore.md) | Restore the archive into a second MongoDB server and confirm the documents came back. |
| [Oplog archival and chains](./oplog-incremental.md) | Turn on incremental backup, watch a chain form, capture the oplog, and meet the wall on the restore side. |
| [Point-in-time recovery](./pitr.md) | Why `restore_mode: pitr` is refused for MongoDB, and what the oplog window gives you instead. |

## What you need

- Sentinel installed: see [Installation](../../intro/installation.md).
- **MongoDB Database Tools** on your `PATH`: `mongodump` and `mongorestore`. Sentinel shells out to
  these; it does not embed them.
- **`mongosh`** on your `PATH`. Restores need it: Sentinel's pre-restore connectivity check runs
  `mongosh --eval "db.version()"` before it will hand anything to `mongorestore`. Backups do not
  need it, because the backup-side ping uses the Go driver instead.
- Docker, for disposable servers.

The repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up an instance of every supported engine. Note that its `mongo` service is not usable as-is
for this track: it publishes no port to the host and runs as a standalone `mongod` with no replica
set, so neither the host-side commands here nor oplog archival would work against it. The pages
below start their own containers instead.

If you have not run Sentinel at all yet, do the [quickstart](../../intro/quickstart.md) first.

## Four things to know before you start

All four produce confusing failures if you meet them cold.

**MongoDB is the only engine whose credentials travel in a connection URI.** Every other engine gets
its password through the subprocess environment. MongoDB's password sits in the userinfo segment of
`--uri=`, which is argv, which is visible in `ps` output and in your shell history. That is why
`mongo_secrets_file` exists, and why this track never puts a password on a command line. See
[Database credentials](../../guides/database-credentials.md).

:::danger A failed MongoDB backup or restore can print your password
Two error paths do not redact the URI. When `mongodump` fails and writes nothing to either standard
stream, Sentinel reports the failure by joining the whole argument vector, `--uri=` included. And
the restore connectivity check reports a failure as `cannot connect to MongoDB at URI <uri>`, with
the URI verbatim. If the URI carries a password, that password lands in your terminal, your CI log,
and your log aggregator. Tracked as issue 156.

Until it is fixed: keep the URI out of shell history and out of the configuration file by using
`uri_env` or `mongo_secrets_file`, treat any log containing a MongoDB failure as credential-bearing,
and prefer a MongoDB user whose credentials you can rotate cheaply.
:::

**Set `output:` on the backup job, and make the artifact a single file.** Without `output`, Sentinel
never learns the artifact's final name and writes no manifest, exactly as on PostgreSQL. MongoDB
adds a second trap: by default `mongodump` writes a *directory tree*, and a directory cannot be
hashed, cannot be listed by the local storage backend, and therefore cannot be staged by a restore
job. The [first backup page](./first-backup.md) shows the one-line fix.

**Override `scheduler.lock_dir`.** It defaults to `/var/run/sentinel`, which an unprivileged user
cannot create. Backups do not take that lock, so the problem first appears at the restore.

**A MongoDB backup to remote storage stages a local archive first.** `mongodump` cannot stream into
a bucket, so Sentinel writes an archive to a transient directory and uploads that. This surprises
people when a large database fills `/tmp`. It is documented in full in
[MongoDB staging directories](../../reference/mongo-staging.md).

## Where this track stops

Incremental **backup** works: chains form, and each incremental run captures the oplog into an
archive beside the artifact. Incremental **restore** does not. The restore planner accepts only
PostgreSQL, so a MongoDB job with `restore_mode: incremental` is rejected at run time with
`unsupported_database_type`, and the oplog replay code is unreachable from a configured job. The
[oplog page](./oplog-incremental.md) shows exactly where that happens and why it arrives so late.

Point-in-time recovery is refused earlier still, at configuration validation. The
[PITR page](./pitr.md) is short and explains both walls: the MongoDB-specific one and the
engine-independent one that also blocks PostgreSQL.

<!-- sources: internal/adapters/dump/mongo/mongo_dump.go, internal/adapters/dump/mongo/args_builder.go, internal/adapters/restore/mongo/mongo_restore.go, internal/adapters/db_probe/mongo.go, internal/config/types.go, internal/config/validator.go, internal/domain/restore/planner.go, internal/adapters/storage/local/backend.go, infra/docker/docker-compose.yml -->
