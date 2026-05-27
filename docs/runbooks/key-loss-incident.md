# Runbook — Key loss incident

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [enable-encryption](./enable-encryption.md), [key-rotation](./key-rotation.md), ADR 0006 (envelope v1)

## When to use

`SENTINEL_MASTER_KEY` is lost, leaked, or revoked, and you need to plan recovery and remediation.

## Blast radius

- Every backup artifact encrypted under that key is **unrecoverable** without the key. AES-256-GCM does not have a recovery path.
- Manifest auth-tag verification fails for those artifacts (`crypto.ErrAuthTagFailed` → CLI `file corrupt or wrong key`).
- `sentinel backup verify` and `sentinel restore` both fail for affected artifacts.

## Preconditions

- An accurate inventory of which schedules / configs reference the lost key.
- Source DBs still available for fresh dumps.

## Steps

### Inventory affected schedules and artifacts

```bash
grep -rnE 'encryption_key_env|encryption_key_file' /etc/sentinel/ ./*.yaml
sentinel storage status --config <config> --output json
sentinel monitor list --config <config> --last 90d
```

Mark every artifact whose schedule referenced the lost key as **unrecoverable**.

### Re-establish a baseline from source

For each affected job:

```bash
# 1. Generate a new key (do NOT run with --force yet if other schedules depend on the old key)
sentinel security init-key
export SENTINEL_MASTER_KEY=<new-key>

# 2. Take a fresh dump against the live DB with the new key
sentinel backup --config <config> --job <name>
```

This produces a new v2 envelope artifact under the new key. Treat earlier artifacts as cold-storage-only (operator-managed); they remain ciphertext-only.

### Do not force-overwrite the key blindly

```bash
# DANGEROUS without audit:
sentinel security init-key --force
```

`--force` regenerates the env-var value but does not migrate existing artifacts. Audit which schedules / restore jobs read the old key first. Coordinate with [key-rotation](./key-rotation.md).

### If the key was leaked (not lost)

1. Rotate per [key-rotation](./key-rotation.md).
2. Assume any artifact encrypted under the leaked key is compromised; treat as plaintext.
3. Move artifacts to a new bucket / path under the new key for clean separation.

## Verification

```bash
xxd -l 5 /path/to/new-backup.enc                 # 5345 4e43 02 (v2 header)
sentinel backup verify <new-execution-id> --config <config>
sentinel monitor list --config <config> --last 1h
```

## Rollback / recovery

There is no rollback for a lost key. The only recovery path is fresh data from source DBs.

## References

- ADR 0006 — Encryption envelope v1 (`docs/adr/0006-encryption-envelope-v1.md`)
- [enable-encryption](./enable-encryption.md), [key-rotation](./key-rotation.md)
- `internal/adapters/crypto/encrypt.go`, `internal/adapters/crypto/decrypt.go`
