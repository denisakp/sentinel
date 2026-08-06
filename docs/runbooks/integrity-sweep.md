# Runbook — Repository-wide integrity sweep (`backup verify --all` + scheduled)

> **Superseded by the documentation site: [guides/integrity-sweep](https://denisakp.github.io/sentinel/guides/integrity-sweep).**
>
> This runbook incomplete JSON field list and a trigger value that is never written. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-08-03
- **Related**: [verify-backup-integrity](./verify-backup-integrity.md), [failed-backup-triage](./failed-backup-triage.md), [enable-encryption](./enable-encryption.md)

## When to use

Auditing the integrity of an **entire** backup repository in one command — instead of
looking up each backup id and running `sentinel backup verify <id>` by hand. Typical uses:
a scheduled weekly/nightly integrity audit, a pre-restore confidence check, or triage after
suspected storage corruption.

## What it does

`backup verify --all` enumerates every recorded **successful** backup from the monitor
history, fetches each artifact from **its own** storage backend (local or remote — no local
copy is left behind), re-hashes it, and compares against the recorded integrity manifest. It
prints a per-backup report + a summary line and returns a single exit code. It is **read-only**:
the sweep writes nothing to the execution history.

```bash
# Sweep the whole repository
sentinel backup verify --all --config sentinel.yaml

# Scope to recent backups (day/week/hour units accepted)
sentinel backup verify --all --since 30d --config sentinel.yaml

# Scope to one job
sentinel backup verify --all --job prod-postgres --config sentinel.yaml

# Machine-readable output for an alerting pipeline
sentinel backup verify --all --output json --config sentinel.yaml
```

`--all` and a `<backup-id>` argument are mutually exclusive; supplying both, or neither, is a
usage error.

## The four states

Each backup is classified into exactly one of:

| Status             | Meaning                                                    | Remedy |
|--------------------|------------------------------------------------------------|--------|
| `ok`               | Artifact fetched and its hash matches the manifest.        | none |
| `corrupted`        | Artifact fetched but its hash no longer matches.           | re-take the backup; treat the artifact as untrusted |
| `missing_artifact` | Manifest recorded but the artifact object is gone.         | investigate storage / lifecycle rules; re-take |
| `missing_manifest` | Artifact present but no integrity manifest (unverifiable). | usually a pre-v1.1 backup; re-take under a current version, or accept with `--ignore-missing-manifest` |

The summary line reports a count per status:

```
12 checked · 10 ok · 1 corrupted · 0 missing_artifact · 1 missing_manifest
```

An empty repository (or no backups matching the filter) reports `0 checked` and exits `0`.

## Exit codes

The sweep returns a single exit code an automated system can branch on:

| Exit code | Meaning |
|-----------|---------|
| `0` | Every checked backup is `ok` (or the only issues are `missing_manifest` and `--ignore-missing-manifest` was passed). |
| `5` | **Integrity failure** — at least one `corrupted` / `missing_artifact`, or a `missing_manifest` without `--ignore-missing-manifest`. |
| `4` | **Operational error** — the check itself could not run (invalid config, unreadable history DB, or an unreachable storage backend). |

The integrity code (`5`) is deliberately distinct from the operational code (`4`) so an alert
can tell "backups are broken" apart from "the check couldn't run". `--ignore-missing-manifest`
downgrades `missing_manifest` to a warning (does not, by itself, cause a non-zero exit) — use it
for repositories that legitimately contain pre-integrity-manifest backups.

## Alerting cron example

Run a nightly sweep and page only on a real integrity failure, while still surfacing
operational problems:

```bash
#!/usr/bin/env bash
# /etc/cron.daily/sentinel-integrity-sweep
set -o pipefail

out=$(sentinel backup verify --all --since 30d --output json --config /etc/sentinel/config.yaml)
code=$?

case "$code" in
  0) exit 0 ;;                                   # all good
  5) echo "$out" | alert-page "Sentinel: backup integrity FAILURE"; exit 0 ;;
  *) echo "$out" | alert-warn "Sentinel: integrity sweep could not run (exit $code)"; exit 0 ;;
esac
```

`--output json` emits a `results` array (one object per backup: `backup_id`, `job`, `status`,
`hash_match`, `stored_hash`, `computed_hash`, `timestamp`, and `error` for operational rows)
plus a `summary` object with the per-status counts.

## Scheduling the sweep (`integrity.scheduled_check`)

Instead of wiring the cron yourself (the shell example above), Sentinel can run the **same**
sweep automatically on a cron, record every run's per-artifact results durably, and notify on
failure. Add an `integrity.scheduled_check` block and run `sentinel schedule start` — the
sweep is registered alongside your backup/restore jobs as a reserved `__integrity_check` job
(it inherits the same skip-if-already-running and crash-isolation behaviour):

```yaml
integrity:
  algorithm: sha256          # optional; sha256 is the default
  scheduled_check:
    enabled: true
    cron: "0 3 * * 0"        # 5-field cron — every Sunday 03:00
    since: 30d               # optional recency window (d/w/h units); omit to sweep everything
    job: ""                  # optional — restrict to one backup job; empty = all jobs
    notify_on: failure       # failure (default) | always | never
```

| Field       | Meaning |
|-------------|---------|
| `enabled`   | Turns the scheduled sweep on (default off — opt-in). |
| `cron`      | 5-field cron expression; **required** when `enabled`. Validated at config load. |
| `since`     | Optional recency window (e.g. `30d`, `4w`, `720h`). Restricts the sweep to backups newer than the window — a runtime-vs-coverage trade-off (a short window is faster but skips older artifacts). Omit to sweep the whole repository. |
| `job`       | Optional single backup-job scope; empty sweeps every job. |
| `notify_on` | `failure` (default) pages only when a non-`ok` result is found; `always` also confirms clean runs (positive assurance); `never` stays silent. |

Notifications reuse your existing channels (`defaults.notifications` — the same Slack / Discord /
email / webhook wiring backups use). On a failing sweep a **failure** notification is sent; under
`always`, a clean run sends a **success** confirmation. Notification delivery is **best-effort**:
a delivery failure is logged as a warning and never crashes the scheduler nor discards the
recorded run.

The reserved name `__integrity_check` may not be used by a user backup/restore job — config
validation rejects the collision.

`sentinel schedule list` shows the registered sweep with type `integrity`.

## Reading the audit trail (`integrity_checks`)

Each scheduled sweep writes **one row per verified artifact**, grouped by a shared `run_id` and
labelled with what triggered it (`scheduled` vs `manual`), into the `integrity_checks` table in
the monitor history DB (`history_db_path`, default `~/.sentinel/history.db`). This is the durable
forensic record answering "when did this artifact go bad?". The table lands via monitor schema
migration `005` (schema version 5) — an existing history DB upgrades in place on first open, and a
newer DB is refused by an older binary with a clear forward-incompatibility error.

Columns: `run_id`, `backup_id`, `job_name`, `result` (one of `ok` / `corrupted` /
`missing_artifact` / `missing_manifest`), `stored_hash`, `computed_hash`, `storage_backend`,
`artifact_path`, `checked_at`, `trigger`. Inspect it directly with any SQLite client:

```bash
# The most recent run's verdicts
sqlite3 ~/.sentinel/history.db \
  "SELECT checked_at, job_name, backup_id, result FROM integrity_checks
   WHERE run_id = (SELECT run_id FROM integrity_checks ORDER BY checked_at DESC LIMIT 1)
   ORDER BY job_name;"

# Every non-ok verdict ever recorded (the rot log)
sqlite3 ~/.sentinel/history.db \
  "SELECT checked_at, trigger, job_name, backup_id, result FROM integrity_checks
   WHERE result <> 'ok' ORDER BY checked_at DESC;"
```

The store rejects any `result` outside the four states at write time, so the audit vocabulary is
guaranteed consistent.

## Notes

- **Enumeration source**: the sweep iterates the execution history, so it verifies exactly the
  backups Sentinel recorded. It does not discover storage-only orphans the history never
  recorded (enumerating the backend directly is a later enhancement).
- **Heterogeneous backends**: each backup is verified against *its own* configured backend, so a
  repository split across multiple buckets/backends is handled transparently.
- **Sequential in v1**: large repositories re-fetch and re-hash many artifacts one at a time; a
  slow sweep is expected. Bounded parallelism is a later optimization.
- **Remote coverage**: the remote-fetch path (download artifact + sidecar, verify, delete) is
  unit-covered with an in-memory backend; the local path is additionally exercised end-to-end in
  `scripts/e2e.sh` (`test_verify_all`).
- **Read-only (manual command)**: `backup verify --all` itself never writes to history. Durable
  recording of results (the `integrity_checks` table) is done by the **scheduled** sweep — see
  "Scheduling the sweep" above (spec 052 / PRD 35).

## References

- `internal/cli/backup_verify.go` — `handleVerifyAll` / `runVerifySweep` / `verifyExecution`
- `internal/cli/integrity_scheduled.go` — `runScheduledIntegrityCheck` (record + notify)
- `internal/cli/schedule.go` — `__integrity_check` registration loop
- `internal/adapters/monitor/migrations/005_add_integrity_checks.sql` — the audit table
- `internal/cli/exit_codes.go` — `ErrVerifyIntegrityFailed` (5) vs `ErrVerifyInternal` (4)
- Spec 051 / PRD 34 (manual sweep) · Spec 052 / PRD 35 (scheduled sweep + audit trail)
