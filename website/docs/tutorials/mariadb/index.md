---
title: MariaDB
description: A guided track from an empty configuration to full backups, verified restores, and binary-log archival on MariaDB.
sidebar_position: 1
---

Four pages, followed in order, against a throwaway MariaDB container you create as you go. By the end
you will have taken full backups with `mariadb-dump`, restored one into a second database, and turned
on binary-log archival.

MariaDB and MySQL share most of Sentinel's code, and where they do this track says so rather than
pretending otherwise. But they are not the same track with the names swapped: the client binaries
differ, the server defaults differ, and one argument is emitted for MySQL and not for MariaDB. Every
such difference is called out where you meet it.

## How this track was written

:::caution Verified against the source, not executed end to end
Unlike the [PostgreSQL track](../postgres/index.md), whose every command and every expected output
was captured by running it against a live container, this track was written by reading the code that
implements each command.

What that means in practice:

- **Commands, flags, and YAML keys were checked against the binary and the source.** Nothing here is
  invented.
- **Outputs that Sentinel itself refuses to produce were captured for real.** Everything on this
  track that ends in a validation error or a planner rejection was run against a MariaDB-typed
  configuration: those commands touch only the configuration file, not a database. Where you see a
  `You should see:` block containing an error, that block is a transcript.
- **Outputs that require a live MariaDB server are described, not transcribed.** Where the PostgreSQL
  track shows you a captured success message, this track tells you what to look for instead. No
  terminal output on this track is invented to fill a gap.
:::

## The track

| Page | What you do |
|---|---|
| [Your first backup](./first-backup.md) | Start a container, seed it, write a configuration, take and verify a backup. |
| [Restoring a backup](./restore.md) | Restore into a second database and confirm the rows came back. |
| [Binary-log archival](./incremental-binlog.md) | Turn on incremental backup, archive binary logs, and see where the restore half stops. |
| [Point-in-time recovery](./pitr.md) | Why time-bounded recovery cannot currently be driven from a MariaDB restore job. |

## What you need

- Sentinel installed: see [Installation](../../intro/installation.md).
- Docker, for a disposable MariaDB server. The repository's
  [`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
  brings up `mariadb:lts` alongside MySQL, PostgreSQL 17, MongoDB, and an S3-compatible object store.
  Its MariaDB service publishes host port `3306`, and this track uses the same port.
- **`mariadb-dump` and `mariadb` on your `PATH`, under exactly those names.**

:::danger The binary names are not negotiable
Sentinel executes `mariadb-dump` for a MariaDB backup and `mariadb` for a MariaDB restore. It does
not fall back to `mysqldump` or `mysql`, and it does not consult the server to find out which
generation of client tools you have. MariaDB renamed these binaries in 11.0; on a host that only has
the older `mysqldump` and `mysql` names, every command on this track fails with an exec error
regardless of how the configuration is written. Install a MariaDB client package of 11.0 or later,
or create the symlinks yourself.
:::

If you have not run Sentinel at all yet, do the [quickstart](../../intro/quickstart.md) first.

## Four prerequisites worth knowing before you start

**Set `output:` on the backup job.** Without it the storage backend names the artifact itself,
Sentinel never learns the final path, and no manifest is written. `sentinel backup verify` then
reports `missing_manifest`, and restore has no hash to check before applying.

**Override `scheduler.lock_dir`.** It defaults to `/var/run/sentinel`, which a non-root user cannot
create. Backups do not take that lock, so the problem only appears at the first restore.

**Restore jobs default to disabled.** A restore job with no `enabled:` key is off, and
`sentinel restore run` will not execute it. Set `enabled: true` explicitly.

**MariaDB does not write binary logs unless you ask it to.** Unlike MySQL 8, binary logging is off by
default. The file names themselves no longer matter to Sentinel, which collects any
`<basename>.NNNNNN` segment since issue #190 was fixed. The
[binary-log page](./incremental-binlog.md) shows how to start the container.

## Where this track stops

**Incremental backup works. Incremental restore does not.** Binary logs really are archived beside
each incremental artifact and recorded in the manifest, but a restore job with
`restore_mode: incremental` is rejected by the restore planner for every engine except PostgreSQL.
The [binary-log page](./incremental-binlog.md) shows the exact rejection.

**Point-in-time recovery cannot be requested at all.** `restore_mode: pitr` is refused at
configuration-validation time for every engine except PostgreSQL, and the binary-log replay selectors
only take effect inside the incremental restore path that is itself unreachable. The
[PITR page](./pitr.md) shows both walls.

{/* sources: internal/config/validator.go, internal/domain/restore/planner.go, internal/domain/backup/pipeline.go, internal/adapters/dump/mariadb/mariadb_dump.go, internal/adapters/restore/mariadb/mariadb_restore.go, internal/adapters/restore/incremental/mysqlbinlog/archive.go, infra/docker/docker-compose.yml */}
