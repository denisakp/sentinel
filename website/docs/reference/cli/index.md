---
title: Command-line interface
description: Every Sentinel command, with its subcommands and flags.
sidebar_position: 1
---

Sentinel is a single binary with eleven top-level commands. Every command accepts `--help`, and
`--help` is authoritative; this reference is checked against it.

```bash
sentinel --help
sentinel <command> --help
```

## Commands

| Command | Purpose |
|---|---|
| [`backup`](./backup.md) | Run a database backup, and inspect or verify existing ones |
| [`restore`](./restore.md) | Manage backup restoration and recovery |
| [`schedule`](./schedule.md) | Manage automated backup and restore scheduling |
| [`monitor`](./monitor.md) | Monitor backup history and statistics |
| [`retention`](./retention.md) | Manage backup retention policies |
| [`config`](./config.md) | Manage configuration files |
| [`db`](./db.md) | Database schema and migration management |
| [`security`](./security.md) | Security key management |
| [`storage`](./storage.md) | Storage backend management |
| [`repair`](./repair.md) | Reconcile internal state across monitor rows, manifests, artifacts, and locks |
| [`version`](./version.md) | Print Sentinel build metadata |

Cobra also supplies `help` and `completion`, which behave as they do in any Cobra program.

## Global flags

The root command carries only two flags:

| Flag | Description |
|---|---|
| `-h`, `--help` | Help for the command |
| `-v`, `--version` | Version for `sentinel` |

**`--config` is not a global flag.** Each command that reads a configuration file registers its own,
which is why every example on this site repeats `--config sentinel.yaml` rather than setting it once.

:::caution Three `backup` subcommands cannot be run today
`sentinel backup chain-status`, `chain-list`, and `force-full` require a configuration file but do
not register a `--config` flag, and the one on `sentinel backup` is not inherited. They fail whether
the flag is passed or not. See [`sentinel backup`](./backup.md) for detail.
:::

<!-- sources: internal/cli/root.go, internal/cli/backup.go, internal/cli/restore.go, internal/cli/schedule.go -->
