---
title: What Sentinel is
description: Sentinel is a command-line tool that backs up, restores, and recovers PostgreSQL, MySQL, MariaDB, and MongoDB databases.
sidebar_position: 1
---

Sentinel is an open-source command-line tool for automated database backup, restore, and disaster
recovery. You point it at a YAML file describing your databases, your storage, and your schedule,
and it takes care of the rest; running dumps, uploading them, verifying their integrity, expiring
old ones, and telling you when something breaks.

It is a single binary with no server, no agent, and no control plane. If it is running, it is
because cron or your scheduler started it.

## Supported databases

| Engine | Full backup | Incremental | Point-in-time recovery |
|---|---|---|---|
| PostgreSQL | Yes | Yes: write-ahead log, PostgreSQL 17+ | Yes |
| MySQL | Yes | Yes: binary logs | Yes, to a target timestamp |
| MariaDB | Yes | Yes: binary logs | Yes, to a target timestamp |
| MongoDB | Yes | Yes: oplog | Bounded by the oplog window |

## What it does

- **Backup and restore** across all four engines, from a single configuration file.
- **Storage anywhere**: a local directory, S3-compatible object storage, Google Cloud Storage,
  Google Drive, or Azure Blob Storage.
- **Incremental backup** using each engine's own change log, so daily backups do not mean daily full
  dumps.
- **Advanced restore**: PostgreSQL point-in-time recovery and incremental chain assembly.
- **Scheduling**: a built-in cron loop that runs both backup and restore jobs.
- **Retention**: automatic expiry by count, by age, or by grandfather-father-son tiers.
- **Monitoring**: every execution recorded in a local SQLite history you can query and export.
- **Notifications**: Slack, Discord, email, and generic webhooks.
- **Integrity and encryption**: SHA-256 manifests on every artifact, and opt-in AES-256 encryption.

## What it deliberately does not do

Knowing the boundaries early saves you evaluating it for the wrong job.

- **It is not a replication or high-availability tool.** Sentinel takes point-in-time copies. It
  does not keep a standby in sync, and it will not fail your application over.
- **It does not run a server.** There is no daemon holding state, no web UI, and no API. The
  scheduler is a foreground process you supervise like any other.
- **It does not install your database client tools.** `pg_dump`, `mysqldump`, `mongodump` and their
  restore counterparts must already be on your `PATH`. Sentinel orchestrates them; it does not
  reimplement them.
- **It does not manage your secrets.** It reads credentials from environment variables and secrets
  files that you provision. It will never accept a password as a command-line argument.
- **It is not a database migration tool.** Schema change management is somebody else's job.

## Where to go next

- **[Installation](./intro/installation.md)**: get the binary and verify it is genuine.
- **[Quickstart](./intro/quickstart.md)**: one backup and one verified restore, from nothing.
- **[How Sentinel fits together](./intro/architecture-overview.md)**: the mental model, before you
  go deeper.

<!-- sources: README.md, release-notes.md, internal/cli/root.go -->
