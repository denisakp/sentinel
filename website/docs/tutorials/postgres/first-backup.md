---
title: Your first PostgreSQL backup
description: Build a PostgreSQL backup configuration from nothing, take a backup, and verify its recorded SHA-256.
sidebar_position: 2
---

By the end of this page you will have a PostgreSQL database called `shop`, a Sentinel configuration
that keeps every file it creates inside one directory, a backup artifact with a manifest beside it,
and proof from Sentinel's own integrity check that the artifact has not changed since it was written.

Budget about twenty minutes. Everything you create here is reused by the next three pages, so do not
delete the directory when you finish.

This track picks up where the [quickstart](../../intro/quickstart.md) stops. If you have already run
the quickstart, none of it is wasted; the configuration you build here is larger, and the reasons
for the extra keys are the point of the page.

## What you need

- Sentinel installed: see [Installation](../../intro/installation.md).
- **PostgreSQL client tools 17 or later** on your `PATH`: `psql`, `pg_dump`, and `pg_restore`.
  Sentinel shells out to these; it does not link `libpq` itself.
- Docker, for a throwaway server.

The commands below start a single container. If you would rather have every supported engine running
at once, the repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up PostgreSQL 17, MySQL, MariaDB, MongoDB, and an S3-compatible object store on a shared
`sentinel` network. Its PostgreSQL service is pinned to 17 for the same reason this page is:
WAL-based incremental backup needs PostgreSQL 17 or later.

## Step 1: Start a throwaway PostgreSQL

```bash
docker run --name sentinel-pg-tutorial \
  -e POSTGRES_PASSWORD=tutorial \
  -e POSTGRES_DB=shop \
  -p 5432:5432 -d postgres:17 \
  -c summarize_wal=on
```

The trailing `-c summarize_wal=on` is not needed for this page. It is needed for
[incremental chains](./incremental-wal.md) two pages from now, and turning it on at creation time
saves you restarting the container later.

Give the server a few seconds, then confirm it is up and that the setting took:

```bash
export PGPASSWORD=tutorial

psql -h 127.0.0.1 -U postgres -d shop -c "SHOW server_version;" -c "SHOW summarize_wal;"
```

You should see:

```text
         server_version
---------------------------------
 17.10 (Debian 17.10-1.pgdg13+1)
(1 row)

 summarize_wal
---------------
 on
(1 row)
```

## Step 2: Put some data in it

```bash
psql -h 127.0.0.1 -U postgres -d shop -c "
CREATE TABLE customers (id serial PRIMARY KEY, name text NOT NULL, email text NOT NULL);
CREATE TABLE orders (id serial PRIMARY KEY, customer_id int REFERENCES customers(id), total numeric(10,2), placed_at timestamptz DEFAULT now());
INSERT INTO customers (name, email) VALUES ('Ada Lovelace','ada@example.invalid'),('Grace Hopper','grace@example.invalid'),('Alan Turing','alan@example.invalid');
INSERT INTO orders (customer_id, total) VALUES (1, 42.00), (2, 17.50), (3, 99.99);"
```

You should see:

```text
CREATE TABLE
CREATE TABLE
INSERT 0 3
INSERT 0 3
```

## Step 3: Write the configuration

Make a working directory and change into it; every path in this track is relative to it:

```bash
mkdir sentinel-postgres-tutorial && cd sentinel-postgres-tutorial
```

Create `sentinel.yaml`:

```yaml
version: "1.0"
log_format: text
history_db_path: ./history.db

scheduler:
  lock_dir: ./locks

defaults:
  storage:
    type: local
    local_path: ./backups
  retention:
    keep_last: 10

databases:
  shop:
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: PGPASSWORD
    database: shop
    output: shop.sql
```

Four of those keys deserve an explanation, because each one is a decision you will make again on
every real deployment.

**`password_env: PGPASSWORD` names an environment variable; it is not the password.** In YAML,
`password_env` is the only way to supply one: there is no key that takes a literal password. On the
command line the equivalents are `--password-env <VAR>` and `--password-file <PATH>`. A bare
`--password` flag still exists but is deprecated and will be removed, because anything on a command
line is visible in `ps` output, `/proc/<pid>/cmdline`, and shell history.

**`history_db_path: ./history.db` keeps the execution history local.** The default is
`~/.sentinel/history.db`, shared by every configuration on the machine. Pointing it at the working
directory means this tutorial's history does not mix with anything else you run, and cleaning up is
one `rm -rf`.

**`scheduler.lock_dir: ./locks` keeps the job locks local.** The default is `/var/run/sentinel`,
which an unprivileged user cannot create. You will meet the failure this avoids on the
[restore page](./restore.md).

**`output: shop.sql` names the artifact; and that is what produces the manifest.**

:::note `output` is what gives you a manifest

Without `output`, the storage backend invents a timestamped name (`SENTINEL_<timestamp>.sql`) but
Sentinel does not learn it, so it writes no `.manifest.json` sidecar. `sentinel backup verify` then
has nothing to compare against and reports `missing_manifest`. Setting `output` explicitly is what
makes Step 7 on this page work.

The trade-off is that a fixed name is overwritten on every run. That is fine for a tutorial and for
any repository where retention is handled by object versioning; for a real local repository, run
backups through `sentinel schedule start`, which builds a timestamped name and records it.
:::

Check the file before going further:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
2026/08/05 17:43:57 WARN TLS not configured for database event=tls_not_configured database=shop
configuration is valid
```

That TLS warning is correct here; you are talking to a container over loopback. On a real database,
configure TLS. Sentinel warns rather than failing so that a first run is possible, and repeats the
warning on every subsequent command so it never becomes invisible.

## Step 4: Take the backup

```bash
sentinel backup --config sentinel.yaml
```

You should see:

```text
2026/08/05 17:43:57 WARN TLS not configured for database event=tls_not_configured database=shop
Backup successfully written to /path/to/sentinel-postgres-tutorial/backups/shop.sql
Backup complete !
2026/08/05 17:43:57 INFO monitor schema migration starting event=monitor_schema_migration_starting db_path=./history.db current_version=0 required_version=5
2026/08/05 17:43:57 INFO monitor schema migration complete event=monitor_schema_migration_complete db_path=./history.db applied_version=5
```

The two migration lines appear only on the first run: Sentinel created `./history.db` and brought it
up to the schema version this binary requires.

## Step 5: Look at what was written

```bash
ls backups/
```

You should see:

```text
shop.sql
shop.sql.manifest.json
```

The sidecar is what makes the artifact verifiable:

```bash
cat backups/shop.sql.manifest.json
```

You should see:

```json
{
  "backup_id": "shop",
  "database": "shop",
  "database_type": "postgres",
  "created_at": "2026-08-05T17:43:57.575782Z",
  "size_bytes": 3908,
  "hash": {
    "algorithm": "sha256",
    "value": "5d28103153eea0f1a5445ad5e5a9a99cf54ec701e77acbb19cbf7f4971c8767d",
    "plaintext_value": "5d28103153eea0f1a5445ad5e5a9a99cf54ec701e77acbb19cbf7f4971c8767d"
  },
  "advanced_restore": {
    "capabilities": ["full", "incremental"],
    "incremental_lineage": {
      "engine": "postgres",
      "execution_supported": true
    }
  }
}
```

`value` and `plaintext_value` are identical because this artifact is not encrypted. When encryption
is enabled they differ: `plaintext_value` fingerprints the dump, `value` fingerprints the
ciphertext.

## Step 6: Check the execution history

Every run is recorded, whether it succeeded or failed:

```bash
sentinel monitor list --config sentinel.yaml
```

You should see:

```text
ID                                    JOB   TYPE  CHAIN  STATUS   TIMESTAMP            DURATION  DELTA  ERROR
20591f75-8d9c-4c7a-9af4-435013b28c0a  shop  full  -      success  2026-08-05 17:43:57  79ms      -
```

Your ID will differ; it is a fresh UUID per execution. Note it down; the next step uses it.

`CHAIN` is empty and `TYPE` is `full` because incremental backup is off. Both columns come alive on
the [incremental chains page](./incremental-wal.md).

For an aggregate view of one job:

```bash
sentinel monitor stats --job shop --config sentinel.yaml
```

You should see:

```text
Backup Job: shop
Period: last 30 days

Executions: 1
Success Rate: 100.0% (1/1)
Failures: 0

Duration:
  Average: 79ms
  Median: 79ms
  Min: 79ms
  Max: 79ms

Size:
  Total: 3908
  Average: 3908

Trend: stable

Last Execution:
  Status: success
  Time: 2026-08-05 17:43:57
  Duration: 79ms
  Size: 3908
```

:::note `--job` is required

`sentinel monitor stats` describes `--job` as optional in its help text, but rejects the command
without it: `Error: --job is required`. Pass it.
:::

## Step 7: Verify the backup

A backup you have not verified is a hope, not a backup. Sentinel hashes every artifact as it writes
it and can re-read the file later to confirm the hash still matches.

Sweep the whole repository:

```bash
sentinel backup verify --all --config sentinel.yaml
```

You should see:

```text
ID                                    JOB   STATUS  HASH_MATCH  TIMESTAMP
20591f75-8d9c-4c7a-9af4-435013b28c0a  shop  ok      true        2026-08-05T17:43:57Z
1 checked · 1 ok · 0 corrupted · 0 missing_artifact · 0 missing_manifest
```

Or verify a single backup by its execution ID:

```bash
sentinel backup verify 20591f75-8d9c-4c7a-9af4-435013b28c0a --config sentinel.yaml
```

You should see:

```text
PASS: Backup 20591f75-8d9c-4c7a-9af4-435013b28c0a integrity verified
  Database: shop
  File:     backups/shop.sql
  Hash:     5d28103153eea0f1a5445ad5e5a9a99cf54ec701e77acbb19cbf7f4971c8767d (sha256)
  Status:   PASS
```

The sweep exits non-zero when anything is wrong, so it works as a cron or CI gate. The four counters
in the summary line are the four things it distinguishes: `ok`, `corrupted` (hash mismatch),
`missing_artifact` (the file is gone), and `missing_manifest` (there is nothing to compare against).
Use `--ignore-missing-manifest` to downgrade the last one to a warning when a repository contains
older artifacts written before you set `output`.

## What just happened

Sentinel resolved `PGPASSWORD`, connected to the server, ran `pg_dump`, streamed the output through
a SHA-256 hasher on its way to `./backups/shop.sql`, wrote the digest and metadata into
`shop.sql.manifest.json`, and appended a row to `./history.db`. The verify step re-read the artifact
from disk and recomputed the digest independently.

Everything on this page is the plain, full-dump path. Nothing was compressed, encrypted, chained, or
uploaded anywhere; those are configuration, not different code. See
[Backup](../../concepts/backup.md) for the model behind what you just ran.

## Next

- **[Restoring into a second database](./restore.md)**: prove the artifact is usable, which is the
  only test of a backup that counts.

{/* sources: internal/cli/backup.go, internal/cli/backup_verify.go, internal/cli/monitor.go, internal/cli/config.go, internal/config/types.go, internal/config/loader.go, internal/domain/backup/pipeline.go, internal/domain/backup/executor.go, internal/adapters/manifest_store/store.go, infra/docker/docker-compose.yml */}
