---
title: Sweeping a repository for corruption
description: Verify every recorded backup in one command, branch on the exit code, and schedule the same sweep with a durable audit trail.
sidebar_position: 16
---

:::info Added in v1.3.0
`sentinel backup verify --all`, the repository-wide integrity sweep, requires Sentinel v1.3.0 or later. Earlier releases verify one backup at a time.
:::

Check every backup Sentinel has recorded against its manifest in one pass, and get a single exit code
an alerting system can act on.

## When to use this

Use this as a scheduled audit, as a confidence check before a large restore, and as triage after a
storage incident that could have touched more than one artifact. It answers "is anything in the
repository rotten", which no per-backup command can answer without a loop.

Use [Verifying one backup](./verify-backup-integrity.md) when you already know which artifact you
care about; the sweep re-downloads and re-hashes everything, which on a remote backend costs real
egress and real time.

The sweep does not discover artifacts the history never recorded. It iterates the execution history,
so a file sitting in a bucket that Sentinel did not write, or wrote before this history database
existed, is invisible to it. For that kind of drift, see the
[`sentinel repair` reference](../reference/cli/repair.md).

## Before you start

- A history database with recorded executions, confirmed with
  `sentinel monitor doctor --config sentinel.yaml`. See
  [Inspecting execution history](./inspect-monitor-history.md).
- Storage credentials for every backend that appears in the history, resolved from the job entries in
  your current configuration. Each artifact is fetched from its own backend, so a repository split
  across several buckets is handled transparently, but a job that has been renamed or removed from
  the configuration takes its credentials with it.
- Time and egress budget. The sweep is sequential in this release: artifacts are fetched and hashed
  one at a time, and a large remote repository will be slow.

The sweep is read-only. `backup verify --all` writes nothing to the history database. Only the
scheduled variant described below records anything.

## Steps

### 1. Run the sweep

```bash
sentinel backup verify --all --config sentinel.yaml --output text
```

```
ID            JOB   STATUS            HASH_MATCH  TIMESTAMP
exec-ok       demo  ok                true        2026-08-05T17:48:18Z
exec-bad      demo  corrupted         false       2026-08-05T16:48:18Z
exec-nm       demo  missing_manifest  -           2026-08-05T15:48:18Z
exec-gone     demo  missing_artifact  -           2026-08-05T14:48:18Z
4 checked · 1 ok · 1 corrupted · 1 missing_artifact · 1 missing_manifest
```

Only executions recorded as successful are enumerated; failed runs produced nothing to verify. A
positional backup ID and `--all` are mutually exclusive, and supplying neither is a usage error.

`--output text` is worth passing explicitly, because with no `--output` the format falls back to the
configuration's `log_format`, which defaults to `json`.

### 2. Understand the four states

| Status | Meaning | What to do |
|---|---|---|
| `ok` | Artifact fetched, hash matches the manifest. | Nothing. |
| `corrupted` | Artifact fetched, hash no longer matches. | Treat as untrusted, re-take from source, keep the file for forensics. |
| `missing_artifact` | Manifest present, artifact object gone. | Check storage lifecycle rules and retention before assuming an accident. |
| `missing_manifest` | Artifact present, no manifest, so nothing to compare against. | Almost always the missing `output:` defect below, not an old backup. |

A fifth outcome exists that is not one of the four. A row rendered as `error` is an operational
failure, such as a backend that would not initialise, and it is counted separately on a second
summary line rather than in the counts above.

### 3. Scope the sweep

```bash
# Recent backups only
sentinel backup verify --all --since 30d --config sentinel.yaml --output text

# One job
sentinel backup verify --all --job prod-postgres --config sentinel.yaml --output text

# Accept a repository that legitimately predates manifests
sentinel backup verify --all --ignore-missing-manifest --config sentinel.yaml --output text
```

`--since` accepts a whole number of days or weeks (`30d`, `4w`) and any Go duration (`720h`, `90m`).
It compares against the recorded execution timestamp, so it bounds runtime and egress in exactly the
way you would expect.

:::caution A scope that matches nothing looks identical to a clean repository
`--job` with a typo, or a `--since` window shorter than your backup interval, prints
`0 checked · 0 ok · …` and exits `0`. Nothing warns you that the filter excluded everything. Always
read the `checked` count, and in automation assert that it is above a floor you expect.
:::

### 4. Branch on the exit code

| Exit | Meaning |
|---|---|
| `0` | Every checked backup is `ok`, or the only issues were `missing_manifest` with `--ignore-missing-manifest`. |
| `5` | Integrity failure. At least one `corrupted` or `missing_artifact`, or a `missing_manifest` without `--ignore-missing-manifest`. |
| `4` | Operational failure. The check itself could not run, or at least one artifact could not be fetched for a reason other than absence. |

The separation of `5` from `4` is the point: an alert can tell "the backups are broken" from "the
checker is broken". Integrity wins when both occur, so a `5` may be hiding an operational failure as
well; read the second summary line for the operational count.

```bash
#!/usr/bin/env bash
# /etc/cron.daily/sentinel-integrity-sweep
set -o pipefail

out=$(sentinel backup verify --all --since 30d --output json --config /etc/sentinel/config.yaml)
code=$?

case "$code" in
  0) exit 0 ;;
  5) echo "$out" | alert-page "Sentinel: backup integrity FAILURE"; exit 0 ;;
  *) echo "$out" | alert-warn "Sentinel: integrity sweep could not run (exit $code)"; exit 0 ;;
esac
```

`--output json` emits a `results` array and a `summary` object, both on stdout. Each result carries
`backup_id`, `job`, `status`, `hash_match`, `stored_hash`, `computed_hash`, `hash_algorithm`,
`size_bytes`, `timestamp`, `path`, and `error` on operational rows. The summary carries `checked`,
`ok`, `corrupted`, `missing_artifact`, `missing_manifest`, and `errored`.

### 5. Let Sentinel run the sweep instead of cron

The same sweep can be registered as a scheduled job, which additionally records every run durably and
notifies on failure. Add an `integrity.scheduled_check` block and start the scheduler.

```yaml
integrity:
  algorithm: sha256          # optional; sha256 is the only accepted value
  scheduled_check:
    enabled: true
    cron: "0 3 * * 0"        # 5-field cron, every Sunday at 03:00
    since: 30d               # optional recency window; omit to sweep everything
    job: ""                  # optional single-job scope; empty sweeps all jobs
    notify_on: failure       # failure (default) | always | never
```

| Field | Meaning |
|---|---|
| `enabled` | Turns the scheduled sweep on. Off by default. |
| `cron` | 5-field cron expression, required when enabled, validated at config load. |
| `since` | Recency window, same grammar as `--since`. Validated at config load. |
| `job` | Restrict to one backup job. Empty sweeps every job. |
| `notify_on` | `failure` pages only on a non-`ok` result; `always` also confirms clean runs; `never` stays silent. |

```bash
sentinel schedule start --config sentinel.yaml
```

The sweep registers under the reserved job name `__integrity_check` and inherits the scheduler's
skip-if-already-running and crash-isolation behaviour. A user backup or restore job of that exact
name is rejected at config validation, whether or not the scheduled check is enabled.

```bash
sentinel schedule list --config sentinel.yaml 2>&1
```

```
TYPE       NAME               SCHEDULE   NEXT EXECUTION
backup     demo               0 2 * * *  2026-08-06T02:00:00Z
integrity  __integrity_check  0 3 * * 0  2026-08-09T03:00:00Z
```

The `2>&1` is needed here too: `schedule list` prints to stderr, like the `monitor` subcommands.

Notifications reuse the channels under `defaults.notifications`. Delivery is best effort: a failed
send is logged as a warning and never crashes the scheduler or discards the recorded run. Setting up
those channels is covered in [Setting up alerting](./alerting-setup.md).

### 6. Read the audit trail

Each scheduled run writes one row per verified artifact into the `integrity_checks` table of the
history database, grouped by a shared `run_id`. This is the durable record that answers "when did
this artifact go bad". It arrives with monitor schema migration `005`; an existing history database
upgrades in place on first open.

Columns are `run_id`, `backup_id`, `job_name`, `result`, `stored_hash`, `computed_hash`,
`storage_backend`, `artifact_path`, `checked_at`, and `trigger`. The store rejects any `result`
outside the four states at write time, so the vocabulary stays consistent.

```bash
# The most recent run's verdicts
sqlite3 ~/.sentinel/history.db \
  "SELECT checked_at, job_name, backup_id, result FROM integrity_checks
   WHERE run_id = (SELECT run_id FROM integrity_checks ORDER BY checked_at DESC LIMIT 1)
   ORDER BY job_name;"

# Every non-ok verdict ever recorded
sqlite3 ~/.sentinel/history.db \
  "SELECT checked_at, trigger, job_name, backup_id, result FROM integrity_checks
   WHERE result <> 'ok' ORDER BY checked_at DESC;"
```

Two things about this table are not what the column names suggest. `trigger` can hold `manual` or
`scheduled`, but only the scheduled runner ever writes rows, so in practice every row reads
`scheduled`; the manual `backup verify --all` records nothing. And a sweep that verified nothing,
because `since` excluded everything or `job` matched no records, writes no rows at all rather than an
empty run marker, so an over-tight scope leaves no evidence that the sweep ran.

## Verify

For a manual sweep, the exit code is the result:

```bash
sentinel backup verify --all --config sentinel.yaml --output text
echo "exit=$?"
```

Check that `checked` is the number of successful executions you expect. Compare it against
`sentinel monitor list --config sentinel.yaml --last 30d --status success 2>&1 | wc -l`.

For the scheduled sweep, confirm registration and then confirm a run landed:

```bash
sentinel schedule list --config sentinel.yaml 2>&1
sqlite3 ~/.sentinel/history.db \
  "SELECT trigger, COUNT(DISTINCT run_id), MAX(checked_at) FROM integrity_checks;"
```

A row with type `integrity` in the listing, and a growing `run_id` count with a recent `checked_at`,
means the sweep is running and recording. A `notify_on: failure` sweep is silent on clean runs by
design, so silence is not evidence that it ran; the table is.

## If it goes wrong

**Every backup comes back `missing_manifest`.** This is usually not a repository of old backups. A
backup job whose configuration omits `output:` never gets a manifest written at all, and the history
row records its artifact path as `unknown`. Set `output:` on every job and re-run; artifacts already
written cannot be given a manifest retroactively. Reaching for `--ignore-missing-manifest` here turns
the alarm off without fixing anything. Tracked as issue #151.

**`0 checked` and exit 0.** A filter matched nothing. Re-run without `--job` and without `--since`
before concluding the repository is clean.

**Exit 4 with no per-row detail.** The sweep could not enumerate the history, or the configuration
would not load. Nothing has been proven about your backups either way; this is not an all-clear.

**One job's rows all report `error`.** Its storage credentials could not be resolved. The backend
configuration is read from the job entry that matches the recorded job name, so a renamed or deleted
job orphans its own artifacts as far as verification is concerned.

**The sweep is too slow to finish inside its window.** Bound it with `--since`, or scope it per job
and stagger the crons. Parallelism is not available in this release.

## Related

- [Verifying one backup](./verify-backup-integrity.md): the single-artifact form, and the exit codes it uses instead.
- [Inspecting execution history](./inspect-monitor-history.md): what the sweep enumerates.
- [Manifests and integrity](../concepts/manifest.md): what is being compared, and why no key is needed.
- [Setting up alerting](./alerting-setup.md): the notification channels the scheduled sweep reuses.
- [Applying a retention policy](./apply-retention.md): the other thing that removes artifacts.
- [`sentinel backup` reference](../reference/cli/backup.md): every `verify --all` flag.
- [`sentinel schedule` reference](../reference/cli/schedule.md): the scheduler that runs the reserved job.
- [Configuration reference](../reference/configuration.md): the full `integrity:` block.

<!-- sources: internal/cli/backup_verify.go, internal/cli/integrity_scheduled.go, internal/cli/schedule.go, internal/cli/verify_since.go, internal/cli/exit_codes.go, internal/config/since.go, internal/config/types.go, internal/config/validator.go, internal/adapters/monitor/recorder.go, internal/adapters/monitor/migrations/005_add_integrity_checks.sql, internal/domain/schedule/next_run.go, docs/runbooks/integrity-sweep.md -->
