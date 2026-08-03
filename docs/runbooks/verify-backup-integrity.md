# Runbook — Verify backup integrity

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: ADR 0005 (manifest v1), [enable-encryption](./enable-encryption.md), [recover-legacy-envelope](./recover-legacy-envelope.md), [failed-backup-triage](./failed-backup-triage.md)

## When to use

Post-backup verification, pre-restore sanity check, scheduled audit, or after suspected corruption / tampering.

## Preconditions

- Backup artifact and its sidecar `<file>.manifest.json` both present.
- For encrypted artifacts: matching key resolvable via `encryption_key_env`.

## Steps

### Find the execution ID

```bash
sentinel monitor list --config /workspace/infra/dataset/local.yaml --last 24h
```

Copy the `ID` from the row of interest.

### Run verify

```bash
sentinel backup verify <execution-id> --config /workspace/infra/dataset/local.yaml
```

Expected:

```
Integrity check passed: SHA-256 fingerprint matches manifest
```

### Quick file-size sanity check

```bash
ls -lh /workspace/backups/
find /workspace/backups -maxdepth 1 -type f -exec ls -lh {} \; | awk '{print $5, $9}'
```

Per-engine spot checks:

| File                 | Quick content check                                                |
|----------------------|--------------------------------------------------------------------|
| `postgres-dev.sql`   | `head -5` shows `--` PostgreSQL dump header                        |
| `mysql-dev.sql`      | `head -5` shows `-- MySQL dump` header                             |
| `mariadb-dev.sql`    | `head -5` shows `-- MariaDB dump` header                           |
| `mongo-dev/`         | `find mongo-dev -name "*.bson" | wc -l` > 0                        |

```bash
grep -c "CREATE TABLE\|INSERT INTO\|PostgreSQL database dump" /workspace/backups/postgres-dev.sql
grep -m 1 "MySQL dump"   /workspace/backups/mysql-dev.sql
grep -m 1 "MariaDB dump" /workspace/backups/mariadb-dev.sql
```

### Compare two backups (`backup diff`)

When a backup looks anomalous (much larger/slower than the last, or you suspect the safety
posture changed), compare the two **recorded metadata** sources — no restore, no re-download of
the artifact:

```bash
sentinel backup diff <old-id> <new-id> --config /workspace/infra/dataset/local.yaml
sentinel backup diff <old-id> <new-id> --output json   # for CI/alerting
```

`backup diff` reads only the monitor row + each `.manifest.json` sidecar (remote backups fetch
just the sidecar) and prints a `FIELD | BEFORE | AFTER | DELTA` table of the fields that differ:
`size_bytes`, `duration_ms`, `hash.algorithm`, `hash.value`, `encryption`, `backup_type`,
`chain_depth`. It **never opens the artifact** — use `backup verify` for byte-level integrity.

Security regressions between the two backups are flagged and set a **non-zero exit** (CI-gateable):

| Signal                 | Meaning                                                        |
|------------------------|---------------------------------------------------------------|
| `ENCRYPTION DISABLED`  | the newer backup is plaintext where the older was encrypted   |
| `HASH ALGORITHM CHANGED` | the integrity hash algorithm changed (e.g. a downgrade)     |
| `ENCRYPTION WEAKENED`  | KDF/algorithm changed, iterations decreased, or envelope dropped |

Size/duration swings get a `⚠` marker but keep exit 0. A pre-v1.1 backup with no manifest still
diffs on its monitor-row fields with a warning.

## Manifest contract (per ADR 0005)

- Sidecar file: `<backup-filename>.manifest.json`, JSON, mode `0600`.
- Required `hash.algorithm = "sha256"` and `hash.value`.
- For encrypted artifacts, `hash.plaintext_value` carries the pre-encryption digest.
- Missing manifest → treated as pre-v1.1 backup; verify emits a WARN (`ErrNoManifest`) and proceeds.
- Unknown fields tolerated on read.

## Verification

Successful exit code from `sentinel backup verify` plus a populated monitor record. Hash mismatch → exit non-zero with explicit diagnostic.

## Rollback / recovery

If verify fails, treat the artifact as untrusted:

1. Pull an older known-good artifact (`monitor list --status success`).
2. Re-run the backup from source.
3. For encrypted artifacts with an envelope-version mismatch see [recover-legacy-envelope](./recover-legacy-envelope.md).

## References

- ADR 0005 — Manifest v1 format (`docs/adr/0005-manifest-format-v1.md`)
- `internal/manifest/manifest.go`, `internal/manifest/types.go`
- `internal/cli/backup_verify.go`
- `internal/cli/backup_diff.go` — `backup diff <id1> <id2>` metadata comparison
