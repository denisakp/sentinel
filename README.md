# Sentinel

Sentinel is an open-source CLI tool for automated database backup, restore, and disaster recovery.
It supports PostgreSQL, MySQL, MariaDB, and MongoDB — with cloud storage, scheduling, monitoring, and notifications built in.

**Current version: v1.3.0**

---

## What it does

- **Backup & restore** for PostgreSQL, MySQL, MariaDB, MongoDB
- **Cloud storage** — Local, S3-compatible, Google Cloud Storage, Google Drive, Azure Blob
- **Incremental backup & restore** — WAL-based (PostgreSQL 17+), binary logs (MySQL/MariaDB), oplog (MongoDB)
- **Advanced restore** — PostgreSQL PITR, incremental chain assembly with fallback confirmation
- **Scheduling** — cron-based for both backups and restores
- **Retention** — automatic cleanup by count or age
- **Monitoring** — SQLite-backed execution history, stats, JSON/CSV export
- **Notifications** — Slack, Discord, email, webhook
- **Security** — AES-256 encryption opt-in, SHA-256 manifest integrity

---

## Installation

### Download a prebuilt binary (recommended)

No Go toolchain required. Prebuilt binaries are published on every release for
**linux**, **macOS**, and **Windows** (amd64 + arm64, except windows/arm64),
each with a SHA-256 `checksums.txt`.

1. Grab the asset for your OS/arch from the
   [latest release](https://github.com/denisakp/sentinel/releases/latest) —
   e.g. `sentinel-<version>-linux-amd64.tar.gz` (Windows ships `.zip`).
2. Download and verify the integrity of your download:

   ```bash
   VERSION=<version>          # e.g. 1.3.0 (no leading "v")
   OS=linux                   # linux | darwin | windows
   ARCH=amd64                 # amd64 | arm64
   BASE=https://github.com/denisakp/sentinel/releases/latest/download

   curl -LO "$BASE/sentinel-$VERSION-$OS-$ARCH.tar.gz"
   curl -LO "$BASE/checksums.txt"
   sha256sum -c checksums.txt --ignore-missing
   ```
3. Extract and put it on your `PATH`:

   ```bash
   tar -xzf "sentinel-$VERSION-$OS-$ARCH.tar.gz"   # unzip on Windows
   sudo mv sentinel /usr/local/bin/
   sentinel version
   sentinel version --tools   # check pg_dump, mysqldump, mongodump versions
   ```

Prebuilt binaries still expect the relevant DB client tools (`pg_dump`,
`mysqldump`, `mongodump`, …) on your `PATH`.

### Verify the release signature (recommended)

Each recent release's `checksums.txt` is signed with
[cosign](https://github.com/sigstore/cosign) keyless signing (GitHub OIDC +
the sigstore public-good Fulcio/Rekor). A signature bundle
`checksums.txt.sigstore.json` is published alongside it. Verifying the
signature proves the checksums file was produced by Sentinel's release
workflow — not just that your archive matches an (otherwise unsigned) list.
This is an added assurance on top of the `sha256sum -c` check above, not a
replacement; the checksum-only path still works if you don't have cosign.

Needs `cosign` v3+ (`brew install cosign`, or download from sigstore). No Go
toolchain and no Sentinel install required.

```bash
BASE=https://github.com/denisakp/sentinel/releases/latest/download
curl -LO "$BASE/checksums.txt"
curl -LO "$BASE/checksums.txt.sigstore.json"

# Verify checksums.txt was signed by Sentinel's release workflow on a tag:
cosign verify-blob \
  --certificate-identity-regexp 'https://github.com/denisakp/sentinel/.github/workflows/release.yml@refs/tags/.*' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  --bundle checksums.txt.sigstore.json \
  checksums.txt        # → "Verified OK"
```

Once `checksums.txt` is verified, check your archive against it with
`sha256sum -c checksums.txt --ignore-missing` as above. Verification fails
closed: a tampered `checksums.txt`, or a signature from any other
identity/issuer, is rejected.

> **Note:** releases published before signing was introduced ship no
> signature bundle and are verifiable by `checksums.txt` (SHA-256) only.
> If a release has no `checksums.txt.sigstore.json` asset, use the
> checksum-only path.

### Install with `go install`

If you already have Go 1.24+:

```bash
go install github.com/denisakp/sentinel@latest
```

> **Note:** a `go install` build is compiled without release ldflags, so
> `sentinel version` reports the `dev / unknown / unknown` development
> fallback rather than a stamped version. Use a prebuilt release binary if you
> need `sentinel version` to report the real version/commit/build date.

### Build from source (dev only)

Requires Go 1.24+.

```bash
git clone https://github.com/denisakp/sentinel.git
cd sentinel
go mod download
go build -o sentinel ./...
./sentinel version
```

---

## Quick start

Create a `sentinel.yaml`:

```yaml
version: "1.0"
log_format: json

defaults:
  schedule: "0 2 * * *"
  storage:
    type: local
    local_path: ./backups
  notifications:
    - type: slack
      webhook_url_env: SLACK_WEBHOOK_URL
      events: [failure, warning]
  retention:
    keep_last: 30
    keep_days: 90
    # Optional Grandfather-Father-Son (GFS) long-horizon retention. When set, a
    # backup is kept if ANY rule keeps it (flat OR any GFS tier). Buckets are
    # calendar periods in UTC; empty periods are skipped. See
    # docs/runbooks/retention-gfs.md.
    gfs:
      keep_daily: 7      # newest backup of each of the last 7 days
      keep_weekly: 4     # ... last 4 ISO weeks (Mon–Sun)
      keep_monthly: 12   # ... last 12 months
      keep_yearly: 3     # ... last 3 years

databases:
  prod-postgres:
    type: postgres
    host_env: PROD_DB_HOST
    port: 5432
    username_env: PROD_DB_USER
    password_env: PG_PASSWORD
    database: myapp_prod

restores:
  test-restore:
    type: postgres
    enabled: false
    host: localhost
    port: 5432
    username_env: TEST_DB_USER
    password_env: TEST_DB_PASSWORD
    database: test_db
    schedule: "0 3 * * 0"
    backup_source:
      type: local
      backup_path: ./backups/prod-postgres-latest.sql
    verify_after_restore: true
```

Run a backup:

```bash
./sentinel backup --config sentinel.yaml
```

---

## Core commands

```bash
# Backup
sentinel backup --config sentinel.yaml

# Scheduler (starts cron loop for backups + restores)
sentinel schedule start --config sentinel.yaml
sentinel schedule list --config sentinel.yaml
sentinel schedule status prod-postgres --config sentinel.yaml

# Restore
sentinel restore list --config sentinel.yaml
sentinel restore enable test-restore --config sentinel.yaml
sentinel restore dry-run test-restore --config sentinel.yaml
sentinel restore run test-restore --config sentinel.yaml
sentinel restore history --config sentinel.yaml

# Monitoring
sentinel monitor list --config sentinel.yaml --last 7d
sentinel monitor stats --config sentinel.yaml
sentinel monitor export --config sentinel.yaml --format json --output history.json

# Retention
sentinel retention preview --config sentinel.yaml
sentinel retention apply --config sentinel.yaml

# Integrity
sentinel backup verify <backup-id> --config sentinel.yaml   # verify one backup
sentinel backup verify --all --config sentinel.yaml          # repository-wide integrity sweep
sentinel backup verify --all --since 30d --output json --config sentinel.yaml
sentinel backup diff <id1> <id2> --config sentinel.yaml      # compare two backups' metadata

# State repair (reconcile monitor rows / manifests / artifacts / locks)
sentinel repair --dry-run --config sentinel.yaml             # report drift, change nothing
sentinel repair --fix --config sentinel.yaml                 # apply recoverable fixes
sentinel repair --purge-orphans --yes --config sentinel.yaml # also delete orphan artifacts

# Config & storage
sentinel config validate --config sentinel.yaml
sentinel storage status --config sentinel.yaml

# Encryption key
sentinel security init-key

# Schema migrations
sentinel db migrate status --config sentinel.yaml
```

---

## Incremental backup

Enable incremental backup on a job:

```yaml
databases:
  mysql-prod:
    type: mysql
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PASSWORD
    database: appdb
    storage:
      type: local
      local_path: ./backups
    incremental_backup:
      enabled: true
      max_chain_depth: 6
      binlog_check: true
    mysql:
      binlog_path: /var/lib/mysql
```

Inspect and manage chains:

```bash
sentinel backup chain-status --config sentinel.yaml
sentinel backup chain-list --config sentinel.yaml
sentinel backup force-full --job mysql-prod --config sentinel.yaml
```

---

## Advanced restore (PITR & incremental)

```yaml
restores:
  postgres-incident-recovery:
    enabled: true
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: POSTGRES_PASSWORD
    database: appdb
    verify_after_restore: true
    backup_source:
      type: local
      backup_path: ./backups/appdb-base.dump
    restore_mode: pitr
    pitr_timestamp: "2026-03-20T23:59:00Z"

  mysql-incremental-recovery:
    enabled: true
    type: mysql
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PASSWORD
    database: appdb
    backup_source:
      type: local
      backup_path: ./backups/appdb-base.sql
    restore_mode: incremental
    incremental_from_backup: appdb-base
    mysql:
      binlog_target_time: "2026-03-21T04:15:00Z"
```

```bash
sentinel restore dry-run postgres-incident-recovery --config sentinel.yaml
sentinel restore run postgres-incident-recovery --config sentinel.yaml
sentinel restore validate-chain mysql-incremental-recovery --config sentinel.yaml
sentinel restore history postgres-incident-recovery --config sentinel.yaml
```

> For full operator notes on PITR, incremental chain rules, and fallback confirmation, see [docs/runbooks/restore-pitr-and-incremental.md](docs/runbooks/restore-pitr-and-incremental.md).

---

## Encryption

Encryption is opt-in. To enable:

```yaml
encryption_key_env: SENTINEL_MASTER_KEY
```

```bash
sentinel security init-key          # generate key
export SENTINEL_MASTER_KEY=<key>
sentinel backup --config sentinel.yaml
```

Plaintext is the default when no encryption config is present. See [docs/runbooks/enable-encryption.md](docs/runbooks/enable-encryption.md) for full security semantics.

---

## Compression

Pipeline compression is opt-in and engine-agnostic — it primarily closes the gap
for **MySQL / MariaDB**, whose dumps are otherwise raw SQL text (PostgreSQL and
MongoDB already compress natively). The stage runs `dump → compress → hash →
encrypt → upload`, and restore auto-detects the algorithm from the manifest (no
operator flag).

```yaml
defaults:
  compression:
    enabled: true
    algorithm: zstd      # gzip | zstd | none  (default: zstd)
    level: 6             # gzip 1–9, zstd 1–19  (0/omitted = per-algorithm default)
```

Enabling pipeline compression alongside engine-native compression
(`pg_dump --compress` / `mongodump --gzip`) on the same job is rejected to
prevent double-compression. Default is off (no behaviour change until enabled).
See [docs/runbooks/backup-compression.md](docs/runbooks/backup-compression.md).

---

## Integrity sweep

`sentinel backup verify` re-hashes a backup artifact and compares it against its stored
manifest. Pass a single `<backup-id>` to verify one backup, or `--all` to sweep the **whole
repository** in one command:

```bash
sentinel backup verify --all --config sentinel.yaml
sentinel backup verify --all --since 30d --job prod-postgres --config sentinel.yaml
sentinel backup verify --all --output json --config sentinel.yaml   # for alerting
```

The sweep enumerates every recorded successful backup, fetches each from its own storage
backend (remote artifacts are downloaded and verified, then deleted — no local copy is left
behind), and classifies each into one of four states — `ok`, `corrupted`, `missing_artifact`,
`missing_manifest` — with a per-status summary. It is **read-only** (no history writes). Exit
codes let an automated system act: `0` all-ok, `5` integrity failure (something is corrupt /
missing), `4` operational error (the check itself couldn't run). `--ignore-missing-manifest`
downgrades legacy pre-v1.1 backups to a warning. See
[docs/runbooks/integrity-sweep.md](docs/runbooks/integrity-sweep.md).

### Scheduling the sweep

Run the **same** sweep automatically on a cron — recording every run and paging on corruption —
by adding an `integrity.scheduled_check` block. `sentinel schedule start` then registers a
reserved `__integrity_check` job alongside your backups/restores (additive; existing scheduling
unchanged):

```yaml
integrity:
  scheduled_check:
    enabled: true
    cron: "0 3 * * 0"     # every Sunday 03:00
    since: 30d            # optional recency window; omit to sweep everything
    job: ""              # optional single-job scope; empty = all jobs
    notify_on: failure    # failure (default) | always | never
```

Each run records **one row per artifact** (grouped by `run_id`, labelled `scheduled`/`manual`)
in the `integrity_checks` audit table — a durable trail of when each artifact was checked and
what was found (added by monitor schema migration `005`; existing history DBs upgrade in place).
A non-`ok` result pages the configured channels (`defaults.notifications`) when `notify_on` is
`failure`/`always`; `never` stays silent; delivery is best-effort. See
[docs/runbooks/integrity-sweep.md](docs/runbooks/integrity-sweep.md).

### Comparing two backups

`sentinel backup diff <id1> <id2>` compares the **recorded metadata** of two backups — size,
duration, hash algorithm, encryption posture, backup type and chain depth — to diagnose a
sudden anomaly (a backup 3× larger, twice as slow, or silently plaintext) **without a restore**:

```bash
sentinel backup diff abc123 def456 --config sentinel.yaml
sentinel backup diff abc123 def456 --output json --config sentinel.yaml   # for CI/alerting
```

It reads only the monitor row and each artifact's `.manifest.json` sidecar — **no artifact
bytes are read** (remote backups fetch just the tiny sidecar, never the artifact). Only the
fields that differ are printed. **Security regressions** are flagged explicitly and yield a
**non-zero exit** so CI can gate: encryption turned off (`ENCRYPTION DISABLED`), a hash-algorithm
change (`HASH ALGORITHM CHANGED`), or an encryption-parameter downgrade (`ENCRYPTION WEAKENED`).
Size/duration swings get a `⚠` marker but stay informational (exit 0). A backup with no manifest
(pre-v1.1) still diffs on its monitor-row fields with a warning. See
[docs/runbooks/verify-backup-integrity.md](docs/runbooks/verify-backup-integrity.md).

### Verifying immediately after upload

The checks above catch corruption **after the fact** — at the next scheduled sweep or restore.
`integrity.verify_after_upload` closes that gap at backup time: right after the artifact reaches
its storage backend, Sentinel re-downloads it and re-hashes it against the manifest, **failing the
backup immediately** on a mismatch instead of reporting success on a corrupted upload:

```yaml
integrity:
  verify_after_upload: true   # default for every job unless overridden

databases:
  prod-s3:
    type: postgres
    storage: { type: s3, s3_bucket: backups }
    verify_after_upload: true   # per-job override (inherits the default above when omitted)
```

Opt-in and **off by default** — it doubles read I/O and, on S3/GCS/Azure/GDrive, adds egress cost
and latency proportional to the backup size. It is most valuable for **remote** backends (network
writes can be silently truncated); it also works for local storage (catches disk write faults) but
is lower-value there since it re-reads the same file. On mismatch the job fails with
`verify_after_upload_failed`, is recorded/notified like any other failure, and the corrupt object
is **left in place** (never auto-deleted) so it can be inspected.

---

## State repair

A crash mid-backup, a hard-killed process, or a manual artifact deletion can leave Sentinel's
three sources of truth — monitor rows, `*.manifest.json` sidecars, and the artifacts on storage —
silently disagreeing. `sentinel repair` reconciles them repository-wide (this is **not**
`monitor doctor --repair`, which is schema-only).

```bash
sentinel repair --dry-run --config sentinel.yaml    # always run this first
```

It detects six drift classes: `orphan_artifact` (object with no sidecar and no row),
`orphan_manifest` (sidecar whose artifact is gone), `artifact_missing` (recorded backup absent
from storage), `stale_running` (a `running` row whose job holds no live lock), `stale_lock`
(dead-PID lock past the threshold), and `chain_broken` (incremental chain with a missing or
non-contiguous link — via the same resolver as `restore validate-chain`).

**Report-only by default; destructive actions are strictly opt-in:**

```bash
sentinel repair --config sentinel.yaml               # report-only (same as --dry-run)
sentinel repair --fix --config sentinel.yaml         # finalize stale runs, remove stale locks, mark broken chains
sentinel repair --purge-orphans --config sentinel.yaml  # also DELETE orphan artifacts (prompts; add --yes for automation)
sentinel repair --dry-run --format json --config sentinel.yaml   # structured findings for tooling
```

Safety guards: `--purge-orphans` is the only path that deletes artifacts, it prompts unless
`--yes`, and it **never** removes an active chain baseline. A `running` row is finalized only when
no **live** lock is held for its job (the guard the scheduler's blind startup reconcile lacks);
foreign-host locks and rows are skipped with a warning. Repair refuses to run against a
`forward-incompatible`/`corrupt` monitor schema (defer to `monitor doctor`), and exits non-zero
while manual-action inconsistencies (`artifact_missing`, `chain_broken`) remain, so it can gate
CI/cron. See [docs/runbooks/state-repair.md](docs/runbooks/state-repair.md).

---

## Performance

How long does a full backup take, how big is the artifact, how much RAM does it burn, how long
does a restore take? Published, reproducible numbers per engine (PostgreSQL, MySQL, MariaDB,
MongoDB) live in [`docs/benchmarks/`](docs/benchmarks/README.md), starting with
[`v1.3.0.md`](docs/benchmarks/v1.3.0.md). Numbers are produced by a committed, re-runnable
harness (`scripts/benchmark.sh` / `make bench-small`) over real databases and real dump/restore
tools — not the `tests/benchmarks/` Go micro-benchmarks, which measure unrelated in-process
logic (config parsing, scheduler overhead). See the benchmarks README for methodology
(hardware, DB versions, storage backend, how to reproduce).

---

## Contributing

- [CONTRIBUTING.md](CONTRIBUTING.md) — setup, coding standards, PR process
- [SECURITY.md](SECURITY.md) — reporting vulnerabilities
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) — community standards
- [Bug report](.github/ISSUE_TEMPLATE/1-bug.md) · [Feature request](.github/ISSUE_TEMPLATE/2-feature-request.md)
