---
title: Guides
description: Task-oriented procedures for setting up, running and maintaining Sentinel in production.
sidebar_position: 1
---

Each guide answers one question of the form "how do I do X". They assume you understand the concept
behind the task; if you do not, the [concepts](../concepts/index.md) pages explain the machinery, and
the [tutorials](../tutorials/index.md) teach by doing.

Every guide states when to use it and what must be true before you start, because the wrong procedure
applied confidently is worse than no procedure at all.

## Setting up

| Guide | Use it when |
|---|---|
| [Environment setup](./environment-setup.md) | You are bringing up databases to work against, locally or in CI. |
| [Database credentials](./database-credentials.md) | You are deciding how Sentinel will authenticate, and want to keep secrets out of process arguments. |
| [Enable encryption](./enable-encryption.md) | You want artifacts encrypted at rest. Off by default. |
| [Key rotation](./key-rotation.md) | You are replacing an encryption key without losing access to existing artifacts. |
| [Alerting setup](./alerting-setup.md) | You want to hear about failures rather than discover them. |

## Running backups

| Guide | Use it when |
|---|---|
| [Run a backup from configuration](./run-backup-from-config.md) | You are taking your first real backup, or debugging one that will not run. |
| [Backup compression](./backup-compression.md) | Artifact size or transfer cost matters. |

## Retention

| Guide | Use it when |
|---|---|
| [Apply retention](./apply-retention.md) | You need old backups expired, and want to know exactly what will be deleted first. |
| [GFS retention](./retention-gfs.md) | Flat count and age rules are not enough and you need calendar tiers. |

## Storage

| Guide | Use it when |
|---|---|
| [Check a storage backend](./check-storage-backend.md) | You want to confirm Sentinel can reach where it writes. |
| [Migrate a storage backend](./migrate-storage-backend.md) | You are moving artifacts between backends. Read the restore-support caveat before you start. |
| [Restore from GCS](./restore-from-gcs.md) | Your artifacts live in Google Cloud Storage. |

## Integrity and history

| Guide | Use it when |
|---|---|
| [Verify backup integrity](./verify-backup-integrity.md) | You want to know whether a specific artifact is still intact. |
| [Repository integrity sweep](./integrity-sweep.md) | You want that answer for every artifact at once. |
| [Inspect monitor history](./inspect-monitor-history.md) | You are asking what ran, when, and whether it worked. |

## Restore and maintenance

| Guide | Use it when |
|---|---|
| [Parallel restore](./parallel-restore.md) | You are restoring several jobs and want them to run concurrently. |
| [Upgrade the Sentinel binary](./upgrade-sentinel-binary.md) | You are moving to a new release. |
| [Database migration status](./db-migration-status.md) | You want to know what schema version the history database is on. |
| [Monitor schema migration](./monitor-schema-migration.md) | A migration needs performing or has gone wrong. |

## A note on accuracy

These guides describe what Sentinel does today, not what it is meant to do. Where a feature does not
work, or works differently from the runbook it was derived from, the guide says so and links to the
open issue rather than quietly documenting the intended behaviour.

That means some pages carry warnings about configuration keys that are accepted but ignored, and
about commands that report success without acting. Those warnings are the most valuable part of the
page. Read them before relying on the feature.

## Not yet written

Two guides are held back until the defects they depend on are resolved, because documenting them now
would mean describing procedures that cannot be followed:

- **Starting the scheduler**, which depends on `schedule stop`
  ([#138](https://github.com/denisakp/sentinel/issues/138)).
- **Restore rehearsals**, which depend on `verify_after_restore`
  ([#149](https://github.com/denisakp/sentinel/issues/149)).

Until then, the [scheduler concept page](../concepts/schedule.md) covers running the cron loop, and
the [restore concept page](../concepts/restore.md) covers rehearsing a restore by hand.

For incident procedures, the
[runbooks in the repository](https://github.com/denisakp/sentinel/tree/develop/docs/runbooks) remain
the reference until the operations section is published.

<!-- sources: docs/runbooks/, internal/cli/ -->
