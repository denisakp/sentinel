# Runbook — Apply retention

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [run-backup-from-config](./run-backup-from-config.md), [check-storage-backend](./check-storage-backend.md)

## When to use

Periodic cleanup of old backup artifacts. Manual application only; the scheduler also evaluates retention automatically after each successful scheduled run.

## Preconditions

- `retention:` block defined on the job or in `defaults:` (`keep_last`, `keep_days`).
- Read/delete access on the configured storage backend.

## Steps

### Configure retention

```yaml
defaults:
  storage:
    type: local
    local_path: /workspace/backups
  retention:
    keep_last: 5    # keep N most recent per job
    keep_days: 30   # keep only those from last 30 days
```

### Preview (dry-run) before deleting

```bash
sentinel retention preview --config /workspace/infra/dataset/local.yaml
```

Output lists each file that would be deleted and the rule that selected it (`keep_last` overrun or `keep_days` age).

### Apply

```bash
# All jobs
sentinel retention apply --config /workspace/infra/dataset/local.yaml

# Single job
sentinel retention apply --config /workspace/infra/dataset/local.yaml --job postgres-sample

# Preview via apply flag
sentinel retention apply --config /workspace/infra/dataset/local.yaml --dry-run
```

## Scheduler naming behavior (v1.0.6+)

Scheduler-driven runs treat `output:` as a prefix and append a UTC second timestamp:

- `postgres-dev_2026-03-15T02-00-00.sql`
- `postgres-dev_2026-03-15T02-00-00-1.sql` (same-second collision suffix)

Notes:

- One-shot `sentinel backup --config` naming is unchanged.
- A configured `output: postgres-dev.sql` is normalised before suffixing to avoid double extensions.
- After successful scheduled runs, retention is evaluated automatically when `keep_last`/`keep_days` are set.
- Retention failures are warning-level: they do **not** convert a successful backup into a failed result.

## Verification

```bash
ls -lh /workspace/backups/ | wc -l
sentinel storage status --config /workspace/infra/dataset/local.yaml --output text
```

Object count should drop to within configured bounds.

## Rollback / recovery

Deletes are not reversible from Sentinel. If you over-pruned: restore from offsite copy or accept the loss. Use `--dry-run` next time.

## References

- `internal/domain/retention/` — pure policy evaluation; CLI orchestration in `internal/cli/retention_helpers.go`
- [check-storage-backend](./check-storage-backend.md) — confirm backend health before/after retention
