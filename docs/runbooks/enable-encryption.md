# Runbook — Enable encryption

> **Superseded by the documentation site: [guides/enable-encryption](https://denisakp.github.io/sentinel/guides/enable-encryption).**
>
> This runbook shows `security init-key` output that the command no longer prints. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-08-02
- **Related**: ADR 0006 (envelope v1), spec 047 (remote-artifact security), [key-rotation](./key-rotation.md), [key-loss-incident](./key-loss-incident.md), [verify-backup-integrity](./verify-backup-integrity.md), [recover-legacy-envelope](./recover-legacy-envelope.md), [check-storage-backend](./check-storage-backend.md)

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

## Remote backends (S3 / GCS / Azure Blob / Google Drive)

As of spec 047, config-driven backups to a **remote** backend get the same
protection as local backups: the artifact is hashed, encrypted (when a key is
configured), and given an integrity manifest **before** it leaves the host.
Sentinel stages the dump locally, applies hash → AES-256-GCM encrypt → manifest,
then uploads the (encrypted) artifact **plus a `<name>.manifest.json` sidecar** to
the bucket, and cleans up the staging copy on both success and failure.

- Before this fix, a configured encryption key was silently ignored for remote
  backends — plaintext SQL was uploaded while the run reported success. That
  silent bypass is closed. See `release-notes.md` (Security).
- Every remote backup — encrypted or not — now records an integrity hash and
  uploads a manifest sidecar, so `sentinel backup verify` and restore can locate
  and validate it. `backup verify` fetches the remote artifact + sidecar; restore
  fetches, decrypts, and restores end-to-end.
- Pre-fix remote backups (plaintext, no sidecar) remain restorable — a missing
  manifest is tolerated, not fatal.

Nothing new to configure: point the job at a remote backend and set
`encryption_key_env` exactly as for local.

```yaml
encryption_key_env: SENTINEL_MASTER_KEY

defaults:
  storage:
    type: s3
    s3_bucket: my-bucket
    s3_region: us-east-1
    s3_access_key_id_env: SENTINEL_S3_ACCESS_KEY
    s3_secret_access_key_env: SENTINEL_S3_SECRET_KEY

databases:
  postgres-secure:
    type: postgres
    host: host.docker.internal
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-encrypted.sql   # object key; sidecar = <key>.manifest.json
```

Confirm the stored object is ciphertext, not plaintext SQL:

```bash
# v2 ciphertext begins with the "SENC" magic (53 45 4E 43 02); plaintext dumps
# begin with the "PostgreSQL database dump" header.
aws s3 cp s3://my-bucket/postgres-encrypted.sql - | head -c5 | xxd
```

### Known limitation — encrypted remote + auto-discovery `strategy: single`

Encrypted remote backups are **not** supported for the auto-discovery
`strategy: single` path (`database: "*"` with `strategy: single`), which writes a
single combined dump straight to the backend and cannot be staged and encrypted
in place. If an encryption key is configured for that combination the job **fails
loud** rather than upload plaintext:

```
backup '<job>': encrypted remote backup is not supported for the auto-discovery
'single' strategy; use strategy 'individual' or local storage
```

Use `strategy: individual` (the default — one encrypted artifact + sidecar per
discovered database) or local storage instead.

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
- `internal/adapters/crypto/encrypt.go`, `internal/adapters/crypto/envelope.go`
- `internal/cli/security.go`
