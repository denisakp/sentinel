---
title: Manifests and integrity
description: The .manifest.json sidecar Sentinel writes beside every artifact, what it records, and how backup verify and validate-chain read it.
sidebar_position: 7
---

A manifest is a small JSON file written alongside a backup artifact, named `<artifact>.manifest.json`.
It records the artifact's SHA-256 fingerprint, its size, the compression and encryption parameters
applied to it, and its position in an incremental chain. It is the only durable statement of what the
artifact was at the moment it was written, and it is what `sentinel backup verify` compares against
months later.

## Why it exists

A backup file on disk tells you nothing about itself. It cannot tell you whether the last four
megabytes were truncated by a full disk, whether an S3 multipart upload silently dropped a part,
whether someone edited it, or whether it is the second link in a chain that is useless without the
first. The execution history in SQLite records that a backup happened; it does not travel with the
artifact, so a file copied to another host, or recovered from a bucket after the history database was
lost, arrives with no provenance at all.

The manifest is the provenance, and it travels with the artifact. Because it sits next to the file
rather than in a central database, an artifact plus its sidecar is self-describing: enough to check
integrity, enough to know whether it needs decrypting and how, and enough to know what else has to be
restored with it.

## How it works

The manifest is produced at the end of a backup run, in the pipeline stage that follows the dump. By
that point the artifact has been dumped, optionally compressed, and optionally encrypted, so every
parameter the manifest records is already settled.

### What it records

```json
{
  "backup_id": "prod-postgres",
  "database": "myapp_prod",
  "database_type": "postgres",
  "created_at": "2026-08-05T02:00:11Z",
  "size_bytes": 41982119,
  "hash": {
    "algorithm": "sha256",
    "value": "9f2c…",
    "plaintext_value": "1ab7…"
  },
  "compression": { "algorithm": "zstd", "level": 3 },
  "encryption": {
    "algorithm": "AES-256-GCM",
    "key_derivation": "PBKDF2-HMAC-SHA256",
    "iterations": 100000,
    "salt": "…",
    "iv": "…",
    "envelope_version": 2
  },
  "advanced_restore": {
    "capabilities": ["full", "incremental"],
    "incremental_lineage": {
      "enabled": true,
      "chain_id": "…",
      "chain_index": 2,
      "max_chain_depth": 6,
      "baseline_backup_id": "…",
      "required_backup_ids": ["…"],
      "engine": "postgres",
      "execution_supported": true
    }
  }
}
```

Two fields deserve attention because their names mislead. `backup_id` is the **job** name, not the
execution ID you pass to `backup verify`; the two are different identifiers. And `hash.value` is the
digest of the bytes **as stored**, after compression and after encryption, while
`hash.plaintext_value` is the digest of the raw dump before either was applied. Verification compares
the stored bytes against `hash.value`, which is why it never needs the encryption key.

The `compression` and `encryption` blocks are omitted entirely when those stages did not run. A
manifest with no `compression` block means the artifact is not pipeline-compressed, and restore
performs no decompression stage; there is no operator flag involved, restore reads the algorithm from
the manifest.

The file is written with mode `0600` and pretty-printed JSON. Unknown fields are tolerated on read,
so a manifest written by a newer Sentinel does not break an older one.

### The hash is computed on the way out, not on the way back

Nothing in the write path opens the finished artifact to hash it. Each stage that writes bytes wraps
its destination in a `HashingWriter`, which forwards every byte to the underlying writer and feeds the
same bytes into a running SHA-256 state in one pass. The compression stage produces the compressed
digest as a side effect of compressing; the encryption stage produces the ciphertext digest as a side
effect of encrypting. Whichever stage ran last supplies `hash.value`.

This matters for a reason beyond speed. A digest computed by re-reading the file afterwards is a
digest of whatever is on disk at read time, which is precisely the thing you are trying to detect a
change in. Hashing the bytes as they pass through pins the fingerprint to what was written.

:::caution The dump itself is buffered in memory
The plaintext digest is computed in a single pass, but over a dump that has been captured whole into
an in-memory buffer first. Sentinel's peak memory use during a backup is therefore proportional to
the uncompressed dump size. Compression and encryption stream properly; the dump stage does not.
:::

### Verifying one backup

```bash
sentinel backup verify <backup-id> --config sentinel.yaml
```

The argument is an execution ID from the history database, which `sentinel monitor list` prints.
Sentinel looks the execution up, derives the manifest path by appending `.manifest.json` to the
recorded artifact path, re-computes the artifact's SHA-256, and compares it against `hash.value`.

For a remote artifact the recorded path is an object key or URI rather than something openable, so the
artifact and its sidecar are downloaded into a temporary directory, verified there, and the directory
is removed before the command returns. Nothing is left behind and nothing is written to the history.

Exit codes are stable and distinct, so a script can branch on them:

| Exit | Meaning |
|---|---|
| `0` | Hash matches the manifest. |
| `1` | Hash mismatch: the artifact has changed since it was written. |
| `2` | The backup ID is not in the history database. |
| `3` | No manifest for this backup; integrity could not be checked. |
| `4` | The check itself could not run: config, history database, or storage backend. |

### Verifying the whole repository

```bash
sentinel backup verify --all --config sentinel.yaml
```

`--all` enumerates every **successful** execution in the history and verifies each one against its own
storage backend, sequentially. A backup ID and `--all` are mutually exclusive; passing both, or
neither, is a usage error. The sweep is read-only.

Each backup lands in exactly one of four states:

| Status | Meaning |
|---|---|
| `ok` | Artifact fetched, hash matches the manifest. |
| `corrupted` | Artifact fetched, hash no longer matches. |
| `missing_artifact` | The manifest is there, the artifact object is gone. |
| `missing_manifest` | The artifact exists but has no manifest, so it cannot be verified. |

A summary line closes the report:

```
12 checked · 10 ok · 1 corrupted · 0 missing_artifact · 1 missing_manifest
```

The sweep returns a single exit code: `0` when everything is `ok`, `5` when at least one backup is a
genuine integrity problem, and `4` when the check could not run. Separating `5` from `4` is
deliberate, so an alert can distinguish "the backups are broken" from "the checker is broken".
`--since 30d` bounds the sweep by recency, `--job prod-postgres` bounds it to one job,
`--output json` emits a `results` array plus a `summary` object, and `--ignore-missing-manifest`
downgrades `missing_manifest` from an integrity failure to a warning.

:::caution `--allow-legacy-envelope` does nothing here
The flag is registered on `backup verify` but never read. Verification hashes stored bytes and never
decrypts, so there is no envelope for it to affect. It is accepted silently and has no effect.
:::

### Lineage: what makes a chain checkable

For an incremental backup the manifest carries an `advanced_restore.incremental_lineage` block. Four
fields carry the structure:

- `chain_id` groups the artifacts that belong to one chain.
- `chain_index` is the artifact's position, `0` for the baseline full backup.
- `baseline_backup_id` names the full backup the chain rests on.
- `required_backup_ids` lists what must be present for this artifact to be restorable.

`sentinel restore validate-chain <job-name>` reads these without restoring anything. It plans the
restore from the manifest at the job's configured backup path, then walks the resolved artifacts
through the chain resolver, which rejects a chain on any of these grounds:

| Rejection | Cause |
|---|---|
| `rule_1_baseline_missing` | No artifacts, or the first one is not at `chain_index: 0`. |
| `rule_1_baseline_manifest_missing` | The baseline has no manifest. |
| `rule_2_intermediate_missing` | A link in the chain has no backup ID or no manifest. |
| `rule_3_baseline_mismatch` | A link names a different baseline from the one at index 0. |
| `rule_4_non_contiguous_chain_index` | The indices have a gap, so a link is absent. |
| `rule_5_timeline_divergence` | A link's timeline ID differs from the baseline's. |
| `rule_6_target_not_in_chain` | The requested target is not one of the resolved artifacts. |
| `rule_7_hash_verification_failed` | A link's artifact did not verify against its manifest. |

Before those rules run, the planner can reject the request outright with a reason code:
`unsupported_database_type` for any engine other than PostgreSQL,
`missing_advanced_metadata` when the manifest has no lineage block,
`missing_incremental_baseline` when no baseline is named, and
`incompatible_incremental_baseline` when the configured `incremental_from_backup` does not match the
baseline the manifest records.

There is also a shallower check, applied by `sentinel repair` rather than by `validate-chain`: a
lineage block must have a non-empty `chain_id`, a `chain_index` of zero or more, and a
`max_chain_depth` of zero or more. That check is a shape check, not a continuity check.

## Known defects

You will meet these in normal use.

### A job with no `output:` gets a manifest, since the fix for issue #151

The dump engine's argument builder invents a name (`SENTINEL_<timestamp>` plus the engine's
extension) when `output:` is unset, and it reports that path back. The pipeline uses it, so the
manifest is written and the history row records the real artifact.

**On v1.4.0 and earlier the name was not propagated.** The pipeline resolved an empty artifact path,
skipped manifest creation entirely, and emitted no warning. The history row recorded the artifact as
`unknown`, and `sentinel backup verify` classified the backup as `missing_manifest` (exit `3`, or a
`--all` integrity failure), so it could never verify. Scheduled runs escaped it, because
`sentinel schedule` filled in a timestamped name before the job ran; the defect bit
`sentinel backup --config …` runs of a job whose `output:` was unset.

That is why the old advice was to set `output:` on every job. **Do not follow it on a current
version**: a literal `output:` makes every run overwrite the previous artifact (see below), and it is
no longer needed for integrity.

### A literal `output:` makes every run overwrite the previous artifact

`output:` is used **verbatim**. A job with `output: shop.sql` writes `shop.sql` on every run,
truncating what was there before. The result is exactly one backup file, permanently overwritten:
retention has nothing to prune, `keep_last: 30` keeps one, the history accumulates rows all pointing
at the same path whose contents are whatever the last run produced, and every member of an
incremental chain resolves to the same file.

Until the fix for #151 this was a trap, because `output:` was also the only way to get a manifest, so
the guidance for integrity pushed you straight into it. It no longer is: omit `output:` and you get a
unique name **and** a manifest.

If you want a fixed prefix or directory, use a placeholder:

```yaml
output: shop-{timestamp}.sql   # shop-2026-01-02T15-04-05.sql
output: shop-{date}.sql        # shop-2026-01-02.sql
```

Placeholders expand in UTC. A value with no placeholder keeps its literal meaning, so existing
configurations produce the filenames they always have ([#193](https://github.com/denisakp/sentinel/issues/193)).

### Point-in-time recovery can never be planned

The pipeline hard-codes the manifest's capability list to exactly `["full", "incremental"]`. The
restore planner requires `"pitr"` to be present in that list before it will plan a point-in-time
recovery, and it also requires `recoverable_window_start_utc` and `recoverable_window_end_utc`, which
nothing ever writes. A job configured with `restore_mode: pitr` is therefore always rejected with
reason code `missing_advanced_metadata`, regardless of the database, the WAL configuration, or the
timestamp requested. The `postgres_recovery` block exists in the manifest schema and is never
populated.

There is no configuration that works around this. PITR is not usable in the current release. Tracked
as issue #148.

## Per-engine behaviour

The manifest schema is identical for every engine. Which lineage fields get populated is not.

| Engine | Lineage fields populated for an incremental backup | Notes |
|---|---|---|
| PostgreSQL | `chain_id`, `chain_index`, `baseline_backup_id`, `required_backup_ids`, `engine` | No side-artifact fields: change data lives in the WAL and is combined at restore time. The only engine `restore validate-chain` accepts. |
| MySQL | The above, plus `binlog_start_file`, `binlog_end_file`, `binlog_artifacts` | The binlog fields name the archive written beside the artifact. |
| MariaDB | Identical to MySQL | The two engines share one pipeline path. |
| MongoDB | The above, plus `oplog_artifact_path` | Names the oplog archive captured alongside the dump. |

Several declared lineage fields are never written by any engine, including
`compatible_target_fingerprint`, `timeline_id`, `binlog_start_pos`, `binlog_end_pos`,
`oplog_ts_start`, `oplog_ts_end`, `checksum_state`, `wal_summary_start_lsn`, and
`wal_summary_end_lsn`. They are reserved schema, not data. One consequence is visible above: the
chain resolver's `rule_5_timeline_divergence` cannot fire, because `timeline_id` is always empty.

## Configuration

Manifests are not configurable; they are written for every job that has a resolvable artifact path.
Three keys change what a manifest contains or when it is checked.

```yaml
version: "1.0"
history_db_path: ~/.sentinel/history.db
encryption_key_env: SENTINEL_MASTER_KEY

integrity:
  verify_after_upload: true

databases:
  prod-postgres:
    type: postgres
    host_env: PROD_DB_HOST
    username_env: PROD_DB_USER
    password_env: PG_PASSWORD
    database: myapp_prod
    output: prod-postgres.backup      # required for a manifest to be written
    storage:
      type: s3
      s3_bucket: my-backups
    verify_after_upload: true
    compression:
      enabled: true
      algorithm: zstd
      level: 3
```

- `output:` must be set, per the defect above.
- `encryption_key_env:` causes the `encryption` block to be populated and makes `hash.value` the
  digest of the ciphertext rather than the plaintext.
- `compression:` populates the `compression` block; restore reads the algorithm back from it.
- `integrity.verify_after_upload:` re-downloads the artifact immediately after upload and re-hashes it
  against the manifest, failing the backup on mismatch rather than reporting success. It can be
  overridden per job.

Every key, with types and defaults, is in the [configuration reference](../reference/configuration.md).

## Example

Run a job, then check what it wrote:

```bash
sentinel backup --config sentinel.yaml
cat ./backups/prod-postgres.backup.manifest.json
```

Verify a specific execution, using the ID from the history:

```bash
sentinel monitor list --config sentinel.yaml --last 24h
sentinel backup verify 019a5c3f-… --config sentinel.yaml
```

```
PASS: Backup 019a5c3f-… integrity verified
  Database: prod-postgres
  File:     ./backups/prod-postgres.backup
  Hash:     9f2c… (sha256)
  Status:   PASS
```

Sweep the repository and act on the exit code:

```bash
sentinel backup verify --all --since 30d --output json --config sentinel.yaml
echo "exit=$?"
```

Exit `0` means every checked artifact still matches its manifest. Exit `5` means at least one does
not, and the `results` array names which.

## Failure modes

**`missing_manifest` on a backup you know succeeded.** Almost always the missing `output:` defect
above. Confirm by checking whether the history row's artifact path reads `unknown`. Set `output:`
and re-run; existing artifacts cannot be retrofitted with a manifest, because the bytes they were
written from are gone.

**`corrupted`.** The artifact no longer hashes to what the manifest recorded. Treat it as untrusted:
do not restore from it. Take a fresh backup from source, and keep the suspect artifact for forensics.
If it is a remote artifact, the corruption most likely happened in transit or in the bucket, which is
the case `verify_after_upload` exists to catch at write time.

**`missing_artifact`.** The manifest survived and the artifact did not. Look at storage lifecycle
rules and at retention before assuming deletion was accidental.

**Verification cannot run at all, exit `4`.** Config, history database, or storage backend. Nothing
has been proven about your backups either way; this is not an all-clear.

**`chain validation failed: status=rejected reason=missing_advanced_metadata`.** Either the manifest
predates lineage recording, or the job is asking for PITR, which cannot succeed. Check the requested
`restore_mode` before investigating the chain.

**A manifest write failure during backup.** This surfaces as a warning and the backup still succeeds.
The artifact is usable but unverifiable, which is worth chasing rather than filtering out of your
logs.

## Related

- [Backup](./backup.md): the run sequence that produces the manifest.
- [Restore](./restore.md): how the manifest is read back to plan and verify a restore.
- [Encryption and key management](./security-encryption.md): what the `encryption` block records.
- [Retention](./retention.md): what happens to an artifact and its sidecar when a policy expires them.
- [`sentinel backup` reference](../reference/cli/backup.md): every `verify` flag.
- [`sentinel restore` reference](../reference/cli/restore.md): `validate-chain` and the restore modes.
- [`sentinel repair` reference](../reference/cli/repair.md): repository-wide drift detection, including orphan manifests and broken chains.
- [Configuration reference](../reference/configuration.md): every YAML key.
- [Incremental backup with WAL](../tutorials/postgres/incremental-wal.md): chains and their lineage in practice.
- [Verify backup integrity](../guides/verify-backup-integrity.md): checking one artifact.
- [Integrity sweep](../guides/integrity-sweep.md): checking every artifact at once.

{/* sources: internal/domain/manifest/lineage.go, internal/adapters/manifest_store/store.go, internal/adapters/crypto/hash.go, internal/domain/backup/pipeline.go, internal/domain/backup/planner.go, internal/domain/backup/executor.go, internal/ports/manifest.go, internal/cli/backup_verify.go, internal/cli/exit_codes.go, internal/cli/backup_factory.go, internal/cli/restore.go, internal/cli/repair.go, internal/domain/restore/planner.go, internal/domain/restore/incremental/chain_resolver.go, internal/adapters/dump/pg/pg_dump.go, internal/adapters/dump/pg/output.go, internal/utils/default.go, internal/config/types.go, docs/runbooks/verify-backup-integrity.md, docs/runbooks/integrity-sweep.md */}
