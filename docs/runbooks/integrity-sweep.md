# Runbook — Repository-wide integrity sweep (`backup verify --all`)

- **Audience**: ops / SRE
- **Last reviewed**: 2026-08-02
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
- **Read-only**: persisting sweep results to history (`--record` + an `integrity_checks` table)
  is owned by the sibling scheduled-integrity feature (PRD 35), which this command unblocks.

## References

- `internal/cli/backup_verify.go` — `handleVerifyAll` / `verifyExecution`
- `internal/cli/exit_codes.go` — `ErrVerifyIntegrityFailed` (5) vs `ErrVerifyInternal` (4)
- Spec 051 / PRD 34
