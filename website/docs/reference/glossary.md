---
title: Glossary
description: "Sentinel-specific terms, defined once, each linking to the page that explains it properly."
sidebar_position: 5
---

Terms that mean something particular in Sentinel, or that are easy to confuse with a neighbouring
idea. Each entry links to the page that explains it in depth.

## A

**Artifact**
: The file a backup produces: a dump, optionally compressed, optionally encrypted. An artifact is
  written to a [storage backend](../concepts/storage-backends.md) and is accompanied by a
  [manifest](../concepts/manifest.md). See [Backup](../concepts/backup.md).

**`additional_args`**
: Engine-specific arguments passed through to the underlying dump tool. Parsed with shell-like
  tokenisation and validated before use. See the
  [`additional_args` reference](./additional-args.md).

## B

**Backup job**
: A named entry under `databases:` in the configuration, describing one database to back up and
  where to put it. Distinct from a *restore job*. See [Backup](../concepts/backup.md).

**Baseline**
: The full backup a [chain](#c) of incrementals is built on. Deleting it makes every incremental
  above it useless, which is why [retention](../concepts/retention.md) protects the active one.

**Binary log**
: MySQL and MariaDB's record of changes, the basis of their incremental backups. Sentinel archives
  binary logs beside the artifact. See
  [Incremental and PITR](../concepts/incremental-pitr.md).

## C

**Chain**
: A baseline plus the ordered incrementals that depend on it. Chain depth is capped by
  `max_chain_depth`, after which a new full backup starts a new chain. Validate one with
  `sentinel restore validate-chain`. See
  [Incremental and PITR](../concepts/incremental-pitr.md).

**Conflict strategy**
: What a restore does when data already exists at the target: `error` (the default), `replace`, or
  `ignore`. MySQL and MariaDB collapse the last two into the same behaviour. See
  [Restore](../concepts/restore.md).

## D

**`defaults_file`**
: A MySQL or MariaDB option file, typically a `my.cnf`, whose `[client]` section supplies host, user
  and password for the whole pipeline rather than just the dump subprocess. See
  [Credential handling](../concepts/credential-sanitization.md).

**Dispatcher**
: The component that fans a single event out to every configured notification channel. See
  [Notifications](../concepts/notifications.md).

**Dry run**
: A rehearsal that reports what would happen without doing it. Note that `sentinel restore dry-run`
  does not invoke the restore planner, so it can pass for a job that cannot actually run
  ([#152](https://github.com/denisakp/sentinel/issues/152)).

## E

**Envelope**
: The container format wrapping an encrypted artifact, identified by a `SENC` header. Version 2
  chunks the plaintext and derives a per-artifact key. See
  [Security and encryption](../concepts/security-encryption.md).

**Execution**
: One recorded run of a job, successful or not, stored in the
  [history database](../concepts/monitoring-history.md).

## F

**Full backup**
: A complete copy, as opposed to an incremental one. Every chain starts with a full backup as its
  baseline. See [Backup](../concepts/backup.md).

## G

**GFS**
: Grandfather-father-son. A retention scheme keeping the newest backup of each recent day, week,
  month and year, layered on top of the flat count and age rules. See
  [Retention](../concepts/retention.md).

## H

**History database**
: The local SQLite file recording every execution. Its location is `history_db_path`, defaulting to
  `~/.sentinel/history.db`. See
  [Monitoring and execution history](../concepts/monitoring-history.md).

## I

**Incremental backup**
: A backup capturing only what changed since the previous one in its chain, using the engine's own
  change log. Works on all four engines. Incremental *restore* is a separate matter, and currently
  fails ([#150](https://github.com/denisakp/sentinel/issues/150)). See
  [Incremental and PITR](../concepts/incremental-pitr.md).

## L

**Lock**
: A file taken for the duration of a job so two runs cannot interleave. Restores take one; backups
  currently do not ([#163](https://github.com/denisakp/sentinel/issues/163)). See
  [Locking and concurrency](../concepts/locking.md).

## M

**Manifest**
: The `<artifact>.manifest.json` sidecar recording an artifact's SHA-256, its encryption parameters,
  and its position in a chain. Written only when the job sets `output:`
  ([#151](https://github.com/denisakp/sentinel/issues/151)). See
  [Manifests and integrity](../concepts/manifest.md).

**Master key**
: The 32-byte key from which per-artifact encryption keys are derived. Provisioned through
  `encryption_key_env` or `encryption_key_file`. Losing it is unrecoverable. See
  [Security and encryption](../concepts/security-encryption.md).

## O

**Oplog**
: MongoDB's operation log, the basis of its incremental backups. Recovery granularity is bounded by
  the oplog window, which is how much history the server retains. See
  [Incremental and PITR](../concepts/incremental-pitr.md).

## P

**PITR**
: Point-in-time recovery: restoring to a specific moment rather than to a backup boundary. Advertised
  by Sentinel, but currently unplannable for any engine
  ([#148](https://github.com/denisakp/sentinel/issues/148)). See
  [Incremental and PITR](../concepts/incremental-pitr.md).

**Planner**
: The component deciding whether a requested restore is possible and how to assemble it, returning a
  plan or a reason code such as `unsupported_database_type` or `missing_advanced_metadata`. See
  [Restore](../concepts/restore.md).

## R

**Redaction**
: Removing credentials from anything Sentinel logs, handled by the `sanitize` package. It covers
  what Sentinel writes, not what an engine tool prints. See
  [Credential handling](../concepts/credential-sanitization.md).

**Restore job**
: A named entry under `restores:`, describing where to restore from and what to restore into.
  Disabled by default, unlike backup jobs. See [Restore](../concepts/restore.md).

**Retention**
: Deciding which old backups to delete, by count, by age, or by GFS tier. See
  [Retention](../concepts/retention.md).

## S

**Secrets file**
: A file holding database credentials, referenced by `defaults_file` or `mongo_secrets_file`, and
  optionally encrypted at rest in an `SSEC` container. See
  [Security and encryption](../concepts/security-encryption.md).

**Staging**
: Writing an artifact to local disk before uploading it to a remote backend, so hashing and
  encryption happen on a file Sentinel controls. Restores stage in the other direction, downloading
  before applying. See [Storage backends](../concepts/storage-backends.md) and
  [the MongoDB staging reference](./mongo-staging.md).

**Storage backend**
: Where artifacts are written: local disk, S3, Google Cloud Storage, Google Drive, or Azure Blob.
  Restore reads from only the first three. See
  [Storage backends](../concepts/storage-backends.md).

## W

**WAL**
: PostgreSQL's write-ahead log, the basis of its incremental backups. Requires `summarize_wal=on`
  server-side on PostgreSQL 17 and later. See
  [Incremental and PITR](../concepts/incremental-pitr.md).

{/* sources: website/docs/concepts/, internal/config/types.go, internal/domain/restore/planner.go */}
