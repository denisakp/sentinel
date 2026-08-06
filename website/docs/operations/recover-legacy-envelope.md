---
title: Recovering a pre-v2 envelope
description: "Sentinel refuses artifacts written before envelope v2: how to get the plaintext out, and why re-wrapping cannot restore confidentiality."
sidebar_position: 8
---

A restore is refused because the artifact predates Sentinel's envelope v2 format.

## Symptoms

A restore stops before staging anything. The message names the job and the artifact path:

```
refusing to decrypt legacy (pre-v2) envelope for backup "nightly-pg" from "/backups/nightly-pg.sql"
```

It continues with the two ways forward: re-encrypt from source, or pass `--allow-legacy-envelope` to
proceed at your own risk. Underneath it is the crypto-layer sentinel, which you will see in logs and
in wrapped errors:

```
crypto: legacy (pre-v2) envelope detected
```

The artifact does not begin with the envelope v2 header. Five bytes are enough to tell:

```bash
xxd -l 5 /backups/nightly-pg.sql
```

```
00000000: 5345 4e43 02                             SENC.      <- envelope v2
00000000: 2f01 0000 1f                             /....      <- legacy, no magic
```

The manifest sidecar agrees: `encryption.envelope_version` is `1` or absent rather than `2`. And a
re-encryption preview classifies the backup as `v1->v2`:

```
ID                                    JOB        MODE    FROM->TO  OUTCOME
9b2f1c04-7a3d-4e58-b1a0-6c4e2f9d8a11  nightly-pg  legacy  v1->v2   would_migrate
```

This is not a key problem. If you are here because a decrypt failed with
`authentication tag verification failed`, go to [Key loss incident](./key-loss-incident.md) instead.

## Before you start

You need the master key that was current when the artifact was written. The envelope version and the
key are independent: a legacy envelope under the wrong key fails for both reasons, and fixing the
envelope will not help.

Decide one thing before touching anything, because it determines which half of this page applies:
**can you still take a fresh backup from the source database?** If yes, that is the remediation, and
everything below about `security reencrypt` is a detour. If the source is gone, the PITR window has
closed, or this artifact is the last surviving copy, read on.

Capture the artifact's first bytes, its manifest, and its backup ID before any command runs. Note
also how classification works, because it decides what is even possible:

- `sentinel security reencrypt` classifies from the **manifest's** `encryption.envelope_version`, not
  from the artifact's bytes.
- A backup with no manifest is classified `skipped_unmigratable`. It cannot be migrated by the
  guided command, whatever its bytes say.
- A backup whose manifest has no `encryption` block is `skipped_unencrypted`, and needs nothing.

## Resolution

### 1. Confirm the artifact is genuinely legacy

Run the `xxd` check above and read the manifest's `encryption.envelope_version`. If the first five
bytes are `5345 4e43` followed by a version byte other than `0x02`, this is a different problem:
`crypto: unsupported envelope version 0x03` means the artifact came from a newer Sentinel than the
binary reading it. Upgrade the binary rather than reaching for the legacy flag.

### 2. Prefer a fresh backup from source

Every current Sentinel build writes envelope v2 automatically. Re-running the job against the live
database produces a sound artifact under a sound nonce scheme, with no exposure inherited from the
old ciphertext:

```bash
sentinel backup --config sentinel.yaml
```

This is the only remediation that restores the confidentiality guarantee. Everything else on this
page recovers availability.

### 3. Opt in once, deliberately, to get the plaintext out

If you need what is inside the legacy artifact, `sentinel restore` will decrypt it with an explicit
opt-in:

:::danger Destructive
`sentinel restore run` writes into the database named by the restore job. Restore a legacy artifact
into a scratch database, not over production. Confirm the job's `database` target first.
:::

```bash
sentinel restore run nightly-pg --config sentinel.yaml --allow-legacy-envelope
```

Sentinel writes a warning to stderr naming the backup and its path, stating that the artifact was
produced with a flawed nonce scheme and that the plaintext should be treated as recovered rather than
as safe to keep. It also emits a structured log record with `event=crypto.legacy_envelope_decrypt`,
so the opt-in is auditable after the fact.

The flag can also be defaulted on by the environment. `SENTINEL_ALLOW_LEGACY_ENVELOPE` set to `1`,
`true`, `TRUE`, `yes`, or `YES` turns it on for `sentinel restore` and for `sentinel backup verify`.

:::caution Do not leave the environment variable set
It converts a deliberate one-time exception into a silent default for every restore on that host,
which is exactly the audit trail you will want during the next incident. Set the flag on the single
command that needs it.
:::

Note that the flag has no effect on `sentinel backup verify`. That command only re-hashes stored
bytes and never decrypts, so it neither refuses a legacy envelope nor accepts one; the flag is
registered but inert. Tracked in
[#164](https://github.com/denisakp/sentinel/issues/164).

### 4. When the source is gone, migrate the artifact with `security reencrypt`

The guided command re-wraps a legacy artifact into the current envelope so it stops needing
`--allow-legacy-envelope`. Legacy migration is its **default** mode.

:::warning Always pass `--config` explicitly
Unlike most Sentinel commands, `security reencrypt` falls back to `$HOME/.sentinel/config.yaml`
rather than `./sentinel-config.yaml` when `--config` is omitted, and it loads the file without
running schema validation. On a host where both files exist you can silently migrate against the
wrong repository. Tracked in [#171](https://github.com/denisakp/sentinel/issues/171).
:::

Preview first. A dry run classifies and reports without staging or mutating anything:

```bash
sentinel security reencrypt --all --dry-run --config sentinel.yaml
```

Read the outcome column before going further:

| Outcome | Meaning |
|---|---|
| `would_migrate` | Legacy, eligible. This is what you are looking for. |
| `skipped_already_current` | Already envelope v2. The command is idempotent; nothing to do. |
| `skipped_unencrypted` | The manifest has no `encryption` block. Nothing to migrate. |
| `skipped_unmigratable` | No manifest could be read. The guided command cannot help; see below. |
| `failed` | An operational error. The original artifact is untouched. |

Then migrate. A single backup ID needs no confirmation; a bulk `--all` mutation requires `--yes`, and
can be narrowed with `--job` and `--since`:

```bash
# One backup.
sentinel security reencrypt 9b2f1c04-7a3d-4e58-b1a0-6c4e2f9d8a11 --config sentinel.yaml

# Every legacy backup for one job, in the last 90 days.
sentinel security reencrypt --all --yes --job nightly-pg --since 90d --config sentinel.yaml
```

The pipeline is write-new, then verify by decrypting the new artifact back and comparing plaintext
hashes, then swap. An interrupted run therefore never leaves you without a recoverable artifact:
either the original is still in place, or a verified replacement is. `--keep-original` writes the new
artifact to a sibling path and leaves the original alone, for a manual cutover.

One behaviour is worth knowing and is not in the command's help text: `--new-key-env` is accepted in
legacy mode as well as rotate mode, which migrates the envelope and re-keys the artifact in a single
pass. Tracked in [#171](https://github.com/denisakp/sentinel/issues/171).

### 5. Accept what migration does not fix

:::danger Re-wrapping does not undo the exposure
`security reencrypt` migrates the artifact's **format and availability**. It cannot undo the
confidentiality weakness of the pre-v2 envelope. The legacy nonce scheme was flawed, so a pre-v2
ciphertext may already be compromised; encrypting the same plaintext again, correctly, changes
nothing about a copy someone already holds. The command prints this caveat on every legacy run.

Where the source database still exists, re-running the backup is the real fix. Use migration only
where it does not, and only to get off the `--allow-legacy-envelope` dependency.
:::

An artifact classified `skipped_unmigratable` has no manifest, so there is no recorded salt, nonce, or
envelope version to migrate from. Recover its plaintext with step 3 if you can, then re-backup from
source or restore it into a scratch database and back that up.

## Verify recovery

The migrated artifact carries the v2 header:

```bash
xxd -l 5 /backups/nightly-pg.sql
```

```
00000000: 5345 4e43 02                             SENC.
```

A second dry run should now classify it as current rather than legacy:

```bash
sentinel security reencrypt --all --dry-run --config sentinel.yaml
```

```
ID                                    JOB        MODE    FROM->TO  OUTCOME
9b2f1c04-7a3d-4e58-b1a0-6c4e2f9d8a11  nightly-pg  legacy  v2->v2   skipped_already_current
```

Then confirm the artifact restores **without** `--allow-legacy-envelope`, into a scratch database.
That is the check that proves the migration, because `sentinel backup verify` only confirms the
stored bytes match the manifest and cannot tell you anything about decryptability.

## Prevent recurrence

Sweep for legacy artifacts rather than discovering them mid-incident. A repository-wide dry run is
non-mutating and safe to run on a schedule:

```bash
sentinel security reencrypt --all --dry-run --config sentinel.yaml
```

Keep `SENTINEL_ALLOW_LEGACY_ENVELOPE` out of service units, container specs, and CI environments. If
steady-state automation needs the flag, the artifacts behind it have not been remediated yet.

Keep manifest sidecars alongside their artifacts through every storage migration and retention pass.
An artifact whose `.manifest.json` has been lost is unmigratable and unverifiable, independently of
its envelope version.

## Related

- [Security and encryption](../concepts/security-encryption.md): the `SENC` v2 envelope, the nonce
  scheme, and what the manifest records.
- [Key loss incident](./key-loss-incident.md): when the failure is the key rather than the envelope.
- [Manifest](../concepts/manifest.md): the sidecar that carries the salt, IV, and envelope version.
- [Key rotation](../guides/key-rotation.md): `--mode rotate`, for current-format artifacts.
- [Restore](../concepts/restore.md): where preflight verification and decryption sit in the run.
- [`sentinel security` reference](../reference/cli/security.md): every subcommand and flag.
- [Troubleshooting](./troubleshooting.md): symptom index across all operations pages.

{/* sources: internal/adapters/crypto/envelope.go, internal/adapters/crypto/decrypt.go, internal/adapters/crypto/log.go, internal/cli/legacy_envelope.go, internal/cli/restore.go, internal/cli/security_reencrypt.go, internal/cli/backup_verify.go, internal/adapters/restore/runtime/preflight.go, internal/ports/encryption.go, docs/runbooks/recover-legacy-envelope.md */}
