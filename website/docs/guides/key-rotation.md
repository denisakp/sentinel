---
title: Rotating the encryption key
description: Move to a new master key without stranding the artifacts encrypted under the old one.
sidebar_position: 5
---

Replace the master key your backups are encrypted under, keeping every existing artifact readable
throughout.

## When to use this

Use this on a scheduled rotation, after a suspected key compromise, or at an operator handover where
someone who is leaving has seen the key. Rotation is also the tail end of recovering from an exposed
bucket: encrypting under a key an attacker may hold is not encryption.

Do not use this to recover from a key you have already lost. Rotation requires the current key in
order to read what it is rotating; if it is gone, the artifacts are gone, and the only path forward
is a fresh backup from a live source. Do not use it to re-wrap pre-v2 envelopes either, which is
`sentinel security reencrypt --mode legacy` and a different procedure.

## Before you start

- The current key, resolvable right now. Verify it before you change anything, with
  `sentinel backup verify <backup-id> --config sentinel.yaml` against a recent artifact.
- A secret store that lets you hold two keys at once and inject both into one process.
- An inventory of everything that reads artifacts written under the current key: scheduled backup
  jobs, restore jobs, and any out-of-band tooling.
- Enough free space alongside your artifacts for one re-encrypted copy at a time. Re-encryption
  writes the new artifact, verifies it, and only then swaps.

One structural fact determines the whole shape of this procedure. Key resolution is repository wide.
`encryption_key_env` and `encryption_key_file` are top-level configuration keys, and there is no
per-job and no per-restore-job override anywhere in the schema. At any moment, one configuration file
means one key.

:::danger Rotation rewrites live artifacts
`security reencrypt` mutates the artifacts your recovery depends on. Run it against a copy or with
`--keep-original` the first time, and never start a rotation you cannot finish, because a half
rotated repository needs both keys to read.
:::

## Steps

### 1. Preserve the current key under a second name

Before anything else, make sure the current key survives under a name that the rotation will not
overwrite:

```bash
export SENTINEL_MASTER_KEY_OLD="$SENTINEL_MASTER_KEY"
```

Write `SENTINEL_MASTER_KEY_OLD` into your secret store now, as a real entry rather than a shell
variable. Everything that follows assumes you can still produce it.

### 2. Generate the new key

```bash
sentinel security init-key --force
```

Store the printed value in your secret store as a new entry. Do not overwrite the old one.

:::caution `--force` is not the safety net it appears to be
The guard that `--force` overrides only checks the hard-coded environment variable
`SENTINEL_MASTER_KEY`. If your configuration names a different variable, `init-key` prints a fresh
key with no warning and no `--force` needed. When the guard does fire, the command exits `0`, so a
wrapper script cannot tell a refusal from a success. Read the output; do not infer from the exit
code.
:::

### 3. Re-encrypt existing artifacts under the new key

This is the step that makes the rest simple, because once it completes there is only one key in play.
`security reencrypt --mode rotate` decrypts each artifact with the key named by the configuration's
`encryption_key_env` and re-encrypts it under the variable named by `--new-key-env`. Both keys must
be resolvable for the whole run.

Preview first. A dry run classifies and reports, and mutates nothing:

```bash
export SENTINEL_MASTER_KEY="$SENTINEL_MASTER_KEY_OLD"   # still the current key
export SENTINEL_MASTER_KEY_NEW='<the key from step 2>'

sentinel security reencrypt --all --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEW --dry-run --config sentinel.yaml
```

A banner confirms nothing was modified, followed by the report it would produce for real:

```
ID                                    JOB            MODE    FROM->TO  OUTCOME
3f1c9a20-5b7e-4d21-9c88-0a2f6e1d4b93  prod-postgres  rotate  v2->v2    would_rotate
1 processed · 0 migrated · 1 rotated · 0 skipped · 0 failed
```

Read the outcome column before continuing. `skipped_unencrypted` means an artifact was never
encrypted, and `skipped_legacy` means it is a pre-v2 envelope that rotate mode will not touch;
migrate those with `--mode legacy` first.

Then apply it. A bulk mutation additionally requires `--yes`; a single backup ID does not:

```bash
# One artifact, to build confidence.
sentinel security reencrypt <backup-id> --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEW --config sentinel.yaml

# Everything, keeping the originals in place for a manual cutover.
sentinel security reencrypt --all --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEW --keep-original --yes --config sentinel.yaml
```

Scope a bulk run with `--job <name>` or `--since <30d|4w|720h>` when you want to rotate in batches.
The pipeline writes the new artifact, verifies it by decrypting it back and comparing plaintext
hashes, and only then swaps, so an interruption never leaves you without a recoverable copy.
`--keep-original` writes to a sibling path and leaves the original alone.

### 4. Cut the configuration over to the new key

Once the artifacts are rotated, put the new key's value into the variable the configuration already
names, and reload whatever holds it:

```yaml
encryption_key_env: SENTINEL_MASTER_KEY   # unchanged; the value behind it is new
```

New backups and scheduled runs encrypt under the new key from that point, with no configuration
change for jobs that already named the variable. If you would rather change the name than the value,
edit `encryption_key_env` and restart the scheduler so the new environment is picked up.

### 5. Restore an old artifact if you did not re-encrypt everything

Because there is no per-job key, you cannot leave a restore job pinned to the old key.

:::caution `encryption_key_env` inside a `restores:` job does nothing
No such field exists in the restore job schema, and Sentinel parses YAML loosely, so an
`encryption_key_env` nested under a restore job is silently dropped rather than rejected. The restore
still runs, still uses the repository-wide key, and fails the tag check with no hint about why. Do
not carry that pattern forward from older notes.
:::

There are two supported ways to read a pre-rotation artifact:

```bash
# Put the old key into the variable the configuration names, for this run only.
SENTINEL_MASTER_KEY="$SENTINEL_MASTER_KEY_OLD" \
  sentinel restore run legacy-restore --config sentinel.yaml
```

Or keep a second configuration file whose top-level `encryption_key_env` names
`SENTINEL_MASTER_KEY_OLD`, and pass it with `--config` for old-artifact restores. Retire it when the
last pre-rotation artifact ages out.

### 6. Retire the old key

Only once nothing references it: no artifact within your retention window still needs it, no restore
configuration names it, and step 3's report showed no failures. Then delete
`SENTINEL_MASTER_KEY_OLD` from your secret store.

## Verify

Confirm new backups use the new key, and that the artifact is still valid ciphertext:

```bash
sentinel backup --config sentinel.yaml
xxd -l 5 /var/backups/sentinel/<new-artifact>
```

```
00000000: 5345 4e43 02                             SENC.
```

Confirm nothing was damaged in transit across the repository:

```bash
sentinel backup verify --all --config sentinel.yaml
```

:::caution `backup verify` does not prove your key still opens the artifact
It hashes the stored bytes and compares them to the manifest. It never decrypts, so a rotated
artifact under the wrong key passes verification exactly as a correct one does. The `--all` sweep
catches corruption, not a key mismatch.
:::

The only check that proves decryptability is one that actually decrypts. Rehearse a restore:

```bash
sentinel restore dry-run <job-name> --config sentinel.yaml
```

## If it goes wrong

**`authentication tag verification failed` during rotation.** The key in `encryption_key_env` is not
the one this artifact was written under. Check that you exported the old value in step 3 and not the
new one. Sentinel cannot distinguish a wrong key from a corrupted file, so do not conclude the
artifact is damaged until you have ruled the key out.

**`--new-key-env` was rejected.** It is required in rotate mode and accepts a variable name only,
never a literal key, so that the key never reaches your shell history or the process listing.

**The run stopped partway through `--all`.** Some artifacts are on the new key and some on the old.
Both keys are still needed. Re-run the same command; already rotated artifacts report
`skipped_already_current` and are left alone.

**Everything reports `skipped_legacy`.** The repository predates envelope v2. Run
`sentinel security reencrypt --all --mode legacy` first to bring artifacts into the current format
under the same key, then rotate.

**The new key is compromised mid-rotation.** Treat it as a key incident and roll forward to a third
key. Do not revert to the previous one, which is now the key an attacker may have seen you rotate
away from.

**You cannot produce the old key.** Stop. Nothing on this page will help, and running rotation will
not either. Artifacts encrypted under a key you do not have are unrecoverable; plan a fresh backup
from the live source under the new key.

## Related

- [Enabling backup encryption](./enable-encryption.md): first-time setup and the opt-in rule.
- [Security and encryption](../concepts/security-encryption.md): the envelope format, key
  derivation, and why key loss is final.
- [Verifying backup integrity](./verify-backup-integrity.md): what `backup verify` proves.
- [Integrity sweep](./integrity-sweep.md): running verification across the whole repository.
- [`sentinel security` reference](../reference/cli/security.md): every flag on `reencrypt`.
- [`sentinel restore` reference](../reference/cli/restore.md): the restore commands used above.

{/* sources: internal/cli/security.go, internal/cli/security_reencrypt.go, internal/adapters/crypto/key.go, internal/adapters/crypto/envelope.go, internal/adapters/restore/runtime/executor.go, internal/scheduler/restore_integration.go, internal/config/types.go, internal/config/restore_types.go, internal/config/loader.go, docs/runbooks/key-rotation.md */}
