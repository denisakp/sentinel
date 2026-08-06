# Runbook — Upgrade Sentinel binary

> **Superseded by the documentation site: [guides/upgrade-sentinel-binary](https://denisakp.github.io/sentinel/guides/upgrade-sentinel-binary).**
>
> This runbook claims migrations apply automatically and that startup reaps stale locks; neither happens. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [db-migration-status](./db-migration-status.md), [start-scheduler](./start-scheduler.md), [scheduler-crash-recovery](./scheduler-crash-recovery.md)

## When to use

Releasing a new Sentinel binary onto a host running the scheduler.

## Preconditions

- New binary downloaded and `chmod +x`.
- Operator access to the host.
- A maintenance window long enough to drain in-flight jobs.

## Steps

### 1. Snapshot the monitor DB

```bash
cp ~/.sentinel/history.db ~/.sentinel/history.db.bak
```

`history.db` carries only execution metadata, but the backup makes migration rollback / forensics trivial.

### 2. Stop the running scheduler

Foreground: Ctrl-C the `sentinel schedule start` process. Background (systemd / supervisor):

```bash
systemctl stop sentinel-scheduler
# or
kill -TERM $(pgrep -f 'sentinel schedule start')
```

Wait for in-flight jobs to release their locks. `defer` ensures both semaphore slot and per-job file lock are released on graceful shutdown (ADR 0008).

### 3. Replace the binary

```bash
install -m 0755 ./sentinel-linux-amd64 /usr/local/bin/sentinel
sentinel version
```

### 4. Verify migration status

```bash
sentinel db migrate status --config /etc/sentinel/sentinel.yaml
```

If pending migrations exist, the next history-touching command auto-applies them. Migrations are atomic, idempotent, fail-fast (see [db-migration-status](./db-migration-status.md)).

### 5. Validate config compatibility

```bash
sentinel config validate --config /etc/sentinel/sentinel.yaml
```

A new release may add validation rules; catch breakage here, not at first scheduled tick.

### 6. Restart the scheduler

```bash
systemctl start sentinel-scheduler
# or run foreground
sentinel schedule start --config /etc/sentinel/sentinel.yaml
```

Startup reaps any stale locks (ADR 0007).

## Verification

```bash
sentinel version
sentinel db migrate status --config /etc/sentinel/sentinel.yaml
sentinel schedule list      --config /etc/sentinel/sentinel.yaml
sentinel monitor list       --config /etc/sentinel/sentinel.yaml --last 1h
```

A new `success` record at the next scheduled tick confirms recovery.

## Rollback / recovery

If the upgrade is bad:

```bash
install -m 0755 ./sentinel-linux-amd64.prev /usr/local/bin/sentinel
cp ~/.sentinel/history.db.bak ~/.sentinel/history.db   # only if migration cannot be replayed by old binary
systemctl start sentinel-scheduler
```

Migrations are forward-only; restore `history.db.bak` if the old binary cannot read the new schema. See [scheduler-crash-recovery](./scheduler-crash-recovery.md) if locks are left behind.

## References

- ADR 0007 — Lock file format v1 (`docs/adr/0007-lock-file-format.md`)
- ADR 0008 — Scheduler concurrency model (`docs/adr/0008-scheduler-concurrency-model.md`)
- [db-migration-status](./db-migration-status.md)
