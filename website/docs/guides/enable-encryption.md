---
title: Enabling backup encryption
description: Generate a master key, wire it into your configuration, and confirm that artifacts leave the host as ciphertext.
sidebar_position: 4
---

Turn on AES-256-GCM encryption for backup artifacts, so a dump is unreadable to anyone who obtains
the file without also obtaining your key.

## When to use this

Use this when you are enabling encryption for the first time, onboarding a new environment, or
proving to an auditor that artifacts are encrypted before they leave the host. It is also the first
half of any answer to "our bucket was exposed".

Do not use it to rotate an existing key; that is [Rotating the encryption key](./key-rotation.md),
and generating a new key in place is exactly how people lose old artifacts. If what you need to
encrypt is a credentials file rather than a dump, see step 6 of
[Supplying database credentials](./database-credentials.md).

## Before you start

- Somewhere to keep a key that is not the machine running Sentinel. A secrets manager injecting an
  environment variable is the expected shape. A mode `0600` file on the host is acceptable; the same
  disk as the backups is not.
- Permission to change the Sentinel configuration and to restart whatever runs it.
- An understanding of one rule before you touch anything else, because it is the thing people get
  wrong.

Encryption is opt in and never ambient. Exporting `SENTINEL_MASTER_KEY` does nothing on its own; the
configuration has to name the key. With no key named, Sentinel writes plaintext silently, because
plaintext is the documented default rather than a fallback. Once a key is named, a missing or
unreadable key is a hard failure and Sentinel will not quietly downgrade to plaintext.

:::danger A lost key means lost backups
There is no escrow, no recovery mode, and no partial read. Every artifact encrypted under a key you
cannot produce is permanently ciphertext, and the only way back is a fresh backup from a live source
database. Store the key before you enable encryption, not after.
:::

## Steps

### 1. Generate a key

```bash
sentinel security init-key
```

The key is printed once and stored nowhere:

```
Sentinel Master Encryption Key
===============================================================

  Key: <base64-encoded 256-bit key>

===============================================================

  IMPORTANT - Store this key securely. It cannot be recovered.
```

The rest of the banner suggests three ways to keep it. `--output json` emits the same key with its
algorithm and a generation timestamp, which is the form to pipe into a secrets manager.

:::caution The overwrite guard is narrower than it looks
`init-key` refuses to print a new key when the environment variable `SENTINEL_MASTER_KEY` is already
set, and asks you to pass `--force`. It checks that one hard-coded name only. If your configuration
names any other variable, the guard never fires and you get a fresh key with no warning. It also
exits `0` when it does refuse, so a script cannot tell a refusal from a success. Treat every
`init-key` run as producing a key you must reconcile by hand.
:::

Put the value in your secret store now, before continuing.

### 2. Name the key in the configuration

Key resolution is repository wide. There is no per-job and no per-restore-job encryption key; a
single top-level setting governs every artifact the configuration produces.

```yaml
version: "1.0"

encryption_key_env: SENTINEL_MASTER_KEY
# encryption_key_file: /etc/sentinel/master.key

databases:
  prod-postgres:
    type: postgres
    host: db.internal
    username: sentinel
    password_env: SENTINEL_DB_PASSWORD
    database: app
    output: prod-postgres.sql
    storage:
      type: local
      local_path: /var/backups/sentinel
```

Set either key, or both. The environment variable wins when both are configured and the variable is
non-empty; a key file is read and whitespace-trimmed. Either way the decoded value must be exactly 32
bytes of standard or URL-safe base64.

### 3. Run a backup

```bash
export SENTINEL_MASTER_KEY="$(vault kv get -field=key secret/sentinel/master)"
export SENTINEL_DB_PASSWORD="$(vault kv get -field=password secret/sentinel/prod)"
sentinel backup --config sentinel.yaml
```

The artifact and its `<name>.manifest.json` sidecar are written together. The manifest carries the
salt, base nonce, and envelope version needed to decrypt, and no key material. Losing it makes the
artifact unreadable even with the correct key, so treat the pair as one object.

### 4. Confirm the fail-fast behaviour

Prove to yourself that a missing key stops the run rather than producing a plaintext dump:

```bash
env -u SENTINEL_MASTER_KEY sentinel backup --config sentinel.yaml
```

The run exits with a key resolution error and no artifact is written. Note that
`sentinel config validate` is structural and does not need live key material, so it will report a
valid configuration here; only a real run exercises the key.

### 5. Remote backends need nothing extra

For S3, GCS, Azure Blob, and Google Drive, Sentinel stages the dump locally, hashes it, encrypts it,
writes the manifest, then uploads the ciphertext and the sidecar, and cleans the staging copy up on
both success and failure. Point the job at a remote backend and set `encryption_key_env` exactly as
above.

:::caution Not supported with `strategy: single`
Encrypted remote backups are refused for auto-discovery with `database: "*"` and `strategy: single`,
which writes one combined dump straight to the backend and cannot be staged and encrypted on the host
first. Sentinel fails the job rather than upload plaintext:

```
encrypted remote backup is not supported for the auto-discovery 'single' strategy; use strategy 'individual' or local storage
```

Use `strategy: individual`, which is the default, or local storage.
:::

## Verify

Check the first five bytes of the artifact. Envelope v2 ciphertext starts with the magic `SENC`
followed by `0x02`:

```bash
xxd -l 5 /var/backups/sentinel/prod-postgres.sql
```

```
00000000: 5345 4e43 02                             SENC.
```

A plaintext PostgreSQL dump begins with its own header text instead, so the two are never ambiguous.
For a remote object, stream the first bytes rather than downloading the whole artifact, for example
`aws s3 cp s3://<bucket>/<key> - | head -c 5 | xxd`.

Confirm the run was recorded, then check the artifact against its manifest. `backup verify`
recomputes the SHA-256 of the stored bytes and compares it to the value the manifest recorded:

```bash
sentinel monitor list --config sentinel.yaml --last 1h
sentinel backup verify <backup-id> --config sentinel.yaml
```

:::caution Verification is not a decryption test
`backup verify` hashes the artifact as it sits and never decrypts it, so it proves the file is intact
and does not prove your key opens it. The `--allow-legacy-envelope` flag it accepts has no effect
here for the same reason.
:::

The real proof is a restore. Rehearse one against a scratch target before you rely on this.

## If it goes wrong

**The backup fails with a key resolution error.** The configuration names a key that could not be
found, was not valid base64, or did not decode to 32 bytes. This is deliberately fatal; Sentinel
never falls back to plaintext when encryption was requested. Check that the variable is exported in
the environment the scheduler actually runs in, which under systemd or cron is not your login shell.

**`authentication tag verification failed`.** The key is wrong, or the ciphertext or its tag has been
altered. Sentinel cannot distinguish the two. Check the key before concluding the file is corrupt.

**Artifacts are still plaintext and no error appeared.** The configuration does not name a key.
Exporting `SENTINEL_MASTER_KEY` alone changes nothing. Confirm `encryption_key_env` or
`encryption_key_file` is present at the top level of the file, not nested inside a job.

**A restore refuses a legacy envelope.** The artifact predates envelope v2 and lacks the `SENC`
header. Pass `--allow-legacy-envelope` to `sentinel restore` to recover the plaintext, then re-take
the backup from source; the flag is a recovery tool, not a setting.

**Turning encryption back off.** Remove `encryption_key_env` and `encryption_key_file` and re-run.
New artifacts are plaintext from that point. Existing encrypted artifacts stay encrypted and still
need the original key, so keep it for as long as any of them are within your retention window.

## Related

- [Security and encryption](../concepts/security-encryption.md): the envelope format, key
  derivation, and what the manifest records.
- [Rotating the encryption key](./key-rotation.md): replacing a key without stranding old artifacts.
- [Verifying backup integrity](./verify-backup-integrity.md): what `backup verify` actually checks.
- [Manifest](../concepts/manifest.md): the sidecar that carries the salt and nonce.
- [`sentinel security` reference](../reference/cli/security.md): every subcommand and flag.
- [Configuration reference](../reference/configuration.md): every YAML key.

<!-- sources: internal/cli/security.go, internal/adapters/crypto/key.go, internal/adapters/crypto/encrypt.go, internal/adapters/crypto/envelope.go, internal/cli/backup_factory.go, internal/cli/backup_verify.go, internal/cli/config.go, internal/config/types.go, internal/ports/encryption.go, docs/runbooks/enable-encryption.md -->
