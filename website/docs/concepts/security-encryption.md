---
title: Security and encryption
description: How Sentinel encrypts backup artifacts and secrets files at rest, how keys are provisioned, and why key loss is unrecoverable.
sidebar_position: 5
---

Sentinel can encrypt a backup artifact with AES-256-GCM before it is written to storage, and can
encrypt the credentials file a job reads at startup. Both are opt-in: a Sentinel configuration with
no key reference produces plaintext artifacts and reads plaintext credentials files, and it does so
silently, because that is the documented default rather than a fallback. Encryption switches on when
the configuration names a key, and from that point a missing or unreadable key is a hard failure
rather than a quiet downgrade.

## Why it exists

A backup is a complete, portable copy of your production data with none of the access control that
protected the database. It lands on a disk, in an object store, or on someone's laptop, and it stays
readable for as long as it exists. The database enforces authentication; the dump file enforces
nothing.

Storage-side encryption helps, but it is the storage provider's key, applied after the bytes have
crossed the network, and it does not survive the object being copied somewhere else. Sentinel
encrypts before the artifact leaves the host, under a key the storage provider never sees, so a
compromised bucket yields ciphertext and a leaked artifact yields ciphertext.

The same argument applies one level down, to the credentials that let Sentinel reach the database at
all. A MySQL `defaults_file` or a MongoDB secrets file is a plaintext password sitting on disk next
to the configuration. Encrypting it at rest removes the last plaintext credential from the host.

## How it works

There are two separate containers. They share a cipher and a key format, and nothing else.

### The backup artifact envelope: `SENC`

When a job has encryption configured, the backup pipeline encrypts the artifact in place after
compression and before the manifest is written. The result is a stream in Sentinel's envelope v2
format:

```
"SENC" (4 bytes) || 0x02 (1 byte) || [uint32-le length][ciphertext + 16-byte tag] ...
```

The plaintext is consumed in 64 KB chunks. A random base nonce is generated once per artifact; each
chunk's nonce is that base nonce with the chunk index XOR-ed into its trailing eight bytes, so no
nonce is reused within a stream. The backup ID is passed as Additional Authenticated Data, binding
the ciphertext to the specific backup record: decrypting it as some other backup fails the tag check.

The key that actually encrypts is not the key you configure. Sentinel reads a 32-byte master key,
generates a fresh random 32-byte salt for this artifact, and derives the per-artifact key with
PBKDF2-HMAC-SHA256 at 100,000 iterations. One compromised artifact key therefore does not compromise
the others.

Everything needed to decrypt except the master key is recorded in the artifact's manifest sidecar:

```json
{
  "encryption": {
    "algorithm": "AES-256-GCM",
    "key_derivation": "PBKDF2-HMAC-SHA256",
    "iterations": 100000,
    "salt": "<base64 salt>",
    "iv": "<hex base nonce>",
    "auth_tag": "<hex tag of the final chunk>",
    "envelope_version": 2
  }
}
```

The manifest holds no key material. Losing the manifest costs you the salt and nonce and makes the
artifact unreadable; losing the master key costs you everything encrypted under it.

Restore reverses the sequence: verify the manifest hash, read the salt and IV back out of the
manifest, re-derive the same key from the master key, and stream the plaintext into the engine's
restore tool. Artifacts written before envelope v2 carry no `SENC` header. Sentinel refuses them by
default and needs `--allow-legacy-envelope` on `sentinel restore` or `sentinel backup verify` to
proceed, because the pre-v2 nonce scheme was flawed and any such ciphertext should be treated as
potentially compromised.

### The secrets file container: `SSEC`

:::info Added in v1.4.0
At-rest encryption of secrets files, the `secrets_key_env` and `secrets_key_file` keys, and both
`sentinel security encrypt-secrets-file` and `sentinel security reencrypt` require Sentinel v1.4.0
or later. Artifact encryption and `sentinel security init-key` are available in earlier releases.
:::

A backup artifact has a manifest to carry its nonce. A standalone credentials file does not, so it
gets its own self-contained container:

```
"SSEC" (4 bytes) || 0x01 (1 byte) || 12-byte base nonce || <SENC v2 stream>
```

The payload is an ordinary `SENC` v2 stream produced by the same writer; the only addition is the
framing that carries the nonce. Two differences from the artifact envelope are worth knowing. The
key is used directly, with no PBKDF2 derivation and no salt, because there is nowhere to record one.
And the AAD is a fixed constant rather than a backup ID, which keeps a secrets file from being
decrypted as anything else.

Detection is content-based. At configuration load time Sentinel reads the file named by
`defaults_file` or `mongo_secrets_file`, checks for the `SSEC` magic, and decrypts it in memory if
present. A plaintext credentials file never starts with those four bytes, so there is no flag to set
and no way to get the two confused. The decrypted plaintext is never written to disk, and the whole
file is decrypted before any of it is parsed, so a wrong key produces a clean error rather than a
half-parsed credential.

### Key provisioning

A key is 32 random bytes, base64-encoded. Sentinel resolves it from an environment variable or a
file, preferring the environment variable when both are configured and the variable is non-empty. A
key file is read and whitespace-trimmed. Standard base64 is tried first, then URL-safe base64, and
the decoded value must be exactly 32 bytes or the run fails.

Nothing is ambient. Exporting `SENTINEL_MASTER_KEY` does not enable encryption on its own; the
configuration has to name the variable.

## Per-engine behaviour

The cryptography does not vary by engine. All four engines produce the same `SENC` v2 envelope from
the same code path, with the same cipher, chunk size, key derivation, and manifest fields. What does
vary is whether the engine has a secrets file to encrypt in the first place.

| Engine | Artifact encryption | Secrets file eligible for `SSEC` |
|---|---|---|
| PostgreSQL | AES-256-GCM envelope v2 | None. PostgreSQL jobs take their password from `password_env` only |
| MySQL | AES-256-GCM envelope v2 | `defaults_file` (a `my.cnf`), optionally located via `defaults_file_env` |
| MariaDB | AES-256-GCM envelope v2 | `defaults_file`, identical to MySQL |
| MongoDB | AES-256-GCM envelope v2 | `mongo_secrets_file`, optionally located via `mongo_secrets_file_env` |

One engine-independent limitation applies to remote storage: an encrypted backup is refused for the
auto-discovery `strategy: single` path, which writes straight to the backend and cannot be staged
and encrypted on the host first. Sentinel fails the job rather than upload plaintext. Use
`strategy: individual` or local storage.

## Configuration

Four top-level keys control key resolution. All four are optional; setting none of them is the
plaintext default.

```yaml
version: "1.0"

# Enables artifact encryption. Either key is sufficient; the env var wins when both are set.
encryption_key_env: SENTINEL_MASTER_KEY
# encryption_key_file: /etc/sentinel/master.key

# Optional separate key for encrypted secrets files. Each field falls back
# independently to its encryption_key_* counterpart when unset.
secrets_key_env: SENTINEL_SECRETS_KEY
# secrets_key_file: /etc/sentinel/secrets.key

databases:
  prod-mysql:
    type: mysql
    database: myapp_prod
    # An SSEC-encrypted my.cnf; detected by content, decrypted in memory at load time.
    defaults_file: /etc/sentinel/mysql.cnf.enc
    output: prod-mysql.sql
    storage:
      type: local
      local_path: ./backups
```

The fallback is per field, not per pair: `secrets_key_env` falls back to `encryption_key_env`, and
`secrets_key_file` falls back to `encryption_key_file`. A deployment that wants one key for
everything sets only the `encryption_key_*` pair and the secrets path picks it up. A deployment that
wants the artifact key and the credentials key on independent rotation schedules sets both pairs.

There is no per-job and no per-restore-job encryption key; key resolution is repository-wide. The
complete key list is in the [configuration reference](../reference/configuration.md).

## Example

Generate a key. It is printed once and stored nowhere:

```bash
sentinel security init-key
```

```
Sentinel Master Encryption Key
===============================================================

  Key: <base64-encoded 256-bit key>

===============================================================

  IMPORTANT - Store this key securely. It cannot be recovered.
```

`--output json` emits the same key with its algorithm and generation timestamp for a secrets-manager
pipeline. Put the value in your secret store, inject it as an environment variable, name that
variable in the configuration, and run a backup:

```bash
export SENTINEL_MASTER_KEY="$(vault kv get -field=key secret/sentinel/master)"
sentinel backup --config sentinel.yaml
```

The artifact on disk is now ciphertext. Its first five bytes are the envelope header:

```bash
xxd -l 5 ./backups/prod-postgres.sql
```

```
00000000: 5345 4e43 02                             SENC.
```

To encrypt a credentials file, point `encrypt-secrets-file` at the plaintext and give it a new
destination:

```bash
sentinel security encrypt-secrets-file /etc/sentinel/mysql.cnf \
  --out /etc/sentinel/mysql.cnf.enc \
  --key-env SENTINEL_SECRETS_KEY
```

```
Encrypted secrets file written to /etc/sentinel/mysql.cnf.enc
The plaintext input '/etc/sentinel/mysql.cnf' was left untouched.
Once you have verified the encrypted file loads, remove the plaintext original (e.g. shred -u).
```

The output is written atomically with mode `0600`. The command refuses an `--out` equal to the input
path, and refuses to overwrite an existing output without `--force`, so it can never destroy the
plaintext you still need. Repoint `defaults_file` at the encrypted path, confirm a run succeeds, and
only then remove the original.

:::danger Destructive
Deleting the plaintext credentials file is irreversible if the secrets key is not yet safely stored.
Run the job once against the encrypted file and confirm it connects before you shred the original.
:::

To move existing artifacts onto a new key, `sentinel security reencrypt --mode rotate` decrypts each
one with the current key and re-encrypts it under the variable named by `--new-key-env`. Preview
first; a dry run mutates nothing:

```bash
sentinel security reencrypt --all --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEW --dry-run --config sentinel.yaml
```

A dry run prints a banner confirming that nothing was modified, then the same report it would
produce for real:

```
ID                                    JOB            MODE    FROM->TO  OUTCOME
3f1c9a20-5b7e-4d21-9c88-0a2f6e1d4b93  prod-postgres  rotate  v2->v2    would_rotate
1 processed · 0 migrated · 1 rotated · 0 skipped · 0 failed
```

Dropping `--dry-run` applies it. A bulk `--all` mutation additionally requires `--yes`; a single
backup ID does not. The pipeline is write-new, then verify by decrypting the new artifact back and
comparing plaintext hashes, then swap, so an interrupted run never leaves you without a recoverable
artifact. `--keep-original` writes the new artifact to a sibling path and leaves the original in
place for a manual cutover.

The command's default mode is `legacy`, not `rotate`. That mode re-wraps a pre-v2 artifact into the
current envelope under the same key so it no longer needs `--allow-legacy-envelope`. It prints a
caveat every time, because re-wrapping restores the format but cannot undo the confidentiality
weakness of a ciphertext that was already exposed. Re-running the backup from source is the real
remediation where the source still exists.

## Failure modes

**The master key is lost.** There is no recovery. AES-256-GCM has no backdoor, no escrow, and no
partial read: every artifact encrypted under that key is permanently ciphertext. The only path
forward is a fresh backup from a live source database under a new key. Treat the key as the most
important thing in the backup system, because it is the one component with no redundancy story.

**`authentication tag verification failed`.** The key is wrong, or the ciphertext or its tag has
been altered. Sentinel cannot distinguish the two, by design. Check that the environment variable
holds the key that was current when this artifact was written before concluding the file is corrupt.

**The backup fails with a key-resolution error.** The configuration names a key that could not be
resolved, was not valid base64, or did not decode to 32 bytes. This is deliberately fatal. Sentinel
never falls back to writing plaintext when encryption was requested. Note that `sentinel config
validate` is structural and does not need live key material, so it will not catch this.

**A restore or verify refuses a legacy envelope.** The artifact predates envelope v2 and lacks the
`SENC` header. Pass `--allow-legacy-envelope` to recover the plaintext, treat what you recover as
recovered rather than safe to keep, and re-take the backup from source. `sentinel security reencrypt`
is the fallback when the source is gone.

**`unsupported envelope version`.** The artifact was written by a newer Sentinel than the one
reading it. Upgrade the binary; this is not corruption.

**An encrypted secrets file fails to load.** If no key is configured at all, the error names the file
and the four keys that could supply one. If a key is configured but wrong, the message is
`wrong key or corrupt data`. Because the whole file is decrypted before parsing, you never get a
partially applied credential set.

**A plaintext secrets file warns about permissions.** Sentinel warns when a `defaults_file` or
`mongo_secrets_file` is group- or world-readable and recommends `chmod 0600`. The warning is
suppressed for `SSEC` files, since a readable ciphertext is not a credential exposure.

## Related

- [How Sentinel fits together](../intro/architecture-overview.md): where the crypto adapter sits
  relative to the backup and restore paths.
- [Backup](./backup.md): where encryption happens in the run sequence.
- [Restore](./restore.md): verification and decryption on the way back in.
- [Credential sanitization](./credential-sanitization.md): keeping passwords out of process
  arguments and logs.
- [Manifest](./manifest.md): the sidecar that carries the salt, IV, and envelope version.
- [`sentinel security` reference](../reference/cli/security.md): every subcommand and flag.
- [Configuration reference](../reference/configuration.md): every YAML key.

<!-- sources: internal/adapters/crypto/key.go, internal/adapters/crypto/encrypt.go, internal/adapters/crypto/decrypt.go, internal/adapters/crypto/envelope.go, internal/adapters/crypto/secrets_envelope.go, internal/adapters/crypto/hash.go, internal/cli/security.go, internal/cli/security_reencrypt.go, internal/cli/security_secrets.go, internal/cli/legacy_envelope.go, internal/cli/backup_factory.go, internal/config/types.go, internal/config/secrets_file_crypto.go, internal/config/defaults_file_resolve.go, internal/config/mongo_secrets_file.go, internal/ports/encryption.go, internal/ports/manifest.go, internal/adapters/restore/runtime/preflight.go, docs/runbooks/enable-encryption.md, docs/runbooks/key-rotation.md, docs/runbooks/key-loss-incident.md, docs/runbooks/recover-legacy-envelope.md -->
