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

| Engine | Track | Verified by |
|---|---|---|
| PostgreSQL | [Start here](./postgres/index.md) | Running it end to end against a real container |
| MySQL | [Start here](./mysql/index.md) | Reading the source |
| MariaDB | [Start here](./mariadb/index.md) | Reading the source |
| MongoDB | [Start here](./mongodb/index.md) | Reading the source |

The distinction in that last column is worth knowing. Every command, flag and configuration key on
every track was checked against the shipped binary, so none of them is invented. But only the
PostgreSQL track was written by actually running it, with each expected output captured from a real
session. The other three derive their expected results from the code and describe in prose what you
should observe, rather than showing a transcript nobody produced.

Where a step cannot work in the current release, the track says so and shows the real failure instead
of skipping it. That happens more than it should: point-in-time recovery cannot be planned for any
engine ([#148](https://github.com/denisakp/sentinel/issues/148)), and incremental restore fails while
staging its baseline ([#150](https://github.com/denisakp/sentinel/issues/150)). Incremental *backup*
works, and each track covers it.

## Before you start

You will need Docker, so that each track can hand you a disposable database rather than asking you to
find one. The repository's own
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up an instance of every supported engine if you prefer that to a single container.

You will also need Sentinel installed, see [Installation](../intro/installation.md), and the client
tools for your engine on your `PATH`.

<!-- sources: infra/docker/docker-compose.yml, scripts/e2e.sh -->
