---
title: sentinel retention
description: Reference for sentinel retention preview and apply, every flag, the rules that select deletion candidates, and four known defects.
sidebar_position: 6
---

Evaluates each backup job's retention policy against the execution history and deletes the artifacts that no configured rule keeps.

## Synopsis

```text
sentinel retention [command]
sentinel retention preview [flags]
sentinel retention apply [flags]
```

`sentinel retention` on its own prints help and does nothing. Both subcommands read the job's policy from the configuration file, query the history database at `history_db_path` for that job's successful executions, compute the deletion candidates, and (for `apply`) delete each candidate through the job's own storage backend.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `preview` | Computes and prints the deletion candidates without touching storage or history. Never deletes, whatever the configuration says. |
| `apply` | Deletes the candidates from storage and removes their rows from the history database. Pass `--dry-run` to compute and print without deleting. |

## Flags

### `sentinel retention`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `retention`. |

### `sentinel retention preview`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | `./sentinel-config.yaml` | Path to the YAML configuration file. When omitted, `./sentinel-config.yaml` in the working directory is used; if that file does not exist the command fails with an error naming the path it searched. |
| `-h`, `--help` | bool | `false` | Print help for `preview`. |
| `--job` | string | n/a | Restrict evaluation to one named backup job. Omit to evaluate every job that has a policy. |

`preview` does not accept `--dry-run`; passing it returns `Error: unknown flag: --dry-run`. Preview mode is unconditional.

### `sentinel retention apply`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | `./sentinel-config.yaml` | Path to the YAML configuration file. Same resolution as `preview`. |
| `--dry-run` | bool | `false` | Compute and print the candidates, then stop before any storage or history mutation. |
| `-h`, `--help` | bool | `false` | Print help for `apply`. |
| `--job` | string | n/a | Restrict the run to one named backup job. An unknown name fails with `Error: backup '<name>' not found` and exit 1. |

:::danger Destructive
`sentinel retention apply` permanently deletes backup artifacts from the configured storage backend and deletes their rows from the history database. There is no undo and no recycle bin. Run `sentinel retention preview --config <file> --job <job>` first and read the candidate list, and confirm the backups you intend to keep are verifiable with `sentinel backup verify --all --config <file>`.
:::

## Policy keys

Policies are configured per job under `databases.<job>.retention`, or once under `defaults.retention`. The default block is copied wholesale into any job whose own `retention` block sets nothing; it is never merged key by key, so a job that sets `keep_last` alone does not inherit the default `keep_days`.

| Key | Type | Default | Description |
|---|---|---|---|
| `keep_last` | int | `0` (rule off) | Keep the N most recent successful backups. |
| `keep_days` | int | `0` (rule off) | Keep successful backups newer than N days, measured from the current UTC time. |
| `dry_run` | bool | `false` | **Has no effect.** See the defect note below. |
| `gfs.keep_daily` | int | `0` (tier off) | Keep the newest backup of each of the last N occupied calendar days (UTC). |
| `gfs.keep_weekly` | int | `0` (tier off) | Keep the newest backup of each of the last N occupied ISO weeks. |
| `gfs.keep_monthly` | int | `0` (tier off) | Keep the newest backup of each of the last N occupied calendar months. |
| `gfs.keep_yearly` | int | `0` (tier off) | Keep the newest backup of each of the last N occupied calendar years. |

A job whose policy sets none of these is skipped entirely when no `--job` is given. Only executions with status `success` and a recorded file path are considered; failed runs are never candidates and are never cleaned up here.

Two safety rules apply after candidate selection:

- The newest backup is always retained, even when every rule would discard it.
- The full backup that anchors the chain of the most recent backup is retained and reported as `protected active baseline`.

## Known defects

:::note `retention.dry_run` is honoured, since the fix for issue #157
A job carrying `dry_run: true` is never deleted from, by `sentinel retention apply` or by the automatic sweep that runs after a scheduled backup. The key and the `--dry-run` flag combine and neither cancels the other: either alone makes the run report without deleting, and disabling a configured `dry_run: true` requires editing the configuration file.

**On v1.4.0 and earlier the key was parsed, validated, inherited, and then read by no deletion path**, so artifacts were deleted for real at the end of every `sentinel backup --config <file>` run. On those versions use `sentinel retention preview` as the only safe evaluation path.

One thing is unchanged: a job whose `retention` block contains `dry_run: true` and nothing else still counts as having a policy, which suppresses inheritance of `defaults.retention` for that job. Fixed by issue #157.
:::

:::caution `keep_last` and `keep_days` intersect, they do not union
When both flat rules are set, a backup is deleted if **either** rule discards it, so a backup survives only when **both** rules keep it. The runbooks and the v1.3.0 release note describe a union of keeps; that description is correct for GFS against the flat rules, and wrong for the two flat rules against each other. With `keep_last: 10` and `keep_days: 7`, an eleventh-newest backup taken two days ago is deleted even though `keep_days` would keep it. To get the union behaviour, configure one flat rule only. Tracked as issue #158.
:::

:::caution Sidecar files are orphaned
Deletion removes the artifact path recorded in the history database and nothing else. The matching `<artifact>.manifest.json` sidecar is left behind, as is any other sidecar written next to it. On local storage the directory accumulates manifests with no artifact; on object storage the same keys accumulate and keep costing money. Sweep them yourself after an apply run, or use `sentinel repair` to detect the resulting inconsistency. Tracked as issue #159.
:::

:::note Deletion works on Google Drive, since the fix for issue #169
Artifact deletion is implemented for `local`, `s3`, `gcs`, `azure` and `google-drive`. `preview` now warns up front when a job's storage type cannot be deleted from, so a policy can no longer look configured and previewed while enforcing nothing.

**On v1.4.0 and earlier a `google-drive` job failed at apply time** with `retention delete not supported for storage type 'google-drive'`, and `preview` gave no hint because it returns before reaching the storage layer. The backend had always implemented deletion; only the retention path lacked a case for it.
:::

:::note The all-jobs run reports each failure and exits non-zero, since the fix for issue #168
Without `--job`, per-job failures are listed individually on stderr under a line naming how many jobs with a policy failed, and the command exits 1.

**On v1.4.0 and earlier it exited 0**, printing only `retention completed with errors` and discarding the individual messages. A per-job run surfaced the error and exited 1 on the same failure, so testing with `--job` showed correct behaviour and hid the difference. On those versions, use `--job` in any automated context where the exit code matters.
:::

The all-jobs summary also prints `total deleted: N backups` in preview mode, where nothing was deleted. Read the mode from the command you typed, not from that line.

## Examples

Preview every job's candidates before writing a policy into a schedule:

```bash
sentinel retention preview --config sentinel.yaml
```

Prints one `<job>: deleted N backups` line per job with a policy, then a total. In preview mode these counts are candidate counts.

Preview one job and read the reason attached to each candidate:

```bash
sentinel retention preview --config sentinel.yaml --job prod-postgres
```

Each line is `- <path> (<bytes>) - <reason>`, where the reason is `exceeded keep_last`, `exceeded keep_days`, `not retained by gfs`, or a comma-joined combination.

Apply one job's policy for real:

```bash
sentinel retention apply --config sentinel.yaml --job prod-postgres
```

Deletes each candidate artifact and its history row, then prints the same per-candidate list. Exit 1 means a deletion failed, and the list shows what was deleted before the failure.

Apply with the flag that actually suppresses deletion:

```bash
sentinel retention apply --config sentinel.yaml --job prod-postgres --dry-run
```

Identical output to `preview`, and identically harmless. This is the only dry-run switch Sentinel honours.

Example policy, with a single flat rule so the intersection defect cannot bite:

```yaml
databases:
  prod-postgres:
    retention:
      keep_days: 30
      gfs:
        keep_weekly: 8
        keep_monthly: 12
```

A backup is kept when it is newer than 30 days, or when it anchors one of the last 8 ISO weeks or last 12 calendar months.

## Related

- [Retention: how Sentinel decides what to delete](../../concepts/retention.md)
- [Configuration reference](../configuration.md)
- [`sentinel backup`](./backup.md), which runs the automatic post-backup sweep
- [`sentinel schedule`](./schedule.md)
- [`sentinel monitor`](./monitor.md) for the history rows retention deletes
- [`sentinel repair`](./repair.md) for the inconsistencies orphaned sidecars create
- [CLI reference index](./index.md)

{/* sources: internal/cli/retention.go, internal/cli/retention_helpers.go, internal/cli/retention_cleaner.go, internal/cli/backup.go, internal/domain/retention/policy.go, internal/domain/retention/gfs.go, internal/domain/retention/types.go, internal/config/types.go, internal/cli/config_resolver.go */}
