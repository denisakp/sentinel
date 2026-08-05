---
title: Tutorials
description: Guided, end-to-end tracks that take you from an empty configuration to a working backup and restore, one database engine at a time.
sidebar_position: 1
---

Each tutorial is a sequence you follow start to finish, against a throwaway database you create as
you go. Nothing is assumed: no pre-existing environment, no prior Sentinel knowledge beyond the
[quickstart](../intro/quickstart.md).

Tutorials teach by doing. If you would rather understand the machinery first, read the
[concepts](../concepts/index.md); if you already know what you need and want the exact flag, use the
[reference](../reference/index.md).

## Pick your engine

| Engine | Track |
|---|---|
| PostgreSQL | [Start here](./postgres/index.md) |
| MySQL | Not yet written |
| MariaDB | Not yet written |
| MongoDB | Not yet written |

The MySQL, MariaDB, and MongoDB tracks are being written for a later increment. Until they land, the
[concepts](../concepts/index.md) pages describe the per-engine differences, and the
[configuration reference](../reference/configuration.md) documents every key those engines accept.

## Before you start

You will need Docker, so that each track can hand you a disposable database rather than asking you to
find one. The repository's own
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up an instance of every supported engine if you prefer that to a single container.

You will also need Sentinel installed, see [Installation](../intro/installation.md), and the client
tools for your engine on your `PATH`.

<!-- sources: infra/docker/docker-compose.yml, scripts/e2e.sh -->
