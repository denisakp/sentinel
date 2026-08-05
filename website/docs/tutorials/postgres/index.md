---
title: PostgreSQL
description: A guided track from an empty configuration to full backups, verified restores, and incremental chains on PostgreSQL.
sidebar_position: 1
---

Four pages, followed in order, against a throwaway PostgreSQL 17 container you create as you go. By
the end you will have taken full backups, restored one and confirmed the data, and built an
incremental chain on top of the write-ahead log.

Every command and every expected output in this track was captured by running it. Where something
does not work in the current release, the page says so and shows the actual error rather than
skipping the step.

## The track

| Page | What you do |
|---|---|
| [Your first backup](./first-backup.md) | Start a container, seed it, write a configuration, take and verify a backup. |
| [Restoring a backup](./restore.md) | Restore into a second database and confirm the rows came back. |
| [Incremental chains](./incremental-wal.md) | Turn on WAL-based incremental backup, build a chain, and validate it. |
| [Point-in-time recovery](./pitr.md) | What PITR is meant to do, and why it cannot currently be planned. |

## What you need

- Sentinel installed: see [Installation](../../intro/installation.md).
- Docker, for a disposable PostgreSQL 17. The repository's
  [`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
  brings up every supported engine if you prefer that to a single container.
- `psql` and `pg_dump` on your `PATH`.

If you have not run Sentinel at all yet, do the [quickstart](../../intro/quickstart.md) first. It
covers one backup and one restore in about fifteen minutes; this track picks up from there and goes
deeper.

## Two prerequisites worth knowing before you start

Both were discovered by running this track, and both produce confusing failures if you miss them.

**Set `output:` on the backup job.** Without it the storage backend names the artifact itself,
Sentinel never learns the final path, and no manifest is written. `sentinel backup verify` then
reports `missing_manifest`, and restore has no hash to check before applying.

**Override `scheduler.lock_dir`.** It defaults to `/var/run/sentinel`, which a non-root user cannot
create. Backups do not take that lock, so the problem only appears at the first restore.

**Incremental backup additionally needs `summarize_wal=on` set server-side.** The
`wal_summary_check` configuration key does not verify this for you: it is stored but never probes the
server. The incremental page shows how to start the container with the setting applied.

## Where this track stops

Point-in-time recovery cannot currently be planned: the backup pipeline never records a PITR
capability on the artifact, so the planner rejects every request regardless of timestamp. The
[PITR page](./pitr.md) explains the mechanism and shows the real rejection rather than pretending the
step works.

Incremental **backup** works and is covered in full. Incremental **restore** currently fails while
staging its baseline, which the chain page states where you would meet it.

<!-- sources: internal/domain/backup/pipeline.go, internal/domain/backup/incremental/, internal/adapters/restore/chain_assembler/, internal/config/types.go, infra/docker/docker-compose.yml -->
