---
title: Compression
description: "The two ways a Sentinel artifact gets smaller: the engine's own dump-tool compression and Sentinel's pipeline stage."
sidebar_position: 13
---

:::info Added in v1.3.0
Pipeline compression (the `compression:` block) requires Sentinel v1.3.0 or later. Engine-native compression options predate it.
:::

A Sentinel artifact can be compressed in two entirely separate ways. The dump tool can compress its own
output, which only PostgreSQL and MongoDB support. Or Sentinel can compress the finished artifact itself
in a pipeline stage, which works the same way for all four engines. They are configured by different
keys, recorded differently in the manifest, and reversed at different points during a restore. Enabling
both on the same job is rejected at configuration load.

## Why it exists

`mysqldump` and `mariadb-dump` emit plain SQL text. A dump that occupies eight gigabytes as text is
usually well under one gigabyte compressed, and that difference is paid for on every upload, every
download during a restore rehearsal, and every month of object storage. The engines that do compress,
PostgreSQL and MongoDB, only do so in some output formats, and their compression happens before Sentinel
ever sees the bytes.

Piping a dump through `gzip` in a shell would solve the size problem and break everything else: the hash
in the manifest would describe bytes nobody kept, the restore path would have no idea what codec was
used, and a truncated pipe would look exactly like a successful small backup. Pipeline compression exists
so that shrinking the artifact is a recorded property of the artifact rather than a shell detail, and so
that a restore can reverse it without being told.

## How it works

Pipeline compression is a stage inside the backup run, positioned after the dump has produced a complete
artifact and before anything else touches it. The order within the pipeline matters: compress, then
decide full versus incremental and archive any engine side artifacts, then encrypt, then write the
manifest.

The stage streams the artifact through the codec into a hashing writer and out to a temporary sibling
file, then atomically renames that file over the original. The artifact keeps its name. There is no
`.gz` or `.zst` suffix appended, because the manifest, not the filename, is what the restore path
consults.

Two hashes come out of this. The SHA-256 of the plaintext dump is recorded as the manifest's plaintext
value and never changes. The SHA-256 of the compressed bytes becomes the stored-artifact hash, unless
encryption then runs, in which case the hash of the ciphertext supersedes it. The manifest records the
algorithm and level in a `compression` block alongside those hashes.

On restore, the manifest drives everything. After the staged artifact has been hash-verified and
decrypted, Sentinel reads the manifest's compression block; if it names a real algorithm, a matching
decompressing reader is inserted into the stream before the plaintext is written out. There is no
operator flag for this and no filename sniffing. An artifact whose manifest has no compression block,
which covers every backup taken before the feature existed as well as every natively-compressed
PostgreSQL or MongoDB dump, skips the stage entirely and restores byte for byte as it always did.

### The double-compress guard

Compressing an already-compressed dump costs CPU and saves nothing. Configuration validation therefore
rejects any job that enables pipeline compression while also configuring engine-native compression, and
names both sides in the error. This check runs at load time, so it fails `sentinel config validate`
rather than failing halfway through a nightly run.

The guard interacts with inheritance in a way that surprises people. `defaults.compression` is inherited
by every job that does not declare its own block, including a PostgreSQL job that uses
`pg_compression_algo`. That job must explicitly opt out with `compression: {enabled: false}`, or the
whole configuration is refused. See the failure modes below.

## Per-engine behaviour

| Engine | Engine-native compression | Keys that enable it | Pipeline compression | How restore reverses it |
|---|---|---|---|---|
| PostgreSQL | Yes, inside `pg_dump` as `--compress=<algo>:<level>` | `compress` (0 to 9), `pg_compression_algo` (`gzip`, `lz4`, `zstd`, `none`), `pg_compression_level` (1 to 9), under `database_options` | Supported, but not together with the native keys | `pg_restore` reads the compressed custom or directory archive natively; Sentinel does nothing |
| MySQL | None. `mysqldump` output is plain SQL | n/a | The only option, and the reason the stage exists | Sentinel decompresses from the manifest before invoking `mysql` |
| MariaDB | None. `mariadb-dump` output is plain SQL | n/a | The only option; behaviour is identical to MySQL | Sentinel decompresses from the manifest before invoking `mariadb` |
| MongoDB | Yes, `mongodump --gzip` | `gzip: true` under `database_options` | Supported, but not together with `gzip: true` | `mongorestore` needs to be told: set `gzip: true` under the restore job's `restore_options` |

PostgreSQL's native compression carries a format restriction that is easy to trip over. `pg_dump` cannot
compress plain or tar output, so a job combining native compression with `pg_out_format: p` or
`pg_out_format: t` fails at dump time with `plain format does not support compression`. Native
compression is usable only with `pg_out_format: c` (custom) or `d` (directory). Pipeline compression has
no such restriction, because it runs on the finished file whatever shape it has.

MySQL and MariaDB share one argument builder and genuinely have no native option; this is not an
omission in Sentinel. For those two engines the pipeline stage is the whole story.

## Configuration

Pipeline compression is opt-in, and can be set once under `defaults:` or per job.

```yaml
version: "1.0"
history_db_path: ~/.sentinel/history.db

defaults:
  compression:
    enabled: true
    algorithm: zstd
    level: 3

databases:
  prod-mysql:
    type: mysql
    host_env: PROD_DB_HOST
    port: 3306
    username_env: PROD_DB_USER
    password_env: MYSQL_PASSWORD
    database: app
    schedule: "0 2 * * *"
    storage:
      type: local
      local_path: ./backups

  prod-postgres:
    type: postgres
    host_env: PROD_PG_HOST
    port: 5432
    username_env: PROD_PG_USER
    password_env: PG_PASSWORD
    database: app
    schedule: "0 3 * * *"
    storage:
      type: local
      local_path: ./backups
    compression:
      enabled: false
    database_options:
      pg_out_format: c
      pg_compression_algo: zstd
      pg_compression_level: 6
```

The `compression` block takes three keys. `enabled` defaults to false. `algorithm` is `gzip`, `zstd`, or
`none`, and defaults to `zstd` when the block is enabled without one; `none` means no pipeline
compression even when `enabled: true`, and combining `enabled: true` with `algorithm: none` is rejected
as contradictory rather than silently ignored. `level` accepts 1 to 9 for gzip and 1 to 19 for zstd, and
defaults to 6 and 3 respectively.

The complete key list is in the [configuration reference](../reference/configuration.md).

## Example

Confirm the configuration above is accepted:

```bash
sentinel config validate --config sentinel.yaml
```

```
configuration is valid
```

Now delete the `compression: {enabled: false}` override on the PostgreSQL job, so that it inherits
`defaults.compression` while still configuring `pg_compression_algo`, and validate again:

```
Error: invalid config "sentinel.yaml": backup 'prod-postgres': compression cannot be enabled together
with engine-native compression (postgres compress/pg_compression_* or mongodb gzip); disable one to
avoid double-compression
```

After a successful run of the MySQL job, the manifest beside the artifact records what was applied:

```json
{
  "compression": {
    "algorithm": "zstd",
    "level": 3
  }
}
```

That block, and nothing else, is what a later restore uses to decide whether to decompress.

## Failure modes

**A PostgreSQL or MongoDB job is rejected for double compression it did not ask for.** The job inherited
`defaults.compression`. Inheritance applies before validation, so a job that only ever set
`pg_compression_algo` still counts as having pipeline compression enabled. Add `compression: {enabled: false}`
to that job, or move the compression block out of `defaults:` onto the jobs that should have it.

**`backup --compress` does nothing on MySQL and MariaDB.** The flag maps onto `pg_dump --compress` for
PostgreSQL and `mongodump --gzip` for MongoDB, and there is no third mapping. On a MySQL or MariaDB job
it is accepted and then ignored, so the artifact is the same size it would have been without it. Worse,
the flag is applied to the job after the configuration has already been validated, which means it can
switch on PostgreSQL native compression on a job whose YAML enables pipeline compression, producing
exactly the double-compression the validator exists to prevent
([issue #183](https://github.com/denisakp/sentinel/issues/183)). Configure compression in YAML rather
than on the command line.

**A PostgreSQL dump fails with `plain format does not support compression`.** Native compression was
requested alongside `pg_out_format: p`, or `tar format does not support compression` alongside
`pg_out_format: t`. Switch the job to `pg_out_format: c`, or use pipeline compression instead, which
works with any output format.

**A restored MongoDB dump is unreadable, or `mongorestore` reports a corrupt archive.** The backup used
native `gzip: true`, so the archive is gzip-framed, and `mongorestore` was not told. Set `gzip: true`
under the restore job's `restore_options`. This asymmetry does not exist for pipeline compression, which
the restore path reverses from the manifest with no configuration at all.

**`compression.level for zstd must be between 1 and 19`, or the gzip equivalent.** The ranges differ
between the two codecs, and the validator applies the range belonging to whichever algorithm is in
effect after defaults are resolved. A level of 9 is valid for both; a level of 12 is zstd only.

**An artifact restores to garbage after the manifest was edited or lost.** The compression block is the
only record of which codec was used, and the filename carries no hint. Treat the manifest as part of the
artifact, not as metadata that can be regenerated.

## Related

- [Backup](./backup.md): where the compression stage sits in the run sequence.
- [Restore](./restore.md): the staging and preflight steps that decompression is part of.
- [Manifests and integrity](./manifest.md): the record that makes decompression automatic.
- [Security and encryption](./security-encryption.md): the stage that runs immediately after compression.
- [Storage backends](./storage-backends.md): where the smaller artifact ends up.
- [Compressing backup artifacts](../guides/backup-compression.md): enabling this on a real job.
- [`sentinel backup` reference](../reference/cli/backup.md): the `--compress`, `--pg-compression-algo`,
  and `--pg-compression-level` flags.
- [Configuration reference](../reference/configuration.md): the `compression` block and
  `database_options`.
- [`additional_args` parsing](../reference/additional-args.md): passing dump-tool flags Sentinel does not
  model.

{/* sources: internal/config/types.go, internal/config/validator.go, internal/config/loader.go, internal/config/marshal.go, internal/adapters/compress/compress.go, internal/domain/backup/pipeline.go, internal/cli/backup_factory.go, internal/cli/backup.go, internal/adapters/dump/pg/args_builder.go, internal/adapters/dump/pg/output.go, internal/adapters/dump/mongo/args_builder.go, internal/adapters/restore/runtime/executor.go, internal/ports/manifest.go */}
