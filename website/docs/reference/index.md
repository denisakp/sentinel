---
title: Reference
description: Exhaustive reference for Sentinel's command-line interface and its YAML configuration schema.
sidebar_position: 1
---

Lookup material. Every flag, every configuration key, with its type, default, and constraints; no
narrative, no curation.

If you are trying to understand *why* something works the way it does, the
[concepts](../concepts/index.md) pages are the better starting point. If you are trying to get
something done for the first time, start with a [tutorial](../tutorials/index.md).

## What is here

| Page | Contents |
|---|---|
| [Configuration](./configuration.md) | The complete YAML schema: every key, its type, whether it is required, its default, and any engine restriction. |
| [Command-line interface](./cli/index.md) | Every command and subcommand, with all flags. |

## A note on accuracy

Every command, flag, and configuration key documented on this site is checked against the shipped
binary and the configuration schema in the source. Nothing here is aspirational: if it is written
down, it exists.

One point worth stating plainly, because it comes up often: **do not pass a password on the command
line.** Supply credentials through `--password-env`, `--password-file`, or the `*_env` family of
configuration keys. A password given as a command-line argument is visible in `ps` output to every
user on the machine, in `/proc/<pid>/cmdline`, and in shell history.

A `--password` flag does still exist. It is deprecated, hidden from `--help`, prints a warning when
used, and is slated for removal in the next minor release. It is not documented here because it
should not be used.

<!-- sources: internal/cli/root.go, internal/config/types.go, internal/config/restore_types.go -->
