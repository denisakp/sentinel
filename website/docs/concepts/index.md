---
title: Concepts
description: How Sentinel's building blocks work and why each one exists; read these to understand the product, not just operate it.
sidebar_position: 1
---

These pages explain how Sentinel works. They are not procedures; for those, follow a
[tutorial](../tutorials/index.md) or reach for the [reference](../reference/index.md). Read a concept
page when you want to understand *why* something behaves the way it does, or when a procedure did
something you did not expect.

Each page explains what the concept is for, how it actually works, how it is configured, and how it
fails.

## Start here

| Page | Read it when |
|---|---|
| [Backup](./backup.md) | You want to know what actually happens between "run a backup" and an artifact existing. |
| [Restore](./restore.md) | You want to know how Sentinel decides *which* artifact to restore, and what it does before touching your database. |
| [Schedule](./schedule.md) | You are moving from running backups by hand to running them on a cron loop. |

## Not yet written

The remaining concepts (encryption, retention, manifests, credential sanitization, storage backends,
locking, incremental and point-in-time recovery, monitoring history, compression, and notifications)
are being published in later increments.

Until then, the [runbooks in the repository](https://github.com/denisakp/sentinel/tree/develop/docs/runbooks)
cover those areas from an operator's angle, and the
[configuration reference](../reference/configuration.md) documents every key they use.

<!-- sources: internal/domain/backup/, internal/domain/restore/, internal/domain/schedule/ -->
