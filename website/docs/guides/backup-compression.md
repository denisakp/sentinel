---
title: Compressing backup artifacts
description: "Turn on pipeline compression for MySQL and MariaDB dumps, choose gzip or zstd, and avoid the double-compression trap."
sidebar_position: 17
---

:::info Added in v1.3.0
Pipeline compression (the `compression:` block) requires Sentinel v1.3.0 or later. Engine-native compression options predate it.
:::

Shrink backup artifacts on disk and in transit by inserting a streaming compression stage between the
dump and the hash.

## When to use this

Use this for **MySQL and MariaDB**, whose dumps stream out as raw SQL text with no compression of
their own. SQL data typically compresses very well, so the artifact and every byte you later upload,
download or re-verify shrink together.

Use it for PostgreSQL or MongoDB only if you are not already using the dump tool's own compression.
`pg_dump` and `mongodump` compress natively, and Sentinel refuses to do both at once.

Do not reach for it to save CPU. Compression is opt-in precisely because it costs time on the machine
running the dump.

Do not expect it from the command line. There is no flag that enables pipeline compression; it exists
only as a configuration block. `sentinel backup --compress` is a different feature entirely, and is
covered below.

## Before you start

- A configuration file. Pipeline compression is configured per job or as a default, never with a
  flag.
- Nothing to install. Both codecs are pure Go, so no `gzip` or `zstd` binary is needed.
- An uncompressed baseline backup of the same database, if you want to measure the saving.
- For PostgreSQL and MongoDB jobs, knowledge of whether `database_options` already asks the dump tool
  to compress. Look for `compress`, `pg_compression_algo` or `pg_compression_level` on a PostgreSQL job,
  and `gzip` on a MongoDB job.

## Steps

### 1. Enable it

`compression` sits on a backup job, or on `defaults` where every job without its own block inherits
it:

```yaml
version: "1.0"

defaults:
  compression:
    enabled: true
    algorithm: zstd
    level: 3

databases:
  prod-mysql:
    type: mysql
    host_env: PROD_DB_HOST
    username_env: PROD_DB_USER
    password_env: MYSQL_PASSWORD
    database: app
    output: prod-mysql.sql
    schedule: "0 2 * * *"
    compression:
      enabled: true
      algorithm: gzip     # this job overrides the default
      level: 6
    storage:
      type: local
      local_path: /var/backups/sentinel
```

| Key | Values | Default |
|---|---|---|
| `enabled` | `true`, `false` | `false`; nothing changes until you set it |
| `algorithm` | `gzip`, `zstd`, `none` | `zstd` when enabled and omitted |
| `level` | gzip `1` to `9`, zstd `1` to `19` | gzip `6`, zstd `3` |

Inheritance is whole-block, like retention: a job with any `compression:` block of its own inherits
nothing from `defaults`. `algorithm: none` means no pipeline compression even with `enabled: true`,
and combining the two is rejected rather than ignored.

Choose `zstd` unless something downstream must read the stored bytes with a stock `gzip`. It gives a
better ratio for the time spent. `lz4` and `snappy` are not implemented.

### 2. Validate

```bash
sentinel config validate --config sentinel.yaml
```

Validation is where the two compression mechanisms are kept apart:

```text
Error: invalid config "sentinel.yaml": backup 'pg-native': compression cannot be enabled together with engine-native compression (postgres compress/pg_compression_* or mongodb gzip); disable one to avoid double-compression
```

Pick one mechanism per job. Compressing ciphertext-like data twice costs CPU and gives back
nothing.

Out-of-range levels are caught here too: `compression.level for gzip must be between 1 and 9`.

### 3. Run a backup

```bash
sentinel backup --config sentinel.yaml
```

The pipeline order is load bearing:

```text
dump  ->  compress  ->  SHA-256  ->  encrypt  ->  upload
```

Compression comes before encryption because ciphertext does not compress. It comes before the hash
because the hash has to describe the bytes that actually land in storage.

The artifact keeps the filename you configured. No `.gz` or `.zst` suffix is appended, because the
manifest, not the extension, is what restore reads.

## Verify

### Compare the size

```bash
ls -l /var/backups/sentinel/prod-mysql.sql
```

Against an uncompressed run of the same database, expect a large reduction on SQL text.

### Read the manifest

```bash
cat /var/backups/sentinel/prod-mysql.sql.manifest.json
```

```json
{
  "hash": {
    "algorithm": "sha256",
    "value": "<digest of the stored bytes>",
    "plaintext_value": "<digest of the original dump>"
  },
  "compression": { "algorithm": "gzip", "level": 6 }
}
```

The `compression` block is what restore reads. `hash.value` covers the stored artifact, so with
compression on it is the digest of the compressed bytes, and with encryption also on it is the digest
of the ciphertext. `hash.plaintext_value` keeps the digest of the original dump, which is what lets
you prove the plaintext is unchanged across a re-encryption or a codec change.

A manifest with no `compression` block means the artifact is not pipeline compressed. Legacy backups
and natively compressed PostgreSQL and MongoDB dumps both look like this, and restore leaves them
alone.

### Confirm it still restores

```bash
sentinel backup verify <backup-id> --config sentinel.yaml
sentinel restore run <restore-job> --config sentinel.yaml
```

Restore needs no flag and no configuration. It stages the artifact, verifies the hash, decrypts if
required, then inserts a decompress stage chosen from `compression.algorithm` in the manifest, and
feeds the result to the restore tool. With both features on, the order is decrypt, then decompress.

## If it goes wrong

**`sentinel backup --compress` changed nothing on a MySQL job.** `--compress` is not this feature. It
sets engine-native compression: `compress` for PostgreSQL and `gzip` for MongoDB. For MySQL and
MariaDB, the two engines pipeline compression exists for, the flag is read and then does nothing at
all. Use the `compression:` block.

**`-c` did something unexpected.** On `sentinel backup`, `-c` is the shorthand for `--compress`. On
`sentinel monitor`, `sentinel retention`, `sentinel config` and `sentinel repair` it is the shorthand
for `--config`. `sentinel backup -c sentinel.yaml` therefore does not select a configuration file.
Always spell `--config` out on `backup`.

**A run produced a doubly compressed artifact even though validation forbids it.** Flag overrides are
applied after the configuration has been validated, so
`sentinel backup --config sentinel.yaml --compress` sets engine-native compression on every
PostgreSQL and MongoDB job in the file without re-running the double-compression check. The result is
a `pg_dump`-compressed stream compressed again by the pipeline. Do not combine `--compress` with a
config that enables `compression`.

**The artifact is not smaller and the manifest has no `compression` block.** The most likely cause is
that the pipeline never had a local artifact to work on. The auto-discovery `strategy: single` path
writes its dump-all output straight to remote storage rather than through a staging directory, so the
whole artifact-security stage, compression and manifest included, is skipped. Use
`strategy: individual`, or local storage. The same restriction is what makes encryption refuse that
combination outright.

**Restore fails with a parse error, or writes garbage.** The `.manifest.json` sidecar was not staged
next to the artifact, so the decompress stage never ran and compressed bytes went straight to `mysql`
or `psql`. A missing sidecar is deliberately not an error, which is what makes this look like
corruption. Keep artifact and sidecar together whenever you copy or move backups by hand.

**You want to stop compressing.** Set `enabled: false` or remove the block. Existing compressed
artifacts stay restorable, because each one carries its own manifest describing how to undo it.

## Related

- [Backup](../concepts/backup.md): the pipeline this stage joins.
- [Backup manifests](../concepts/manifest.md): the sidecar that makes restore automatic.
- [Encryption at rest](../concepts/security-encryption.md): the stage that runs immediately after
  compression.
- [Enabling encryption](./enable-encryption.md): turning on the stage this one feeds.
- [Verifying backup integrity](./verify-backup-integrity.md): what `hash.value` and
  `hash.plaintext_value` are checked against.
- [Restoring from Google Cloud Storage](./restore-from-gcs.md): a restore that decompresses without
  being told to.
- [Storage backends](../concepts/storage-backends.md): why remote artifacts are staged locally first.
- [Configuration reference](../reference/configuration.md): the `compression` keys.
- [`sentinel backup`](../reference/cli/backup.md): `--compress` and the `--pg-compression-*` flags.

<!-- sources: internal/config/types.go, internal/config/loader.go, internal/config/validator.go, internal/adapters/compress/compress.go, internal/domain/backup/pipeline.go, internal/cli/backup_factory.go, internal/cli/backup.go, internal/adapters/restore/runtime/executor.go, internal/ports/manifest.go, docs/runbooks/backup-compression.md -->
