---
title: Key loss incident
description: "Triage for a lost, wrong, or leaked master encryption key: what is still recoverable, what is not, and how to re-baseline."
sidebar_position: 7
---

The master key that decrypts your backup artifacts is missing, does not match, or has been exposed.

## Symptoms

A restore of an encrypted artifact aborts before it touches the target database:

```
file corrupt or wrong key: crypto: authentication tag verification failed (chunk 0)
```

Sentinel cannot tell a wrong key from a damaged ciphertext, and does not guess. Both produce this
message.

The key cannot be resolved at all. The variable named here is whichever one `encryption_key_env`
points at:

```
crypto: no master key found (set SENTINEL_MASTER_KEY or configure encryption_key_file)
```

```
crypto: no master key found (configure encryption_key_env or encryption_key_file)
```

The key resolved but is not a key:

```
crypto: master key is not valid base64: illegal base64 data at input byte 43
crypto: master key must be 32 bytes (got 16 after base64 decode)
```

An encrypted credentials file cannot be opened at configuration load time, so the job fails before it
reaches the database:

```
cannot decrypt secrets file '/etc/sentinel/mysql.cnf.enc': wrong key or corrupt data
```

```
encrypted secrets file '/etc/sentinel/mysql.cnf.enc' but no decryption key configured (set secrets_key_env/secrets_key_file or encryption_key_env/encryption_key_file)
```

:::caution A passing `backup verify` is not evidence that your key works
`sentinel backup verify` re-computes the SHA-256 of the **stored** bytes and compares it to the
manifest. It never decrypts. `verifyExecution` discards its options parameter
(`internal/cli/backup_verify.go:178` takes it as `_`), which also means the `--allow-legacy-envelope`
flag registered on `backup verify` does nothing. A verified encrypted artifact is proven **intact**.
It is not proven **decryptable with the key you hold**. Tracked in
[#164](https://github.com/denisakp/sentinel/issues/164).

`sentinel restore dry-run` does not decrypt either; it prints the job's configuration. The only
check that exercises the key is a real restore.
:::

## Before you start

Capture this before you change anything. Some of it disappears the moment a process restarts.

```bash
# Which keys does the configuration actually name?
grep -nE 'encryption_key_(env|file)|secrets_key_(env|file)' sentinel.yaml

# Execution history and the artifacts at risk.
sentinel monitor list --config sentinel.yaml --last 90d --limit 1000 --format json > incident-executions.json

# What is reachable in each storage backend right now.
sentinel storage status --config sentinel.yaml --output json > incident-storage.json
```

If a scheduler process is still running, its environment may still hold the key that its parent
shell has lost. Capture it before that process is restarted or redeployed.

:::danger Do not run `sentinel security init-key` yet
`init-key` prints a key and writes nothing, so the command itself is harmless. The danger is the
step operators take next: pasting the new key over the old entry in the secret store, which destroys
the only remaining chance of recovering it.

Its overwrite guard will not stop you. The guard reads the hard-coded `SENTINEL_MASTER_KEY`
regardless of which variable `encryption_key_env` names, so under any other variable name it never
fires. When it does fire it prints a warning to stderr and exits **0**, so a script reads the refusal
as success. Tracked in [#160](https://github.com/denisakp/sentinel/issues/160) and
[#170](https://github.com/denisakp/sentinel/issues/170).

Likewise, do not overwrite an existing `encryption_key_file` or an existing secret-store entry until
triage is complete.
:::

## Resolution

### 1. Establish which key the configuration expects

Key resolution is repository-wide, not per job. Sentinel prefers the environment variable over the
file when both are set and the variable is non-empty, and `secrets_key_env` / `secrets_key_file` each
fall back independently to their `encryption_key_*` counterpart. A configuration that names only
`encryption_key_env` uses that one key for artifacts and for encrypted credentials files, so a single
lost key can take out both.

### 2. Check whether the value is merely absent from this process

This is the most common cause and the least alarming one. Check presence and length without ever
printing the value:

```bash
[ -n "${SENTINEL_MASTER_KEY:-}" ] && echo "set, ${#SENTINEL_MASTER_KEY} chars" || echo "unset"
```

A 32-byte key is 44 characters of base64. A different length means a truncated, wrapped, or wrong
value rather than a lost one; the `must be 32 bytes` error above says the same thing. For a key file,
`ls -l` and a byte count tell you whether the file is empty or has been rewritten. Repeat the check
inside the environment that actually runs Sentinel: the systemd unit, the container spec, the cron
entry. A key present in your shell and absent from the scheduler is an environment-propagation bug,
not a key loss.

### 3. Prove whether the key you hold still decrypts

Nothing short of a restore answers this. Point a restore job at a scratch database and run it.

:::danger Destructive
`sentinel restore run` writes into the database named by the restore job. Confirm the job's
`database` is a throwaway target before running it, not the production database you are trying to
protect.
:::

```bash
sentinel restore run scratch-verify --config sentinel.yaml --keep-file
```

| Outcome | What it means |
|---|---|
| The restore completes | The key is good. Your problem is elsewhere: environment propagation, the wrong config, or a damaged single artifact. |
| `file corrupt or wrong key: crypto: authentication tag verification failed` | The key does not match this artifact, or the artifact is damaged. Sentinel cannot distinguish the two. |
| `crypto: no master key found ...` | The key was not resolved at all. Return to step 2. |
| `crypto: legacy (pre-v2) envelope detected` | Not a key problem. See [Recovering a pre-v2 envelope](./recover-legacy-envelope.md). |
| `crypto: unsupported envelope version 0x03` | The artifact was written by a newer Sentinel. Upgrade the binary; this is not corruption and not key loss. |

### 4. Search for the key before declaring it lost

Every place worth checking, in rough order of yield: version history in the secret manager, the
configuration-management repository, the environment of a still-running scheduler process, host
backups of the `encryption_key_file` path, and whatever break-glass copy your organisation holds. A
key file is base64 text and is read whitespace-trimmed, so a copy that gained a trailing newline is
still usable.

If you recover it, put it back into the secret store as a new version, re-export it, and repeat
step 3.

### 5. If the key is genuinely lost, say so and stop looking

There is no recovery path. AES-256-GCM has no escrow, no backdoor, and no partial read; the artifact
key is derived from the master key with PBKDF2, and without the master key it cannot be re-derived.
Every artifact encrypted under that key, and every `SSEC` credentials file encrypted under it, is
permanently ciphertext. No Sentinel command, no upgrade, and no support path changes that.

Record the affected artifacts as unrecoverable, but **do not delete them**. Keeping ciphertext costs
storage and nothing else, and it is the only thing that becomes valuable again if the key later
resurfaces. Deleting it forecloses that.

### 6. Re-baseline from source under a new key

The only route to a usable backup is a fresh dump from a live source database.

```bash
sentinel security init-key
```

Store the printed key in your secret manager as a **new** entry beside the old one. Then point the
configuration at it and run a backup:

```bash
export SENTINEL_MASTER_KEY="$(vault kv get -field=key secret/sentinel/master-v2)"
sentinel backup --config sentinel.yaml
```

`sentinel backup --config` runs every enabled job under `databases:`. There is no job selector on
`sentinel backup`.

If an encrypted credentials file was also lost with the key, rebuild the plaintext file from your
credential source and re-encrypt it:

```bash
sentinel security encrypt-secrets-file /etc/sentinel/mysql.cnf \
  --out /etc/sentinel/mysql.cnf.enc \
  --key-env SENTINEL_SECRETS_KEY
```

### 7. If the key was leaked rather than lost, rotate it

You still hold the key, so the artifacts are still readable and can be moved onto a new one. Preview
first; a dry run mutates nothing:

```bash
sentinel security reencrypt --all --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEW --dry-run --config sentinel.yaml
```

Drop `--dry-run` and add `--yes` to apply it. Always pass `--config` explicitly: unlike most
commands, `security reencrypt` defaults to `$HOME/.sentinel/config.yaml` rather than
`./sentinel-config.yaml`, and it loads the file without running schema validation. Tracked in
[#171](https://github.com/denisakp/sentinel/issues/171).

Rotation changes who can read the artifacts from now on. It does not retract the exposure. Treat
every artifact written under the leaked key as though its plaintext were public, and plan on that
basis.

## Verify recovery

A new artifact carries the envelope v2 header in its first five bytes:

```bash
xxd -l 5 ./backups/prod-postgres.sql
```

```
00000000: 5345 4e43 02                             SENC.
```

Integrity, which is all `verify` can tell you:

```bash
sentinel backup verify <new-backup-id> --config sentinel.yaml
sentinel monitor list --config sentinel.yaml --last 1h
```

Then the check that actually matters: restore the new artifact into a scratch database, as in
step 3. Until a restore has succeeded under the new key, you have an untested backup.

## Prevent recurrence

The master key is the one component of the backup system with no redundancy story. Give it one.
Store it in a secret manager with version history, so an overwrite is reversible, and keep a
documented break-glass copy somewhere that survives the loss of the primary store.

Rehearse a restore on a schedule. It is the only routine check that exercises the key end to end;
integrity sweeps and `sentinel config validate` never touch key material, and neither does
`backup verify`.

Do not rely on the `init-key` overwrite guard as a safety net in any runbook or script, for the
reasons above. If you split `secrets_key_*` from `encryption_key_*`, commit to operating two rotation
schedules; a second key that nobody rotates is a second key that can be lost.

## Related

- [Security and encryption](../concepts/security-encryption.md): the envelope format, key derivation,
  and what the manifest does and does not carry.
- [Recovering a pre-v2 envelope](./recover-legacy-envelope.md): the other reason a decrypt is
  refused.
- [Key rotation](../guides/key-rotation.md): moving artifacts onto a new key while access is intact.
- [Enable encryption](../guides/enable-encryption.md): turning encryption on for a repository.
- [Verify backup integrity](../guides/verify-backup-integrity.md): what a hash check does prove.
- [`sentinel security` reference](../reference/cli/security.md): every subcommand and flag.
- [Failed backup triage](./failed-backup-triage.md): when the failure is not encryption-related.
- [Troubleshooting](./troubleshooting.md): symptom index across all operations pages.

{/* sources: internal/adapters/crypto/key.go, internal/adapters/crypto/decrypt.go, internal/adapters/crypto/envelope.go, internal/cli/security.go, internal/cli/security_reencrypt.go, internal/cli/security_secrets.go, internal/cli/backup_verify.go, internal/cli/backup.go, internal/cli/restore.go, internal/cli/crypto_errors.go, internal/config/secrets_file_crypto.go, internal/ports/encryption.go, docs/runbooks/key-loss-incident.md */}
