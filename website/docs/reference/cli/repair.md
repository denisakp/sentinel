---
title: sentinel repair
description: Reference for sentinel repair, the repository-wide state reconciliation command, with every flag and drift class.
sidebar_position: 11
---

Reconciles the three places Sentinel records state, monitor rows, manifest sidecars, and storage artifacts, plus the job lock directory, and on explicit opt-in applies the fixes that are safe to automate.

## Synopsis

```text
sentinel repair [flags]
```

`repair` takes no positional arguments. It is report-only unless `--fix` or `--purge-orphans` is passed.

This is not [`sentinel monitor doctor --repair`](./monitor.md), which only migrates the history database's schema and touches one file. `repair` walks every enabled job's storage backend and every monitor row.

Before doing anything, `repair` diagnoses the monitor schema. If the status is not `current` it refuses to run and exits with the corresponding `monitor doctor` code: 1 for `stale-pending`, 2 for `forward-incompatible`, 3 for `missing`, 4 for `corrupt`. Fix the schema first, then re-run.

## Subcommands

`repair` has no subcommands. It is a single top-level command.

| Subcommand | Purpose |
|---|---|
| n/a | None. All behaviour is selected by the flags below. |

## Flags

### `sentinel repair`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | n/a | Path to the YAML configuration file. Supplies `history_db_path`, the per-job storage blocks, `scheduler.lock_dir`, and `scheduler.stale_lock_threshold`. |
| `--dry-run` | bool | `false` | Force report-only. Detects everything, mutates nothing. Overrides `--fix` and `--purge-orphans` when combined with them. |
| `--fix` | bool | `false` | Apply the recoverable fixes: finalise stale running rows and remove stale locks. |
| `--format` | string | `text` | Output format: `text` or `json`. Any value other than `json`, case-insensitively, renders as text. |
| `-h`, `--help` | bool | `false` | Print help for `repair`. |
| `--job` | string | n/a | Restrict reconciliation to one named backup job. An unknown name matches nothing and reports no drift rather than failing. |
| `--purge-orphans` | bool | `false` | Additionally delete orphan artifacts. Implies `--fix`. Prompts for confirmation unless `--yes`. |
| `--yes` | bool | `false` | Skip the interactive confirmation for `--purge-orphans`. Has no effect on its own. |

### What each mode reconciles

The three action flags are not independent settings; they resolve to one of three postures.

| Invocation | Posture | Effect |
|---|---|---|
| no action flags, or `--dry-run` | report-only | Every drift class is detected and printed. Nothing on disk, in storage, or in the database changes. The footer reads `Nothing was modified (report-only; use --fix / --purge-orphans to apply).` |
| `--fix` | recoverable | Stale running rows are finalised to `interrupted` and removable lock files are deleted. No artifact is ever deleted in this mode. |
| `--purge-orphans` | recoverable plus purge | Everything `--fix` does, plus deletion of orphan artifacts from the storage backend. |

`--dry-run` always wins: `--dry-run --purge-orphans --yes` detects the orphans, labels them `report (use --purge-orphans to delete)`, and deletes nothing.

:::danger `--purge-orphans` permanently deletes artifacts
`--purge-orphans` calls the storage backend's delete for each orphan artifact. On S3, GCS, Azure, or Google Drive this is a remote object deletion with no local copy left behind, and Sentinel does not stage or archive the bytes first. Run `sentinel repair --config sentinel.yaml --dry-run` and read every `orphan_artifact` line before adding the flag. `--yes` removes the last confirmation prompt: use it only in automation whose input you have already reviewed.

An orphan is by definition an artifact that no monitor row and no manifest sidecar refers to, so purging one cannot break a restore that Sentinel knows about, but it will discard a backup that was taken outside Sentinel or whose history row was lost. Confirm you have another copy before purging.
:::

### Drift classes

Every finding carries one of these class names, in both text and JSON output.

| Class | Detected when | `--fix` | `--purge-orphans` |
|---|---|---|---|
| `orphan_artifact` | An object in the repository has neither a monitor row nor a `.manifest.json` sidecar. | Reported only. | Deleted, subject to confirmation and baseline protection. |
| `untracked_artifact` | An object has a manifest sidecar but no monitor row. | Reported only. | Reported only; never auto-purged. |
| `orphan_manifest` | A `.manifest.json` sidecar exists but the artifact it names is absent from the listing. | Reported only; the sidecar is kept. | Reported only; the sidecar is kept. |
| `artifact_missing` | A successful monitor row names an artifact that is absent from storage. | Manual action required. | Manual action required. |
| `stale_running` | A row is still `running` or `in-progress` and no live lock is held for the job. | Finalised to `interrupted`. | Same as `--fix`. |
| `stale_lock` | A lock file whose PID is dead and whose age exceeds the stale threshold. | Removed. | Same as `--fix`. |
| `chain_broken` | An incremental chain fails the ordered-chain resolver or the manifest lineage contract. | Manual action required. | Manual action required. |

Two safety rules constrain the mutating classes. A `stale_running` row whose lock names a different hostname is skipped as `skipped (foreign-host lock)`, because the other host may still be running the job; a row whose lock is live is skipped as `skipped (live lock)`. A `stale_lock` owned by another hostname is never touched. The stale threshold comes from `scheduler.stale_lock_threshold` in minutes and defaults to 60; the lock directory comes from `scheduler.lock_dir` and defaults to `/var/run/sentinel`.

Orphan purge additionally runs the retention module's active-baseline protection. An orphan whose file name matches the baseline of the active incremental chain is kept and labelled `kept (protected active baseline)`, so purging cannot decapitate a chain that later backups depend on.

`chain_broken` detection is structural only: `repair` reads each chain member's manifest for lineage fields and runs the same resolver as `restore validate-chain`, but it does not re-hash artifacts. Use [`sentinel backup verify`](./backup.md) for hash verification.

:::caution `--fix` does not mark broken chains, despite its help text
The `--fix` help string reads `Apply recoverable fixes: finalize stale running rows, remove stale locks, mark broken chains`, and the command's long description repeats the claim. In practice `chain_broken` findings are only ever emitted with the action `manual action required (blocks incremental restore)`; no chain state is written under any flag combination. The two classes `--fix` actually mutates are `stale_running` and `stale_lock`.
:::

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Reconciliation completed and no `artifact_missing` or `chain_broken` finding remains. |
| 1 | Monitor schema is `stale-pending`; run `sentinel monitor doctor --repair` first. |
| 2 | Monitor schema is `forward-incompatible` with this binary. |
| 3 | Monitor history database is missing. |
| 4 | Monitor database is corrupt, or an operational failure prevented reconciliation, such as a config or backend error. |
| 5 | Manual-action inconsistencies remain: at least one `artifact_missing` or `chain_broken` finding. |

Exit 5 is returned even in report-only mode and even when fixes were applied successfully, which makes `sentinel repair --dry-run` usable directly as a cron or CI health gate.

### Output

Text output groups findings under a `Repository:` heading per distinct storage backend identity, with findings that have no repository, such as a lock-listing failure, collected under `General:`. Each line reads `<class>  <path and detail>  → <action>`. A count summary and a modification footer close the report.

JSON output is a single object with `mode` (`report_only`, `fix`, `purge`), a `findings` array, a `summary` object counting each class plus `applied`, and a top-level `modified` boolean. Each finding carries `class`, `job`, `repository`, `path`, `detail`, `action`, `size_bytes`, and `applied`.

Both formats go to stdout.

## Examples

Detect drift across every enabled job, changing nothing:

```bash
sentinel repair --config sentinel.yaml --dry-run
```

Prints one line per finding. `No drift detected.` means the repository, the history database, and the lock directory agree.

Scope the sweep to one job on a shared bucket:

```bash
sentinel repair --config sentinel.yaml --job prod-postgres --dry-run
```

Artifacts are still matched against the monitor rows of every job, so another job's backups in the same bucket are not misreported as orphans.

Gate a nightly cron on repository consistency:

```bash
sentinel repair --config sentinel.yaml --dry-run --format json > /var/log/sentinel/repair.json
```

Exit 5 means an `artifact_missing` or `chain_broken` finding needs a human.

Clean up after an unclean shutdown, finalising interrupted rows and clearing dead locks:

```bash
sentinel repair --config sentinel.yaml --fix
```

Applied lines read `marked interrupted` and `removed`, and the footer counts the changes. No artifact is deleted.

Review orphan artifacts, then delete them interactively:

```bash
sentinel repair --config sentinel.yaml --dry-run
sentinel repair --config sentinel.yaml --purge-orphans
```

The second command prompts `Delete N orphan artifact(s) (B bytes)? [y/N]`. Answering anything other than `y` or `yes` leaves every artifact in place and labels them `kept (not confirmed)`.

Purge without a prompt, in automation whose dry-run output has already been reviewed:

```bash
sentinel repair --config sentinel.yaml --purge-orphans --yes
```

## Related

- [`sentinel monitor`](./monitor.md)
- [`sentinel backup`](./backup.md)
- [`sentinel retention`](./retention.md)
- [`sentinel restore`](./restore.md)
- [Concurrency and job locking](../../concepts/locking.md)
- [The backup manifest](../../concepts/manifest.md)
- [Configuration reference](../configuration.md)
- [CLI reference index](./index.md)

<!-- sources: internal/cli/repair.go, internal/cli/exit_codes.go, internal/cli/monitor_doctor.go, internal/adapters/monitor/doctor.go, internal/adapters/lock/lock.go, internal/adapters/lock/state.go, internal/domain/retention/policy.go, internal/config/types.go, internal/config/loader.go, docs/runbooks/state-repair.md, docs/runbooks/stale-lock-recovery.md, docs/runbooks/chain-corruption-recovery.md -->
