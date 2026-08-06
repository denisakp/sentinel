---
title: sentinel config
description: Reference for sentinel config validate, the checks it runs, the errors it emits, and how the configuration path is resolved.
sidebar_position: 7
---

Parses a Sentinel YAML configuration file, applies every default and environment substitution, and runs the full schema validation without executing any backup or restore.

## Synopsis

```text
sentinel config [command]
sentinel config validate [flags]
```

`sentinel config` on its own is a grouping command. It takes no action, prints its help text, and exits 0.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `validate` | Load, normalise, and fully validate a configuration file. Prints `configuration is valid` and exits 0, or prints the first error and exits 1. |

## Flags

### `sentinel config`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `config`. |

### `sentinel config validate`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | `./sentinel-config.yaml` | Path to the YAML configuration file. |
| `-h`, `--help` | bool | `false` | Print help for `validate`. |

## Configuration path resolution

Two steps, in order:

1. A non-empty `--config` value wins unconditionally.
2. Otherwise `./sentinel-config.yaml` in the current working directory.

If neither resolves, the command fails before reading anything:

```text
Error: no configuration file found: "./sentinel-config.yaml" does not exist in the current directory.
Use --config to specify a path, e.g.: sentinel --config /path/to/sentinel.yaml
```

The same resolver backs every Sentinel command that reads a configuration file, so a path that works here works everywhere.

:::note `-c` is not a site-wide convention
`config validate` and `db migrate status` accept the short form `-c`. Most other commands register `--config` in long form only. Prefer `--config` in scripts.
:::

## What validation covers

`validate` runs the load phase and the validation phase. Both can fail, and the load phase runs first, so a YAML or environment problem is reported before any schema rule.

### Load phase

| Step | Failure surfaces as |
|---|---|
| Read the file | `failed to read config file: ...` |
| Parse YAML | `failed to parse YAML: ...` with the offending line |
| Require at least one backup job | `configuration must define at least one backup job` |
| Apply defaults | Never fails; fills `history_db_path`, `max_concurrent_backups`, `log_format`, scheduler settings, per-job inheritance from `defaults:` |
| Resolve named storage references | `storage '<name>' referenced by ... is not defined` |
| Apply environment overrides and interpolate `*_env` keys | `backup '<job>': environment variable '<NAME>' is not set` |

Because interpolation happens during load, **every environment variable named by the configuration must be set in the shell running `config validate`**. Validating a production configuration from a workstation that lacks those variables fails on the first missing one, not on a schema problem.

### Validation phase

Top-level rules:

| Rule | Error |
|---|---|
| `version` must be exactly `"1.0"` | `invalid config version '<value>': expected '1.0'` |
| At least one backup job | `no backup jobs defined in configuration` |
| `max_concurrent_backups` in 1..100 | `max_concurrent_backups must be between 1 and 100` |
| `max_concurrent_restores` in 1..100 when set | `max_concurrent_restores must be between 1 and 100` |
| `integrity:` block internally consistent | `integrity: ...` |
| `scheduled_integrity_check` cron parses | reported with the offending expression |

Per backup job, prefixed `backup '<name>': `, the validator checks the engine type, a non-empty `database`, the auto-discovery `strategy` when `database: "*"`, the cron expression, connection fields, `*_env` variable names, the storage block, retention, database options, compression, notifications, the incremental-backup policy, and the TLS block.

Per restore job, prefixed `restore '<name>': `, it checks connection fields, the cron expression, a non-negative `timeout_seconds`, the conflict strategy, `allow_cascade` (PostgreSQL only), the advanced restore options, and that `restore_options.additional_args` tokenises. See [`additional_args` quoting and parsing](../additional-args.md).

### Warnings

Warnings are written to stderr as structured logs and never change the exit code:

| Event | Meaning |
|---|---|
| `tls_not_configured` | A backup job has no `tls:` block. Connections may be unencrypted. |
| Plaintext-credential warning | A storage block holds a literal credential rather than an `*_env` reference. |

A configuration can be valid and still emit both.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Configuration loaded and validated. `configuration is valid` is printed to stdout. |
| `1` | Any load or validation failure, including a missing configuration file. |

Validation stops at the first error. A file with several problems needs several runs.

## Examples

Validate an explicit path:

```bash
sentinel config validate --config sentinel.yaml
```

Successful output, with the TLS warning a job without a `tls:` block produces:

```text
2026/08/05 18:27:55 WARN TLS not configured for database event=tls_not_configured database=demo
configuration is valid
```

Validate the conventional file in the working directory:

```bash
sentinel config validate
```

Gate a deployment in CI, with the credentials the configuration expects supplied by the runner's secret store:

```bash
export DEMO_PG_PASSWORD="$(cat /run/secrets/demo_pg_password)"
sentinel config validate --config /etc/sentinel/sentinel.yaml || exit 1
```

A missing `version` key is the most common first failure on a hand-written file:

```text
Error: invalid config "sentinel.yaml": invalid config version '': expected '1.0'
```

Add `version: "1.0"` at the top level and re-run.

## Related

- [Configuration reference](../configuration.md)
- [`additional_args` quoting and parsing](../additional-args.md)
- [`sentinel db`](./db.md)
- [`sentinel backup`](./backup.md)
- [`sentinel schedule`](./schedule.md)
- [CLI reference index](./index.md)
- [Installing Sentinel](../../intro/installation.md)

<!-- sources: internal/cli/config.go, internal/cli/config_resolver.go, internal/config/loader.go, internal/config/validator.go, internal/config/restore_types.go -->
