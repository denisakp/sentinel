# Runbook — Failed backup triage

> **Superseded by the documentation site: [operations/failed-backup-triage](https://denisakp.github.io/sentinel/operations/failed-backup-triage).**
>
> This runbook lists statuses and error strings that the code never emits. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [inspect-monitor-history](./inspect-monitor-history.md), [troubleshooting](./troubleshooting.md), [stale-lock-recovery](./stale-lock-recovery.md), [check-storage-backend](./check-storage-backend.md), [enable-encryption](./enable-encryption.md)

## When to use

Alert fires for a `failure` event, or a routine `monitor list` shows new failures.

## Preconditions

- `history_db_path` set and readable.
- Access to the host running the scheduler (for env-var inspection).

## Steps

### 1. Enumerate recent failures

```bash
sentinel monitor list --config <config> --status failure --last 24h
sentinel monitor list --config <config> --status timeout --last 24h
sentinel monitor list --config <config> --status skipped --last 24h
```

Note the `ID`, `JOB`, and truncated `ERROR` columns.

### 2. Pull the full error

```bash
sentinel monitor show --config <config> --id <execution-id>
```

### 3. Classify

| Symptom                                              | Category               | Next                                           |
|------------------------------------------------------|------------------------|------------------------------------------------|
| `failed to ping database`, `connection refused`      | Connectivity           | [troubleshooting](./troubleshooting.md)        |
| `encryption key not set`, `key-resolution error`     | Encryption / key       | [enable-encryption](./enable-encryption.md)    |
| `lock: held by another holder`, `skipped`            | Lock contention        | [stale-lock-recovery](./stale-lock-recovery.md)|
| `bucket not found`, `403`, `404`, storage SDK errors | Storage backend        | [check-storage-backend](./check-storage-backend.md) |
| `file corrupt or wrong key`                          | Integrity / envelope   | [verify-backup-integrity](./verify-backup-integrity.md), [recover-legacy-envelope](./recover-legacy-envelope.md) |
| `exceeded chain depth`, `lineage gap`                | Incremental chain      | [chain-corruption-recovery](./chain-corruption-recovery.md) |
| `timeout`                                            | Slow source / network  | Tune `timeout_seconds`; investigate DB load    |

### 4. Replay the failed job

After applying a fix, re-run just the offending job:

```bash
sentinel backup --job <name> --config <config>
```

Then confirm:

```bash
sentinel monitor list --config <config> --job <name> --last 10m
```

### 5. Bulk replay (multiple jobs failed for the same reason)

Re-run them sequentially:

```bash
for j in postgres-prod mysql-prod mongo-prod; do
  sentinel backup --job "$j" --config <config> || echo "still failing: $j"
done
```

## Verification

A `success` row appears for each replayed job. `monitor list --status failure --last 1h` is empty.

## Rollback / recovery

For artifacts produced by a partially-successful retry: verify integrity per [verify-backup-integrity](./verify-backup-integrity.md) before relying on them.

## References

- [inspect-monitor-history](./inspect-monitor-history.md)
- `internal/adapters/monitor/`, `internal/sanitize/` (error formatting)
