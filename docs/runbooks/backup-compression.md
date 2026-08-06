# Runbook — Backup compression (gzip / zstd)

> **Superseded by the documentation site: [guides/backup-compression](https://denisakp.github.io/sentinel/guides/backup-compression).**
>
> This runbook superseded; verified against the code. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-08-02
- **Related**: PRD 33 / spec 049 (pipeline compression), [enable-encryption](./enable-encryption.md), [verify-backup-integrity](./verify-backup-integrity.md), [restore-from-backup](./restore-from-backup.md), [check-storage-backend](./check-storage-backend.md)

## When to use

You want to shrink backup artifacts (storage-at-rest + upload/download bandwidth)
for the engines that do **not** compress natively — **MySQL** and **MariaDB**,
whose dumps stream to storage as raw SQL text. SQL data typically compresses
~80–90%.

PostgreSQL (`pg_dump --compress`) and MongoDB (`mongodump --gzip`) already
compress natively; pipeline compression is available for them too, but you must
not enable both at once (see the double-compress guard below).

## How it works

Compression is an **engine-agnostic streaming pipeline stage**, a sibling of the
AES-256-GCM encryption stage. The order is load-bearing:

```
dump → compress → SHA-256 hash → encrypt → upload
```

- **Compress before encrypt** — ciphertext is incompressible, so compressing
  after encryption would achieve ~0%.
- The recorded manifest hash covers the **compressed** bytes (the artifact that
  lands in storage), consistent with the encrypted-path precedent.
- The artifact keeps its configured filename (no `.gz` / `.zst` suffix is added);
  the manifest records the algorithm + level, and restore reads it back
  automatically.

Restore is fully automatic: `download → decrypt → decompress → feed to the
restore tool`, with the algorithm read from the manifest — **no operator flag**.

## Preconditions

- Sentinel binary present (compression is pure-Go: stdlib gzip +
  `github.com/klauspost/compress/zstd`, no external tools).
- Opt-in: nothing changes until you set `compression.enabled: true`.

## Configuration

`CompressionConfig` can be set per job (`databases.<job>.compression`) or as a
global default (`defaults.compression`); a job without its own block inherits the
default (same inheritance rule as retention).

```yaml
defaults:
  compression:
    enabled: true
    algorithm: zstd      # gzip | zstd | none  (default: zstd)
    level: 6             # gzip 1–9, zstd 1–19  (0/omitted = per-algorithm default)

databases:
  prod-mysql:
    type: mysql
    host: host.docker.internal
    port: 3306
    username: sentinel
    password_env: DEV_MYSQL_PASSWORD
    database: app
    output: prod-mysql.sql
    compression:
      enabled: true
      algorithm: gzip     # override the default for this job
```

Fields:

- `enabled` — turns pipeline compression on. Default `false` (opt-in; zero
  behaviour change until set).
- `algorithm` — `gzip`, `zstd`, or `none`. Omitted → `zstd` (best ratio/speed,
  pure Go). `none` means "no pipeline compression" even if `enabled: true`.
- `level` — codec level. gzip `1`–`9`, zstd `1`–`19`. `0` / omitted selects the
  per-algorithm default (gzip 6, zstd 3).

### Double-compress guard (Option A)

Pipeline compression may **not** be enabled on a job that also uses
engine-native compression. Validation fails with:

```
backup '<job>': compression cannot be enabled together with engine-native
compression (postgres compress/pg_compression_* or mongodb gzip); disable one
to avoid double-compression
```

Native compression means, for PostgreSQL, any of the `compress`,
`pg_compression_algo` (≠ `none`), or `pg_compression_level` database options; for
MongoDB, `gzip: true`. Choose one mechanism per job.

## Steps

### Enable and run

```bash
export DEV_MYSQL_PASSWORD=sentinel
sentinel config validate --config /workspace/infra/dataset/local.yaml
sentinel backup --config /workspace/infra/dataset/local.yaml
```

### Confirm the artifact shrank

Compare against an uncompressed baseline of the same database:

```bash
ls -l /workspace/backups/prod-mysql.sql
```

Expect a materially smaller file (target ≥ 80% reduction on SQL data).

### Inspect the manifest

```bash
cat /workspace/backups/prod-mysql.sql.manifest.json
```

```json
{
  "hash": { "algorithm": "sha256", "value": "<hash-of-compressed-bytes>" },
  "compression": { "algorithm": "gzip", "level": 6 }
}
```

The `compression` block is what restore auto-detects. A backup **without** a
`compression` block (legacy backups, or natively-compressed PG/Mongo dumps) is
restored unchanged — no decompress stage is attempted.

## Restore

No extra flags. Point the restore job at the artifact as usual; Sentinel reads
`compression.algorithm` from the manifest and inserts the decompress stage after
decryption:

```bash
sentinel restore run <job> --config /workspace/infra/dataset/local.yaml
```

If the artifact is also encrypted, the order is decrypt → decompress → restore,
handled automatically.

## Compression + encryption

Both can be enabled on the same job. The pipeline compresses first, then
encrypts (ciphertext is incompressible, so this order is mandatory). The manifest
carries both a `compression` block and an `encryption` block; the `hash.value`
covers the final stored (compressed-then-encrypted) bytes.

## Rollback / recovery

To stop compressing new backups, set `compression.enabled: false` (or remove the
block) and re-run. Existing compressed artifacts remain restorable — the manifest
records how to decompress them.

## Choosing an algorithm

- **zstd** (default) — best ratio-for-speed; recommended for most workloads.
- **gzip** — ubiquitous, slightly lower ratio; use when downstream tooling must
  read the raw stream with standard `gzip`.

lz4 / snappy are intentionally out of scope for v1.

## References

- `internal/adapters/compress/` — gzip + zstd codecs (`ports.CompressWriter` /
  `ports.DecompressReader`)
- `internal/domain/backup/pipeline.go` — `ApplyArtifactSecurity` (compress stage)
- `internal/cli/backup_factory.go` — `compressBackupFile` (hook wiring)
- `internal/adapters/restore/runtime/executor.go` — `applyPreflight` (decompress)
- `internal/config/{types,validator,loader}.go` — `CompressionConfig`
