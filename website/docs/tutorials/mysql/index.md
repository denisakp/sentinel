---
title: MySQL
description: A guided track from an empty configuration to full backups, verified restores, and binary-log archival on MySQL.
sidebar_position: 1
---

Four pages, followed in order, against a throwaway MySQL container you create as you go. By the end
you will have taken full backups, restored one into a second database, and turned on binary-log
archival so each incremental backup carries the logs written since the previous one.

## How this track was written

:::caution Verified against the source, not executed end to end
Unlike the [PostgreSQL track](../postgres/index.md), whose every command and every expected output
was captured by running it against a live container, this track was written by reading the code that
implements each command.

What that means in practice:

- **Commands, flags, and YAML keys were checked against the binary and the source.** Nothing here is
  invented.
- **Outputs that Sentinel itself refuses to produce were captured for real.** Everything on this
  track that ends in a validation error or a planner rejection was run: those commands touch only
  the configuration file, not a database, so they could be exercised honestly. Where you see a
  `You should see:` block containing an error, that block is a transcript.
- **Outputs that require a live MySQL server are described, not transcribed.** Where the PostgreSQL
  track shows you a captured success message, this track tells you what to look for instead. No
  terminal output on this track is invented to fill a gap.

Treat the mechanics as reliable and the cosmetics (exact durations, UUIDs, byte counts) as absent
rather than approximate.
:::

## The track

| Page | What you do |
|---|---|
| [Your first backup](./first-backup.md) | Start a container, seed it, write a configuration, take and verify a backup. |
| [Restoring a backup](./restore.md) | Restore into a second database and confirm the rows came back. |
| [Binary-log archival](./incremental-binlog.md) | Turn on incremental backup, archive binary logs, and see where the restore half stops. |
| [Point-in-time recovery](./pitr.md) | Why time-bounded recovery cannot currently be driven from a MySQL restore job. |

## What you need

- Sentinel installed: see [Installation](../../intro/installation.md).
- Docker, for a disposable MySQL server. The repository's
  [`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
  brings up `mysql:lts` alongside MariaDB, PostgreSQL, MongoDB, and an S3-compatible object store if
  you prefer that to a single container. Its MySQL service publishes host port `3307`, and this
  track uses the same port so the two can coexist.
- **`mysqldump` and `mysql` on your `PATH`.** Sentinel shells out to both; it does not link a MySQL
  client library. `mysqldump` produces every backup on this track and `mysql` applies every restore.

If you have not run Sentinel at all yet, do the [quickstart](../../intro/quickstart.md) first.

## Four prerequisites worth knowing before you start

**Set `output:` on the backup job.** Without it the storage backend names the artifact itself,
Sentinel never learns the final path, and no manifest is written. `sentinel backup verify` then
reports `missing_manifest`, and restore has no hash to check before applying.

**Override `scheduler.lock_dir`.** It defaults to `/var/run/sentinel`, which a non-root user cannot
create. Backups do not take that lock, so the problem only appears at the first restore.

**Restore jobs default to disabled.** A restore job with no `enabled:` key is off, and
`sentinel restore run` will not execute it. Set `enabled: true` explicitly.

**Binary-log archival needs a specific file-naming scheme.** Sentinel only collects binary logs
whose file names begin with `mysql-bin.`, which is not what a stock MySQL 8 server produces. The
[binary-log page](./incremental-binlog.md) shows how to start the container so the names match.

## Where this track stops

**Incremental backup works. Incremental restore does not.** Binary logs really are archived beside
each incremental artifact, and the archive is recorded in the manifest. But a restore job with
`restore_mode: incremental` is rejected by the restore planner for every engine except PostgreSQL,
so the archived logs cannot be replayed through a configured job. The
[binary-log page](./incremental-binlog.md) shows the exact rejection.

**Point-in-time recovery cannot be requested at all.** `restore_mode: pitr` is refused at
configuration-validation time for every engine except PostgreSQL, and MySQL's own replay selectors
(`mysql.binlog_target_time`, `mysql.binlog_target_position`) only take effect inside the incremental
restore path that is itself unreachable. The [PITR page](./pitr.md) shows both walls.

<!-- sources: internal/config/validator.go, internal/domain/restore/planner.go, internal/domain/backup/pipeline.go, internal/adapters/dump/mysql/, internal/adapters/restore/mysql/, internal/adapters/restore/incremental/mysqlbinlog/archive.go, infra/docker/docker-compose.yml -->
