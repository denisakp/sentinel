# Runbook — Key rotation

> **Superseded by the documentation site: [guides/key-rotation](https://denisakp.github.io/sentinel/guides/key-rotation).**
>
> This runbook shows `encryption_key_env` nested under a `restores:` job, which is not a field that exists. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-08-05
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

### 5. Re-encrypt existing artifacts under the new key (`security reencrypt`)

For uniform key coverage, `sentinel security reencrypt --mode rotate`
re-wraps existing encrypted artifacts under the new key in place — no scratch
DB, no restore/backup dance. It decrypts each artifact with the current key
(`encryption_key_env`) and re-encrypts with the key named by `--new-key-env`.

Preview first (never mutates), then bulk-apply (`--all` requires `--yes`):

```bash
export SENTINEL_MASTER_KEY=<old-key>          # current, from encryption_key_env
export SENTINEL_MASTER_KEY_NEW=<new-key>

# Dry run — shows what would be re-encrypted, changes nothing:
sentinel security reencrypt --all --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEW --dry-run --config <config>

# Apply to a single artifact:
sentinel security reencrypt <backup-id> --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEW --config <config>

# Apply to everything (optionally scoped by --job / --since), keeping originals:
sentinel security reencrypt --all --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEW --keep-original --yes --config <config>
```

Flags: `--all` (repo-wide; mutually exclusive with `[backup-id]`, needs `--yes`),
`--job <name>` / `--since <30d|4w|720h>` (scope `--all`), `--new-key-env`
(**required** in rotate mode, env-only — never a literal key), `--keep-original`
(retain the pre-rotation artifact), `--dry-run`, `--output json|text`.

Once every live artifact is re-encrypted and nothing references
`SENTINEL_MASTER_KEY_OLD`, retire the old key.

> `--mode legacy` re-wraps **legacy v1 envelopes** into the current format under
> the *same* key (no `--new-key-env`); that path is covered in
> [recover-legacy-envelope](./recover-legacy-envelope.md), not here.

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
- `internal/cli/security.go`, `internal/cli/security_reencrypt.go`, `internal/adapters/crypto/key.go`
