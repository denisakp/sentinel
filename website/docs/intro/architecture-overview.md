---
title: How Sentinel fits together
description: The mental model behind Sentinel; what a backup run actually does, and where each moving part lives.
sidebar_position: 1
---

Sentinel is a single binary that orchestrates tools you already have. Understanding the shape of a
run makes everything else in this documentation easier to place.

## One binary, no server

There is no daemon holding state, no control plane, and no database of its own beyond a local SQLite
file recording what happened. Every Sentinel invocation is a process that starts, does one job, and
exits. The scheduler is the only long-running mode, and it is a foreground process you supervise like
any other service.

This matters for how you operate it: if nothing is running, nothing is happening. There is no queue
draining in the background and no work that resumes on its own.

## What a backup run actually does

A single backup job moves through the same sequence every time:

1. **Validate**: the configuration is parsed and checked. Credentials named by `*_env` fields are
   resolved now, so a missing environment variable fails immediately rather than halfway through.
2. **Lock**: a file lock is taken for the job, so two concurrent runs of the same job cannot
   interleave and corrupt each other's output.
3. **Ping**: connectivity to the database is confirmed before any work begins.
4. **Dump**: Sentinel builds an argument list and runs your engine's own tool: `pg_dump`,
   `mysqldump`, `mariadb-dump`, or `mongodump`. Credentials are passed through the environment, never
   as command-line arguments.
5. **Pipeline**: the dump is optionally compressed, optionally encrypted, hashed with SHA-256, and
   written to the configured storage backend. A manifest recording the hash and lineage is written
   alongside it.

   :::caution Peak memory scales with dump size
   The dump is buffered in memory in full before it is hashed and written. Compression and
   encryption are streaming, but the dump that feeds them is not, so a database whose uncompressed
   dump is 50 GB needs comparable memory to back up. Size the machine accordingly.
   :::
6. **Record**: the outcome, success or failure, is written to the execution history.
7. **Notify**: configured channels are told what happened.

A restore run is the same idea in reverse: resolve which artifact to use, stage it locally, verify
its hash, decrypt if needed, then feed it to `pg_restore`, `mysql`, `mariadb`, or `mongorestore`.

## The moving parts

| Part | What it is | Read more |
|---|---|---|
| **Backup** | Producing an artifact from a live database | Concepts (increment 2) |
| **Restore** | Turning an artifact back into a database | Concepts (increment 2) |
| **Schedule** | The cron loop that runs backup and restore jobs | Concepts (increment 2) |
| **Storage backend** | Where artifacts live: local, S3, GCS, Google Drive, Azure | Concepts (increment 3) |
| **Manifest** | The SHA-256 record proving an artifact is intact and where it sits in a chain | Concepts (increment 3) |
| **Retention** | Deciding which old artifacts to delete | Concepts (increment 3) |
| **Encryption** | Opt-in AES-256 protection of artifacts at rest | Concepts (increment 3) |
| **Locking** | Preventing two runs of the same job from colliding | Concepts (increment 3) |
| **Monitor** | The local SQLite history of every execution | Concepts (increment 4) |
| **Notifications** | Slack, Discord, email, webhook | Concepts (increment 4) |

:::note This documentation is still being written
Concept, guide, tutorial, and reference sections are being published incrementally. Sections not yet
listed in the sidebar have not been written yet; they are not missing links, just future work. In
the meantime, the [runbooks in the repository](https://github.com/denisakp/sentinel/tree/develop/docs/runbooks)
cover operational procedures in depth.
:::

## Three ideas worth internalising early

**Credentials are kept out of process arguments.** Configuration fields ending in `_env` name an
environment variable rather than holding a value. This is not a stylistic preference; anything on a
command line is visible in `ps` output to every user on the machine, and ends up in shell history.
Use `--password-env` or `--password-file`.

A `--password` flag still exists, but it is deprecated, hidden from `--help`, and slated for removal
in the next minor release. Using it prints a warning explaining why. Treat it as gone.

**Every artifact is hashed when written.** Integrity is not an afterthought you enable. A manifest
recording the SHA-256 of the artifact is written at backup time, which is what makes
`sentinel backup verify` able to tell you later whether a file has changed underneath you.

**Incremental backup uses your engine's own change log.** Sentinel does not invent a diffing scheme.
PostgreSQL incrementals ride on the write-ahead log, MySQL and MariaDB on binary logs, MongoDB on the
oplog. That is why incremental support and point-in-time recovery differ per engine; the mechanism
belongs to the database, not to Sentinel.

## Next

- **[Quickstart](./quickstart.md)**: see the sequence above happen for real.

<!-- sources: CLAUDE.md §Architecture, internal/domain/backup/executor.go, internal/domain/restore/executor.go, internal/adapters/lock/, internal/sanitize/, internal/adapters/crypto/, internal/cli/backup.go -->
