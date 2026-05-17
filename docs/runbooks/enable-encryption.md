# Runbook — Enable encryption

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: ADR 0006 (envelope v1), [key-rotation](./key-rotation.md), [key-loss-incident](./key-loss-incident.md), [verify-backup-integrity](./verify-backup-integrity.md), [recover-legacy-envelope](./recover-legacy-envelope.md)

## When to use

First-time enablement of at-rest encryption for backup artifacts, or onboarding a new environment.

## Preconditions

- Sentinel binary present.
- Secret store / env-var injection mechanism in place for `SENTINEL_MASTER_KEY`.
- You understand the **opt-in rule** below — ambient env vars alone do not enable encryption.

## Activation rule (v1.0.1)

- No `encryption_key_env` / `encryption_key_file` in config → backup stays plaintext, even if `SENTINEL_MASTER_KEY` is exported.
- Explicit encryption config + missing/invalid key → backup **fails at runtime** (no silent plaintext fallback).

## Steps

### Generate a master key

```bash
sentinel security init-key
```

Output:

```
SENTINEL_MASTER_KEY=<base64-encoded-256-bit-key>
```

Store securely (vault, KMS, secret manager). Then:

```bash
export SENTINEL_MASTER_KEY=<value>
```

### Wire into config

```yaml
encryption_key_env: SENTINEL_MASTER_KEY

databases:
  postgres-secure:
    type: postgres
    host: host.docker.internal
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-encrypted
    storage:
      type: local
      local_path: /workspace/backups
```

### Run

```bash
export SENTINEL_MASTER_KEY=<your-key>
export DEV_POSTGRES_PASSWORD=sentinel
sentinel backup --config /workspace/infra/dataset/local.yaml
```

The artifact and its SHA-256 manifest are written to storage.

### Confirm runtime fail-fast on missing key

```bash
unset SENTINEL_MASTER_KEY
sentinel backup --config /workspace/infra/dataset/local.yaml
```

Expected: backup exits with a key-resolution / encryption error. No plaintext artifact is produced.

`sentinel config validate` is structural and does **not** require live key material.

## Envelope format

- Cipher: AES-256-GCM, 64 KB chunks, counter-XOR nonce.
- v2 artifacts carry a 5-byte header `"SENC" || 0x02`; manifest mirrors `encryption.envelope_version = 2`.
- Details: ADR 0006 (`docs/adr/0006-encryption-envelope-v1.md`).

## Verification

```bash
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 1h
xxd -l 5 /workspace/backups/<your-encrypted-file>
sentinel backup verify <execution-id> --config /workspace/infra/dataset/local.yaml
```

See [verify-backup-integrity](./verify-backup-integrity.md).

## Rollback / recovery

To stop producing encrypted backups, remove `encryption_key_env` / `encryption_key_file` from the config and re-run. Existing encrypted artifacts remain decryptable only with the original key — see [key-loss-incident](./key-loss-incident.md).

## References

- ADR 0006 — Encryption envelope v1 (`docs/adr/0006-encryption-envelope-v1.md`)
- `internal/crypto/encrypt.go`, `internal/crypto/envelope.go`
- `internal/cli/security.go`
