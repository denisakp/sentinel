# Runbook — Troubleshooting

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [failed-backup-triage](./failed-backup-triage.md), [stale-lock-recovery](./stale-lock-recovery.md), [environment-setup](./environment-setup.md)

## When to use

You hit one of the common, well-known errors below. For systemic failure investigation start at [failed-backup-triage](./failed-backup-triage.md).

## Common errors

### `exec: "pg_dump": executable file not found in $PATH`

Host is missing the DB client. Install per [environment-setup](./environment-setup.md).

### `failed to ping database`

1. Check host/port — from inside a container use `host.docker.internal`, not `localhost`.
2. Verify the DB container is running: `docker ps`.
3. Test raw connectivity: `nc -zv host.docker.internal 5432`.

### `mysqldump: [Warning] Using a password on the command line interface can be insecure`

Warning, not an error. Backup succeeds. Credentials in this project are passed via `password_env` in YAML.

### Backup file created but empty (0 bytes)

For PostgreSQL plain format (`pg-out-format: p`), Sentinel captures `pg_dump` stdout and writes via `WriteBackup`. An empty DB still produces a non-zero header. A truly 0-byte file means `pg_dump` exited with an error — set `log_format: text` in config to surface stderr.

### `error: scheduler not running` on `schedule stop`

`schedule stop` only works inside the same process session as `schedule start`. For background runs use a process manager (systemd, supervisor) or Ctrl-C the foreground process. See [start-scheduler](./start-scheduler.md).

### Monitor shows no records

Ensure `history_db_path` is set in YAML and the path is writable. Records are only written for config-driven runs (`sentinel ... --config ...`). See [inspect-monitor-history](./inspect-monitor-history.md).

### `configuration file already has an encryption key set`

`sentinel security init-key` fails when `SENTINEL_MASTER_KEY` is already set in the environment. Use `--force` to regenerate:

```bash
sentinel security init-key --force
```

Audit which schedules rely on the existing key before forcing — see [key-rotation](./key-rotation.md) and [key-loss-incident](./key-loss-incident.md).

### Lock conflict / `skipped` in monitor

A lock is held by another holder. See [stale-lock-recovery](./stale-lock-recovery.md).

### Decrypt fails with `file corrupt or wrong key`

Either the key is wrong, the artifact is a legacy envelope, or the file is truncated/tampered. Check envelope version per [recover-legacy-envelope](./recover-legacy-envelope.md); verify integrity per [verify-backup-integrity](./verify-backup-integrity.md).

## Verification

After applying a fix, re-run the offending command and check `sentinel monitor list --last 1h` for a `success` record.

## Rollback / recovery

Per individual error above.

## References

- `internal/cli/`, `internal/sanitize/` (error formatting)
- [failed-backup-triage](./failed-backup-triage.md)
