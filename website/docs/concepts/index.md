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

## The core loop

Start here if you are new. These three explain what Sentinel does on a normal day.

| Page | Read it when |
|---|---|
| [Backup](./backup.md) | You want to know what actually happens between "run a backup" and an artifact existing. |
| [Restore](./restore.md) | You want to know how Sentinel decides *which* artifact to restore, and what it does before touching your database. |
| [Schedule](./schedule.md) | You are moving from running backups by hand to running them on a cron loop. |

## Keeping backups trustworthy

| Page | Read it when |
|---|---|
| [Manifests and integrity](./manifest.md) | You want to know how Sentinel proves an artifact has not changed, and what `backup verify` does and does not tell you. |
| [Security and encryption](./security-encryption.md) | You are deciding whether to encrypt artifacts at rest, or handling a key. |
| [Credential handling](./credential-sanitization.md) | You are choosing how Sentinel authenticates, and want passwords kept out of process arguments and logs. |
| [Locking and concurrency](./locking.md) | Two runs might overlap, or one is refusing to start. |

## Managing what accumulates

| Page | Read it when |
|---|---|
| [Retention](./retention.md) | You need old backups expired, and want to know exactly what will be deleted. |
| [Storage backends](./storage-backends.md) | You are choosing where artifacts live, or moving them. |
| [Compression](./compression.md) | Artifact size or transfer cost matters. |
| [Incremental and PITR](./incremental-pitr.md) | Full backups are too expensive, or you need to recover to a point in time. |

## Knowing what happened

| Page | Read it when |
|---|---|
| [Monitoring and history](./monitoring-history.md) | You are asking what ran, when, and whether it worked. |
| [Notifications](./notifications.md) | You want to hear about failures rather than discover them. |

## A note on what these pages say

They describe what Sentinel does today, not what it is meant to do. Where a capability does not work,
the page says so and links to the open issue rather than describing the intended behaviour.

Several pages carry warnings of that kind. They are the most useful part of the page: read them
before you rely on the feature.

<!-- sources: internal/domain/, internal/adapters/ -->
