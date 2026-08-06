---
title: Your first MySQL backup
description: Build a MySQL backup configuration from nothing, take a backup with mysqldump, and verify its recorded SHA-256.
sidebar_position: 2
---

By the end of this page you will have a MySQL database called `shop`, a Sentinel configuration that
keeps every file it creates inside one directory, a backup artifact with a manifest beside it, and
proof from Sentinel's own integrity check that the artifact has not changed since it was written.

Budget about twenty minutes. Everything you create here is reused by the next three pages, so do not
delete the directory when you finish.

## What you need

- Sentinel installed: see [Installation](../../intro/installation.md).
- **`mysqldump` and `mysql` on your `PATH`.** Sentinel runs `mysqldump` as a subprocess to produce
  the backup and `mysql` to apply a restore; it does not link a client library, so a missing binary
  surfaces as an exec failure rather than a configuration error.
- Docker, for a throwaway server.

The commands below start a single container. If you would rather have every supported engine running
at once, the repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up `mysql:lts`, MariaDB, PostgreSQL 17, MongoDB, and an S3-compatible object store on a shared
`sentinel` network. Its MySQL service publishes host port `3307`, and this page uses the same port so
the two do not collide.

## Step 1: Start a throwaway MySQL

```bash
docker run --name sentinel-mysql-tutorial \
  -e MYSQL_ROOT_PASSWORD=tutorial \
  -e MYSQL_DATABASE=shop \
  -p 3307:3306 -d mysql:lts \
  --log-bin=mysql-bin
```

The trailing `--log-bin=mysql-bin` is not needed for this page. It is needed for
[binary-log archival](./incremental-binlog.md) two pages from now, and setting it at creation time
saves you recreating the container later. It matters even though MySQL 8 already writes binary logs
by default, because the default file names are `binlog.NNNNNN` and Sentinel only collects files named
`mysql-bin.NNNNNN`.

The server takes a few seconds to initialise. Export the password once, into the variable the `mysql`
client and Sentinel both read, then confirm the server is up:

```bash
export MYSQL_PWD=tutorial

mysql -h 127.0.0.1 -P 3307 -u root -e "SELECT VERSION(); SHOW VARIABLES LIKE 'log_bin%';"
```

`MYSQL_PWD` is the only password channel used anywhere on this track. Sentinel passes the resolved
password to `mysqldump` and `mysql` through exactly this variable and never as a command-line
argument, because anything on a command line is visible in `ps` output and `/proc/<pid>/cmdline`.

**What to look for**: a version string beginning `8.`, `log_bin` reported as `ON`, and
`log_bin_basename` ending in `mysql-bin`. If `log_bin_basename` ends in `binlog` instead, the
`--log-bin` argument did not reach the server; remove the container and start it again.

## Step 2: Put some data in it

```bash
mysql -h 127.0.0.1 -P 3307 -u root shop -e "
CREATE TABLE customers (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(120) NOT NULL, email VARCHAR(180) NOT NULL);
CREATE TABLE orders (id INT AUTO_INCREMENT PRIMARY KEY, customer_id INT, total DECIMAL(10,2), placed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY (customer_id) REFERENCES customers(id));
INSERT INTO customers (name, email) VALUES ('Ada Lovelace','ada@example.invalid'),('Grace Hopper','grace@example.invalid'),('Alan Turing','alan@example.invalid');
INSERT INTO orders (customer_id, total) VALUES (1, 42.00), (2, 17.50), (3, 99.99);"
```

The `mysql` client prints nothing on success. Confirm the rows landed:

```bash
mysql -h 127.0.0.1 -P 3307 -u root shop -e "SELECT COUNT(*) FROM customers; SELECT COUNT(*) FROM orders;"
```

**What to look for**: `3` from each count.

## Step 3: Write the configuration

Make a working directory and change into it; every path in this track is relative to it:

```bash
mkdir sentinel-mysql-tutorial && cd sentinel-mysql-tutorial
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
    type: mysql
    host: 127.0.0.1
    port: 3307
    username: root
    password_env: MYSQL_PWD
    database: shop
    output: shop.sql
    database_options:
      single_transaction: true
      routines: true
      triggers: true
```

Five of those keys deserve an explanation, because each one is a decision you will make again on
every real deployment.

**`password_env: MYSQL_PWD` names an environment variable; it is not the password.** In YAML,
`password_env` is the only key that supplies a password directly: there is no key that takes a
literal one. The MySQL-specific alternative, a `my.cnf` file, is covered in Step 8 below. On the
command line the equivalents are `--password-env <VAR>` and `--password-file <PATH>`.

**`history_db_path: ./history.db` keeps the execution history local.** The default is
`~/.sentinel/history.db`, shared by every configuration on the machine. Pointing it at the working
directory means this tutorial's history does not mix with anything else you run.

**`scheduler.lock_dir: ./locks` keeps the job locks local.** The default is `/var/run/sentinel`,
which an unprivileged user cannot create. You will meet the failure this avoids on the
[restore page](./restore.md).

**`output: shop.sql` names the artifact, and that is what produces the manifest.**

**`database_options` are dump-tool flags, expressed as booleans.** For MySQL and MariaDB, Sentinel
recognises exactly four keys and translates each true value into one `mysqldump` flag:

| Key | Flag added |
|---|---|
| `single_transaction` | `--single-transaction` |
| `routines` | `--routines` |
| `triggers` | `--triggers` |
| `events` | `--events` |

Anything else in the block is ignored silently. `single_transaction` is the one that matters most on
a live InnoDB database: it takes the dump from a consistent snapshot without locking every table.
For flags outside that list, use `additional_args`; see the
[additional arguments reference](../../reference/additional-args.md).

:::note `output` is what gives you a manifest

Without `output`, the storage backend invents a timestamped name but Sentinel does not learn it, so
it writes no `.manifest.json` sidecar. `sentinel backup verify` then has nothing to compare against
and reports `missing_manifest`. Setting `output` explicitly is what makes Step 7 on this page work.

An `output` value with no file extension gets `.sql` appended; `shop.sql` is kept verbatim.

The trade-off is that a fixed name is overwritten on every run. That is fine for a tutorial; for a
real repository, run backups through the scheduler, which builds a timestamped name and records it.
:::

Check the file before going further:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
2026/08/05 20:57:09 WARN TLS not configured for database event=tls_not_configured database=shop
configuration is valid
```

That TLS warning is correct here; you are talking to a container over loopback. On a real database,
configure TLS. Sentinel warns rather than failing so that a first run is possible, and repeats the
warning on every subsequent command so it never becomes invisible.

## Step 4: Take the backup

```bash
sentinel backup --config sentinel.yaml
```

**What to look for**: the same TLS warning, then `Backup successfully written to` followed by the
absolute path (printed by the local storage backend as it writes the file), then `Backup complete !`
(printed by the MySQL dump adapter once the write returns). On the very first run two further lines
report that Sentinel created `./history.db` and migrated it to the schema version this binary
requires; they do not appear again.

## Step 5: Look at what was written

```bash
ls backups/
```

The directory should contain exactly two files: `shop.sql`, and `shop.sql.manifest.json`. The
manifest path is always the artifact path with `.manifest.json` appended.

The sidecar is what makes the artifact verifiable:

```bash
cat backups/shop.sql.manifest.json
```

The fields it carries, and what each one means for MySQL:

| Field | Value on this artifact |
|---|---|
| `backup_id` | `shop`, the job name |
| `database_type` | `mysql` |
| `hash.value` | SHA-256 of the stored bytes |
| `hash.plaintext_value` | SHA-256 of the dump before encryption |
| `advanced_restore.capabilities` | `["full", "incremental"]`, written as a fixed pair for every engine |
| `advanced_restore.incremental_lineage.engine` | `mysql` |

`value` and `plaintext_value` are identical because this artifact is not encrypted. When encryption
is enabled they differ: `plaintext_value` fingerprints the dump, `value` fingerprints the ciphertext.
See [Enable encryption](../../guides/enable-encryption.md).

## Step 6: Check the execution history

Every run is recorded, whether it succeeded or failed:

```bash
sentinel monitor list --config sentinel.yaml
```

**What to look for**: one row under the headers `ID`, `JOB`, `TYPE`, `CHAIN`, `STATUS`, `TIMESTAMP`,
`DURATION`, `DELTA`, `ERROR`. `JOB` is `shop`, `TYPE` is `full`, `STATUS` is `success`, and `CHAIN`
and `DELTA` are both `-` because incremental backup is off. Both of those columns come alive on the
[binary-log page](./incremental-binlog.md). Note the `ID`; it is a fresh UUID per execution and the
next step uses it.

For an aggregate view of one job:

```bash
sentinel monitor stats --job shop --config sentinel.yaml
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

**What to look for**: one row with `STATUS` `ok` and `HASH_MATCH` `true`, then a summary line
counting four outcomes: `ok`, `corrupted` (hash mismatch), `missing_artifact` (the file is gone), and
`missing_manifest` (there is nothing to compare against).

Or verify a single backup by its execution ID:

```bash
sentinel backup verify <execution-id> --config sentinel.yaml
```

**What to look for**: a `PASS:` line naming the backup, followed by the database, the file path, the
hash, and `Status: PASS`.

The sweep exits non-zero when anything is wrong, so it works as a cron or CI gate. Use
`--ignore-missing-manifest` to downgrade the last counter to a warning when a repository contains
older artifacts written before you set `output`. See
[Verify backup integrity](../../guides/verify-backup-integrity.md).

## Step 8: Supply credentials from a `my.cnf` instead

This is the one credential mechanism MySQL and MariaDB have that PostgreSQL and MongoDB do not, and
it is worth knowing because it is how most MySQL estates already store client credentials.

```bash
cat > client.cnf <<'EOF'
[client]
host = 127.0.0.1
port = 3307
user = root
password = tutorial
EOF

chmod 0600 client.cnf
```

Then replace the four connection keys on the job with one:

```yaml
databases:
  shop:
    type: mysql
    defaults_file: ./client.cnf
    database: shop
    output: shop.sql
```

Sentinel reads the file once, at configuration load, and parses only its `[client]` section, taking
`host`, `port`, `user`, and `password`. Four things follow from that, and each one surprises someone:

- **The values seed the whole pipeline, not just the dump subprocess.** They fill in `host`, `port`,
  `username`, and the password for every command that reads this job, so a job with a
  `defaults_file` needs no `password_env` at all. Validation, which normally insists on
  `password_env`, accepts its absence once the file has supplied a password.
- **Explicit YAML wins.** Each field is filled only when the corresponding job key is empty, so you
  can keep `defaults_file` for the password and still override the host in YAML.
- **A file with no `[client]` section is ignored, not rejected.** Sentinel treats it as supplying
  nothing and moves on, which means a typo in the section header surfaces later as
  `password_env is required` rather than as a file error.
- **Permissions are checked.** A group- or world-readable file produces a warning on standard error
  recommending `chmod 0600`. The warning is suppressed for a file that is encrypted at rest.

Two companion keys are worth knowing: `defaults_file_env` names an environment variable holding the
path, for deployments where the mount location is only known at run time; and, from v1.4.0,
`sentinel security encrypt-secrets-file` produces an encrypted `my.cnf` that Sentinel decrypts in
memory. Both are covered in [Database credentials](../../guides/database-credentials.md).

:::danger `defaults_file` is a backup-job key only
There is no `defaults_file` on a restore job. A restore job resolves its password from
`password_env` and nothing else, so the credentials you put in a `my.cnf` here will not be picked up
by the [restore page](./restore.md). Configuration validation enforces this from the other
direction: `defaults_file` on a non-MySQL, non-MariaDB backup job is rejected outright.
:::

## What just happened

Sentinel resolved the password, connected to the server, ran `mysqldump` with the flags your
`database_options` produced, hashed the output on its way to `./backups/shop.sql`, wrote the digest
and metadata into `shop.sql.manifest.json`, and appended a row to `./history.db`. The verify step
re-read the artifact from disk and recomputed the digest independently.

:::caution `--compress` does nothing on MySQL
The `sentinel backup --compress` flag is read only by the PostgreSQL and MongoDB dump paths. The
MySQL and MariaDB argument builders have no compression field, so the flag is accepted and silently
discarded. For a smaller artifact, use the pipeline `compression:` block, which runs after the dump
and is engine-agnostic; see [Backup compression](../../guides/backup-compression.md).
:::

Everything else on this page is the plain, full-dump path. Nothing was encrypted, chained, or
uploaded anywhere; those are configuration, not different code. See
[Backup](../../concepts/backup.md) for the model behind what you just ran.

## Next

- **[Restoring into a second database](./restore.md)**: prove the artifact is usable, which is the
  only test of a backup that counts.

<!-- sources: internal/cli/backup.go, internal/cli/backup_verify.go, internal/cli/monitor.go, internal/cli/config.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go, internal/config/marshal.go, internal/config/defaults_file_resolve.go, internal/adapters/mysqlargs/core.go, internal/adapters/mysqlargs/defaults_file.go, internal/adapters/dump/mysql/mysql_dump.go, internal/adapters/dump/mysql/args_builder.go, internal/domain/backup/pipeline.go, internal/utils/default.go, infra/docker/docker-compose.yml -->
