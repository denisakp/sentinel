# Runbook — Chain corruption recovery

> **Superseded by the documentation site: [operations/chain-corruption-recovery](https://denisakp.github.io/sentinel/operations/chain-corruption-recovery).**
>
> This runbook centres on three subcommands that cannot be invoked at all. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [restore-pitr-and-incremental](./restore-pitr-and-incremental.md), [apply-retention](./apply-retention.md), [verify-backup-integrity](./verify-backup-integrity.md)

## When to use

`sentinel restore validate-chain` fails, `restore run` reports missing lineage, or `backup chain-status` shows a broken / truncated chain.

## Preconditions

- Source DB still reachable for a fresh base.
- Storage backend writable.

## Steps

### Detect

```bash
sentinel backup chain-status --config /workspace/infra/dataset/local.yaml
sentinel backup chain-list   --config /workspace/infra/dataset/local.yaml
sentinel restore validate-chain <restore-job> --config /workspace/infra/dataset/local.yaml
```

Look for missing intermediate artifacts, lineage gaps, or `max_chain_depth` exhaustion.

### Force a new full base

```bash
sentinel backup force-full --job <name> --config /workspace/infra/dataset/local.yaml
```

This produces a fresh full backup outside the existing chain. Subsequent incrementals will lineage off the new base.

### Protect the new base from retention

Update `retention:` so the new full is not pruned before downstream incrementals exist:

```yaml
databases:
  <name>:
    # ...
    retention:
      keep_last: 5     # >= chain depth + 1 buffer
      keep_days: 30    # >= longest expected recovery window
```

Re-validate:

```bash
sentinel retention preview --config /workspace/infra/dataset/local.yaml --job <name>
sentinel backup chain-status --config /workspace/infra/dataset/local.yaml
```

### Cull broken artifacts

Once the new chain is healthy, the old broken artifacts are dead weight. Either:

```bash
# Preview, then apply
sentinel retention preview --config /workspace/infra/dataset/local.yaml --job <name>
sentinel retention apply   --config /workspace/infra/dataset/local.yaml --job <name>
```

Or remove them manually from the storage backend after confirming they are not referenced by any active restore job.

## Verification

```bash
sentinel restore validate-chain <restore-job> --config /workspace/infra/dataset/local.yaml
sentinel restore dry-run        <restore-job> --config /workspace/infra/dataset/local.yaml
sentinel backup chain-status    --config /workspace/infra/dataset/local.yaml
```

All three should pass. For PITR/incremental specifics see [restore-pitr-and-incremental](./restore-pitr-and-incremental.md).

## Rollback / recovery

Not applicable — this runbook is itself the recovery path. If the new base also fails to capture, investigate connectivity and `mysql.binlog_path` / WAL availability per [troubleshooting](./troubleshooting.md).

## References

- `internal/adapters/restore/incremental/mysqlbinlog/`, `internal/adapters/restore/incremental/pgcombine/`
- `internal/cli/backup.go` (chain subcommands)
