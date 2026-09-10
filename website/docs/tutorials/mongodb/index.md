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

:::note MongoDB failure messages redact the password, since the fix for issue 156
Both error paths now pass the URI through Sentinel's redaction before printing it. The scheme, user
and host survive, so the message still tells you what failed; only the secret is replaced.

**On v1.4.0 and earlier the password was printed verbatim** by two paths: a `mongodump` failure that
wrote nothing to either standard stream reported the whole argument vector with `--uri=` included,
and the restore connectivity check reported `cannot connect to MongoDB at URI <uri>` unredacted. On
those versions, treat any log containing a MongoDB failure as credential-bearing.

The advice below is worth following regardless of version, because it keeps the secret out of more
places than a log: supply the URI through `uri_env` or `mongo_secrets_file` so it never enters the
configuration file or your shell history, and prefer a MongoDB user whose credentials you can rotate
cheaply.
:::

**A local MongoDB artifact is a single archive file.** `mongodump --archive=` is used for local
storage as well as remote, so the artifact hashes, lists and stages like any other engine's.

On v1.4.0 and earlier the default was `--out=`, which writes a *directory tree*: it could not be
hashed, was skipped by the local backend's listing, and could not be staged by a restore
([#191](https://github.com/denisakp/sentinel/issues/191)). And `output:` was required before a
manifest was written at all ([#151](https://github.com/denisakp/sentinel/issues/151)). Both are
fixed; the [first backup page](./first-backup.md) has the detail.

**`scheduler.lock_dir` no longer defaults to a path you cannot write.** It resolves per user, and
only root gets `/var/run/sentinel` ([#154](https://github.com/denisakp/sentinel/issues/154)).
Backups take that lock now too ([#163](https://github.com/denisakp/sentinel/issues/163)), so it is
no longer a restore-only concern.

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

{/* sources: internal/adapters/dump/mongo/mongo_dump.go, internal/adapters/dump/mongo/args_builder.go, internal/adapters/restore/mongo/mongo_restore.go, internal/adapters/db_probe/mongo.go, internal/config/types.go, internal/config/validator.go, internal/domain/restore/planner.go, internal/adapters/storage/local/backend.go, infra/docker/docker-compose.yml */}
