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

**Preferred remediation — re-backup from source.** Treat the recovered plaintext as transient. Immediately:

1. Take a fresh dump from the source DB using the current Sentinel build (writes v2 envelope automatically).
2. Or, restore the recovered artifact to a scratch DB and re-backup it.
3. Delete the recovered plaintext.

This is the only remediation that restores the confidentiality guarantee — a fresh ciphertext under the v2 envelope with no nonce-reuse exposure.

### Guided migration when you cannot re-backup from source — `sentinel security reencrypt`

When the source DB state is gone, the PITR window has closed, or the legacy artifact is the only surviving copy, use the guided command to re-wrap it into the v2 envelope so it stops requiring `--allow-legacy-envelope`:

```bash
# Preview what would migrate (mutates nothing):
sentinel security reencrypt --all --dry-run --config sentinel.yaml

# Migrate one backup (single id needs no --yes):
sentinel security reencrypt <backup-id> --config sentinel.yaml

# Migrate all legacy backups (bulk needs --yes; scope with --job / --since):
sentinel security reencrypt --all --yes --config sentinel.yaml
```

> ⚠ **Confidentiality limit.** `security reencrypt` migrates the artifact's
> **format/availability** — it does **NOT** undo the legacy envelope's
> confidentiality weakness. A pre-v2 ciphertext may already be compromised;
> re-wrapping the same plaintext cannot change that. Re-backup from source
> (above) wherever possible. Use `reencrypt` only when you can't, to get off the
> `--allow-legacy-envelope` dependency. The command prints this caveat on every
> legacy run.

The command never destroys a recoverable artifact: it writes the new artifact, verifies it (decrypt-back + hash), and only then retires the original (`--keep-original` retains it for a manual cutover). It is idempotent — an already-v2 backup is skipped.

### Rotating the key at the same time

`security reencrypt --mode rotate --new-key-env <VAR>` re-encrypts **current-format** backups under a new master key (compromised/retired key). See [key-rotation](./key-rotation.md). Rotation has no confidentiality caveat — the v2 envelope is sound.

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
- `internal/adapters/crypto/envelope.go`, `internal/adapters/crypto/decrypt.go`
