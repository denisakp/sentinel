# Runbook — Key rotation

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [enable-encryption](./enable-encryption.md), [key-loss-incident](./key-loss-incident.md), [recover-legacy-envelope](./recover-legacy-envelope.md), ADR 0006

## When to use

Periodic key rotation policy, suspected key compromise, or operator handover. Goal: rotate the active key without losing access to existing artifacts.

## Preconditions

- Secret store / env injection mechanism that allows running with **multiple env vars** simultaneously (`SENTINEL_MASTER_KEY` for new, e.g. `SENTINEL_MASTER_KEY_OLD` for legacy).
- An accurate map of which schedules / restore jobs reference the current key.

## Steps

### 1. Keep the old key reachable

Before generating a new key, snapshot the current value into a separate, restore-only env var:

```bash
export SENTINEL_MASTER_KEY_OLD=$SENTINEL_MASTER_KEY
```

Store `SENTINEL_MASTER_KEY_OLD` in your secret store. Restore jobs reading old artifacts will reference `encryption_key_env: SENTINEL_MASTER_KEY_OLD` until those artifacts are aged out.

### 2. Generate the new key

```bash
sentinel security init-key --force
```

`--force` is required because `SENTINEL_MASTER_KEY` is already set. The new key is printed once — store it immediately.

```bash
export SENTINEL_MASTER_KEY=<new-key>
```

### 3. New backups use the new key automatically

Every `sentinel backup --config` and scheduled run from this point encrypts with the new key. No config change needed for jobs that already use `encryption_key_env: SENTINEL_MASTER_KEY`.

### 4. Restoring old artifacts

For restore jobs pointing at pre-rotation artifacts:

```yaml
restores:
  legacy-restore:
    type: postgres
    enabled: true
    # ...
    backup_source:
      type: local
      backup_path: /workspace/backups/postgres-dev_pre-rotation.enc
    encryption_key_env: SENTINEL_MASTER_KEY_OLD
```

Then:

```bash
export SENTINEL_MASTER_KEY_OLD=<old-key>
sentinel restore run legacy-restore --config <config>
```

### 5. (Optional) Re-encrypt under the new key

For uniform key coverage:

1. Restore the old artifact to a scratch DB.
2. Take a fresh backup against that scratch DB (encrypted with the new key).
3. Replace the old artifact with the new one in cold storage.
4. Retire `SENTINEL_MASTER_KEY_OLD` once nothing references it.

### Legacy envelope (v1) interaction

Pre-v2 envelope artifacts also require `--allow-legacy-envelope`. See [recover-legacy-envelope](./recover-legacy-envelope.md). Combine with the old key when both apply.

## Verification

```bash
sentinel backup --config <config> --job <name>
xxd -l 5 /path/to/new-artifact            # 5345 4e43 02
sentinel backup verify <id> --config <config>
sentinel restore dry-run legacy-restore --config <config>
```

## Rollback / recovery

If the new key is compromised mid-rotation, treat as [key-loss-incident](./key-loss-incident.md). Roll forward to a third key; do not revert to the previous one.

## References

- ADR 0006 — Encryption envelope v1 (`docs/adr/0006-encryption-envelope-v1.md`)
- `internal/cli/security.go`, `internal/crypto/key.go`
