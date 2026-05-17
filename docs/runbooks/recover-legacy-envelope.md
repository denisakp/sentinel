# Runbook — Recover legacy envelope (pre-v2)

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: ADR 0006 (envelope v1/v2), [enable-encryption](./enable-encryption.md), [key-rotation](./key-rotation.md)

## When to use

You hold an encrypted backup produced before the v2 envelope bump and need to recover plaintext. By default Sentinel **refuses** legacy artifacts.

## Preconditions

- The original AES-256 key used to encrypt the artifact (env-resolvable).
- Operator awareness: recovered plaintext is treated as **recovered, not safe to keep**.

## Identify the envelope version

v2 artifacts carry a fixed 5-byte header `"SENC" || 0x02`. Legacy artifacts do not.

```bash
xxd -l 5 /path/to/backup.enc
# v2     -> 00000000: 5345 4e43 02
# legacy -> anything else (typically a small uint32-le length prefix)
```

The manifest also mirrors this: `encryption.envelope_version = 2` for v2.

## Steps

### Opt in to legacy decryption

```bash
SENTINEL_ALLOW_LEGACY_ENVELOPE=1 ./sentinel restore run <job> --allow-legacy-envelope
```

This emits a WARNING line to stderr and a `crypto.legacy_envelope_decrypt` log record. Default-deny applies to both `sentinel restore` and `sentinel backup verify`.

### Re-encrypt under the current envelope

Treat the recovered plaintext as transient. Immediately:

1. Take a fresh dump from the source DB using the current Sentinel build (writes v2 envelope automatically).
2. Or, restore the recovered artifact to a scratch DB and re-backup it.
3. Delete the recovered plaintext.

See [key-rotation](./key-rotation.md) if you also need a new key.

## Verification

```bash
xxd -l 5 /path/to/new-backup.enc       # 5345 4e43 02
sentinel backup verify <new-id> --config sentinel.yaml
```

## Rollback / recovery

Not applicable — this runbook is itself a recovery path. Do not re-enable `--allow-legacy-envelope` in steady-state automation.

## References

- ADR 0006 — Encryption envelope v1 (`docs/adr/0006-encryption-envelope-v1.md`)
- Feature spec: `specs/007-crypto-nonce-xor-fix/`
- `internal/crypto/envelope.go`, `internal/crypto/decrypt.go`
