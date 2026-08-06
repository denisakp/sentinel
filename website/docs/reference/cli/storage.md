---
title: sentinel storage
description: Reference for sentinel storage status, its flags and output, and the three blind spots that make its report incomplete.
sidebar_position: 10
---

Reports reachability, backup count, and total size for each backend declared in the configuration's named `storages:` map.

## Synopsis

```text
sentinel storage [command]
sentinel storage status [flags]
```

`sentinel storage` on its own prints help. `status` is read-only: it connects to each named backend, lists its objects, and prints a summary. It never uploads, downloads, or deletes.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `status` | Connects to every entry in the top-level `storages:` map and reports whether it is reachable, how many backups it holds, their total size, and the timestamp of the most recent one. |

## Flags

### `sentinel storage`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `storage`. |

### `sentinel storage status`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--config` | string | `./sentinel-config.yaml` | Path to the Sentinel YAML config. When omitted, `./sentinel-config.yaml` in the working directory is used; if that file does not exist the command fails with an error naming the path it searched. The config is fully validated before any backend is contacted. |
| `-h`, `--help` | bool | `false` | Print help for `status`. |
| `--output` | string | inherits `log_format` from the config, which defaults to `json` | Output format: `json` or `text`. Any value other than `json` produces the text form. |

Because `log_format` defaults to `json`, omitting `--output` prints JSON on a configuration that does not set `log_format: text`. Pass `--output text` explicitly for the human-readable table.

Per-backend fields, identical in both formats:

| Field | Type | Description |
|---|---|---|
| `name` | string | The key under `storages:`. |
| `type` | string | The declared `type`, echoed verbatim even when it is not a supported backend. |
| `reachable` | bool | Whether the backend was constructed and listed successfully. Rendered as `OK` or `UNREACHABLE` in text output. |
| `backup_count` | int | Number of objects the backend reported. `0` when unreachable. |
| `total_size_mb` | float | Sum of object sizes in mebibytes. `0` when unreachable. |
| `last_backup` | string | RFC 3339 timestamp of the newest object. Omitted when there is none. |
| `error` | string | The construction or listing error. Omitted when reachable. |

Supported types are `local`, `s3`, `gcs`, `google-drive`, and `azure`. Anything else is reported unreachable with `unsupported storage type: <value>`.

:::caution `sentinel storage status` always exits 0
The exit code is 0 whether every backend is reachable or none of them is. It cannot be used directly as a health gate. Parse the JSON and test the `reachable` field instead, for example `sentinel storage status --config sentinel.yaml --output json | jq -e 'all(.reachable)'`.
:::

## Known defects

:::danger Named Azure backends are always reported unreachable
The Azure branch builds its client with only the account name and container, leaving the authentication type empty, and never passes `azure_storage_key` or `azure_storage_key_env`. Every named Azure storage therefore fails with `azure: unsupported auth type "" (must be managed_identity, connection_string, or sas_token)`, including one that backups are writing to successfully. The rest of Sentinel constructs Azure through the storage registry with the account key and is unaffected. Treat an `UNREACHABLE` Azure line carrying that exact message as a reporting defect, not an outage, and confirm the real state with `sentinel backup verify --all --config <file>`. Tracked as issue #161.
:::

:::caution Only the named `storages:` map is inspected
A job that declares its backend inline under `databases.<job>.storage:` or inherits it from `defaults.storage` never appears in the report, because neither is copied into `storages:`. A configuration that uses inline storage exclusively prints `No named storage backends configured.` while backups run normally. A job that references a named entry by `storage.name` is covered, since the reference resolves to that entry. Tracked with the same issue #161.
:::

:::caution Named entries are never type-validated
Configuration validation checks `storage.type` on each backup job, not on the entries under `storages:`. A typo such as `type: s#` in a named entry that no job references passes validation and only surfaces here, as `unsupported storage type: s#` on a line reported unreachable. Tracked with the same issue #161.
:::

## Examples

Check every named backend, in the human-readable form:

```bash
sentinel storage status --config sentinel.yaml --output text
```

Output for a configuration with one local and one misconfigured entry:

```text
Storage Backend Status (2 configured)
─────────────────────────────────────────
  onsite               local        OK
    Backups: 42  Size: 1180.4 MB
    Last backup: 2026-08-05T18:15:03Z
  offsite              azure        UNREACHABLE
    Error: azure: unsupported auth type "" (must be managed_identity, connection_string, or sas_token)
```

The Azure line is the defect above, not a credentials problem in your configuration.

Emit JSON for monitoring, which is also what you get with no `--output` on a default configuration:

```bash
sentinel storage status --config sentinel.yaml --output json
```

An array of objects with the fields listed above. Since the exit code is always 0, gate on the payload:

```bash
sentinel storage status --config sentinel.yaml --output json \
  | jq -e '[.[] | select(.type != "azure")] | all(.reachable)'
```

Excluding `azure` keeps the gate usable until issue #161 is fixed; remove that filter once it is.

A configuration shape that `status` can see, using a named backend referenced by the job:

```yaml
storages:
  onsite:
    type: local
    local_path: /var/backups/sentinel
  offsite:
    type: s3
    s3_bucket: sentinel-backups
    s3_region: eu-west-3
    s3_access_key_id_env: SENTINEL_S3_KEY_ID
    s3_secret_access_key_env: SENTINEL_S3_SECRET

databases:
  prod-postgres:
    storage:
      name: offsite
```

Moving an inline `storage:` block into `storages:` and referencing it by name is the only way to make a job's backend visible to this command.

## Related

- [Storage backends](../../concepts/storage-backends.md)
- [Configuration reference](../configuration.md)
- [`sentinel backup`](./backup.md) for the storage flags that override a configured backend
- [`sentinel retention`](./retention.md), which deletes through the same backends
- [`sentinel repair`](./repair.md)
- [CLI reference index](./index.md)

{/* sources: internal/cli/storage_cmd.go, internal/cli/config_resolver.go, internal/adapters/storage/registry.go, internal/adapters/storage/azure/auth.go, internal/adapters/storage/azure/config.go, internal/config/validator.go, internal/config/loader.go, internal/config/types.go */}
