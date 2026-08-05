---
title: Verifying one backup
description: Re-hash a single stored artifact against its manifest, read the exit code, and interpret a missing manifest correctly.
sidebar_position: 15
---

Prove that one specific backup artifact still holds the bytes it held when it was written.

## When to use this

Use this immediately before restoring from an artifact, after any storage incident that could have
touched it, and whenever you are asked to demonstrate that a particular backup is intact. It reads
the artifact and compares it against the SHA-256 recorded in the artifact's own manifest sidecar, so
it detects truncation, silent corruption, and editing.

Use [Sweeping a repository for corruption](./integrity-sweep.md) instead when the question is about
every backup rather than one. Do not use either to prove that a backup restores; a byte-identical
artifact can still contain a dump of an empty database. That is a restore rehearsal, not an
integrity check.

## Before you start

- The execution ID of the backup. Get it from
  [Inspecting execution history](./inspect-monitor-history.md); it is the `ID` column of
  `sentinel monitor list`. It is not the job name, and it is not the `backup_id` field inside the
  manifest, which confusingly holds the job name.
- A configuration file whose `history_db_path` points at the history that recorded the run.
- For an artifact on S3, GCS, Azure, or Google Drive: the job must still exist in that configuration
  under the same name, because the storage credentials are looked up by job name. A renamed or
  deleted job leaves the artifact unverifiable even though it is still in the bucket.
- Enough temporary disk for one copy of a remote artifact. It is downloaded to a temporary directory,
  verified there, and the directory is removed before the command returns.

No encryption key is needed. Verification hashes the bytes as stored, which for an encrypted job is
the ciphertext, and compares them against the ciphertext digest the manifest recorded. Nothing is
decrypted, and nothing is written to the history.

## Steps

### 1. Find the execution ID

```bash
sentinel monitor list --config sentinel.yaml --last 24h 2>&1
```

The `2>&1` is required; `monitor list` prints to stderr. Copy the `ID` of the row you care about.

### 2. Run the check

```bash
sentinel backup verify 019a5c3f-... --config sentinel.yaml --output text
```

```
PASS: Backup 019a5c3f-... integrity verified
  Database: prod-postgres
  File:     /var/backups/prod-postgres.backup
  Hash:     9f2c… (sha256)
  Status:   PASS
```

`--output text` is worth passing explicitly. With no `--output`, the format falls back to the
configuration's `log_format`, which defaults to `json`, so the plain command emits a JSON object
rather than the block above. Both go to stdout, so a redirect works here.

### 3. Read the exit code, not just the text

```bash
sentinel backup verify 019a5c3f-... --config sentinel.yaml --output text
echo "exit=$?"
```

| Exit | Result | Meaning |
|---|---|---|
| `0` | `PASS` | The artifact hashes to the value in its manifest. |
| `1` | `FAIL` | Hash mismatch. The artifact is not what was written. |
| `2` | error | No execution with that ID in the history database. |
| `3` | `skipped` | No manifest sidecar, so integrity could not be checked at all. |
| `4` | error | The check could not run, or the artifact itself is gone. |

Exit `4` covers two different situations, which is worth knowing before you wire an alert to it. A
config or backend failure exits `4`, and so does an artifact that has been deleted while its
manifest survived. The repository sweep classifies that second case as `missing_artifact` and exits
`5`; the single-ID command does not distinguish it.

Exit `3` is not a pass. It means the question was never asked.

### 4. Compare against the previous backup when something looks off

If a backup is suspiciously large, slow, or you suspect the security posture changed, compare the two
recorded metadata sets. No artifact bytes are read.

```bash
sentinel backup diff 019a5b21-... 019a5c3f-... --config sentinel.yaml --output text
```

```
FIELD        BEFORE  AFTER  DELTA
duration_ms  1234    2000   ⚠ +62%
```

Size and duration swings are marked and stay at exit 0. A security regression, such as encryption
turning off or the hash algorithm changing, exits non-zero so CI can gate on it. See the
[`sentinel backup` reference](../reference/cli/backup.md) for the full field list.

### 5. Catch corruption at write time instead

Everything above checks after the fact. `verify_after_upload` re-downloads the artifact from its
storage backend the moment the upload completes, re-hashes it against the manifest, and fails the
backup rather than reporting success on a silently truncated upload.

```yaml
integrity:
  verify_after_upload: true      # default for every job

databases:
  prod-postgres:
    output: prod-postgres.backup
    storage:
      type: s3
      s3_bucket: my-backups
    verify_after_upload: true    # per-job override, wins over the default
```

It is opt in and off by default, because it doubles read I/O and adds egress cost proportional to
artifact size. Its value is on remote backends, where a network write can be silently truncated. On
mismatch the job is recorded and notified as a failure, and the suspect object is deliberately left
in place rather than deleted, so it can be pulled for forensics before retention removes the last
good copy.

It is silently a no-op for any job that has no `output:` key, for the same reason described below.

## Verify

A `PASS` line with exit 0 is the result. Confirm the two hashes agree by eye in JSON output, which
prints both:

```bash
sentinel backup verify 019a5c3f-... --config sentinel.yaml --output json
```

```json
{
  "backup_id": "019a5c3f-...",
  "computed_hash": "27af69f5…",
  "database": "prod-postgres",
  "file_path": "/var/backups/prod-postgres.backup",
  "result": "pass",
  "stored_hash": "27af69f5…",
  "verified_at": "2026-08-05T18:48:27Z"
}
```

`stored_hash` comes from the manifest, `computed_hash` from the artifact as it exists now. Nothing is
written to the history database by this command, so re-running it is free and leaves no trace.

## If it goes wrong

**`Warning: no manifest found ... (pre-v1.1 backup)`, exit 3, on a backup you know is recent.** The
message names the wrong cause most of the time. A backup job whose configuration omits `output:`
produces no manifest at all: the dump engine invents a filename internally, the pipeline never
learns it, manifest writing is skipped without a warning, and the history row records the artifact
path as `unknown`. Confirm with `sentinel monitor show --id <id> 2>&1` and look at the `File:` line.
The fix is to set `output:` on every job; existing artifacts cannot be given a manifest afterwards,
because the bytes it would have been computed from are gone. Scheduled runs are not affected, since
the scheduler fills in a timestamped name. Tracked as issue #151.

**`FAIL ... hash mismatch`, exit 1.** Treat the artifact as untrusted and do not restore from it.
Take a fresh backup from the source database, and keep the suspect file for forensics rather than
deleting it. Find the last artifact that still verifies with
[the repository sweep](./integrity-sweep.md). If the artifact is remote, the corruption most likely
happened in transit or in the bucket, which is exactly what `verify_after_upload` exists to catch at
write time.

**`backup artifact not found in storage`, exit 4.** The manifest survived and the artifact did not.
Look at storage lifecycle rules and at your retention policy before assuming the deletion was
accidental.

**`remote verify not supported for storage type ...`, or a credentials error on a remote artifact.**
The job's storage block is resolved from the current configuration by job name. If the job was
renamed or removed, Sentinel has no credentials for the bucket the artifact lives in.

**`--allow-legacy-envelope` appears to do nothing.** It does nothing. The flag is registered on
`backup verify` and is never read by the command; verification hashes stored bytes and never
decrypts, so there is no envelope for it to affect. It is accepted silently, including when set
through `SENTINEL_ALLOW_LEGACY_ENVELOPE`. The flag is live on `sentinel restore`, which does decrypt.

## Related

- [Inspecting execution history](./inspect-monitor-history.md): where the execution ID comes from.
- [Sweeping a repository for corruption](./integrity-sweep.md): the same check across every recorded backup.
- [Manifests and integrity](../concepts/manifest.md): what the sidecar records and why the hash is computed on the way out.
- [Enabling backup encryption](./enable-encryption.md): why an encrypted artifact still verifies without a key.
- [`sentinel backup` reference](../reference/cli/backup.md): every `verify` and `diff` flag.
- [`sentinel repair` reference](../reference/cli/repair.md): repository-wide drift, including orphan manifests.
- [Configuration reference](../reference/configuration.md): `output:`, `integrity.verify_after_upload`, and `log_format`.

<!-- sources: internal/cli/backup_verify.go, internal/cli/backup_diff.go, internal/cli/exit_codes.go, internal/cli/legacy_envelope.go, internal/cli/backup_factory.go, internal/adapters/manifest_store/store.go, internal/domain/backup/executor.go, internal/domain/backup/pipeline.go, internal/config/types.go, internal/config/loader.go, docs/runbooks/verify-backup-integrity.md -->
