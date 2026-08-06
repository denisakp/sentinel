---
title: Your first MariaDB backup
description: Build a MariaDB backup configuration from nothing, take a backup with mariadb-dump, and verify its recorded SHA-256.
sidebar_position: 2
---

By the end of this page you will have a MariaDB database called `shop`, a Sentinel configuration that
keeps every file it creates inside one directory, a backup artifact with a manifest beside it, and
proof from Sentinel's own integrity check that the artifact has not changed since it was written.

Budget about twenty minutes. Everything you create here is reused by the next three pages, so do not
delete the directory when you finish.

## What you need

- Sentinel installed: see [Installation](../../intro/installation.md).
- **`mariadb-dump` and `mariadb` on your `PATH`.** Sentinel runs `mariadb-dump` as a subprocess to
  produce the backup and `mariadb` to apply a restore. These names are hard-coded; see the warning on
  the [track index](./index.md) if your client package still installs `mysqldump` and `mysql`.
- Docker, for a throwaway server.

The commands below start a single container. If you would rather have every supported engine running
at once, the repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up `mariadb:lts` on host port `3306` alongside MySQL, PostgreSQL 17, MongoDB, and an
S3-compatible object store. This page uses the same port.

## Step 1: Start a throwaway MariaDB

```bash
docker run --name sentinel-mariadb-tutorial \
  -e MYSQL_ROOT_PASSWORD=tutorial \
  -e MYSQL_DATABASE=shop \
  -p 3306:3306 -d mariadb:lts \
  --log-bin=mariadb-bin
```

The `MYSQL_`-prefixed variables are the ones the repository's own compose file uses for its MariaDB
service, and the image still honours them; `MARIADB_ROOT_PASSWORD` and `MARIADB_DATABASE` are
equivalent if you prefer the modern spelling.

The trailing `--log-bin=mariadb-bin` is not needed for this page. It is needed for
[binary-log archival](./incremental-binlog.md) two pages from now, and setting it at creation time
saves you recreating the container later.

:::note This is a real MariaDB difference
MySQL 8 enables binary logging by default; MariaDB does not. On MariaDB the `--log-bin` argument is
what turns the feature on at all, not merely what renames the files. Sentinel additionally only
collects log files whose names begin with `mariadb-bin.`, which is why the argument carries a value
here rather than being passed bare.
:::

The server takes a few seconds to initialise. Export the password once, into the variable the
`mariadb` client and Sentinel both read, then confirm the server is up:

```bash
export MYSQL_PWD=tutorial

mariadb -h 127.0.0.1 -P 3306 -u root -e "SELECT VERSION(); SHOW VARIABLES LIKE 'log_bin%';"
```

`MYSQL_PWD` keeps its MySQL-era name on MariaDB. Sentinel sets exactly this variable when it invokes
`mariadb-dump` and `mariadb`, and never puts the password on a command line, because anything on a
command line is visible in `ps` output and `/proc/<pid>/cmdline`.

**What to look for**: a version string beginning `11.` (or whatever `lts` currently points at),
`log_bin` reported as `ON`, and `log_bin_basename` ending in `mariadb-bin`. If `log_bin` is `OFF`,
the `--log-bin` argument did not reach the server; remove the container and start it again.

## Step 2: Put some data in it

```bash
mariadb -h 127.0.0.1 -P 3306 -u root shop -e "
CREATE TABLE customers (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(120) NOT NULL, email VARCHAR(180) NOT NULL);
CREATE TABLE orders (id INT AUTO_INCREMENT PRIMARY KEY, customer_id INT, total DECIMAL(10,2), placed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY (customer_id) REFERENCES customers(id));
INSERT INTO customers (name, email) VALUES ('Ada Lovelace','ada@example.invalid'),('Grace Hopper','grace@example.invalid'),('Alan Turing','alan@example.invalid');
INSERT INTO orders (customer_id, total) VALUES (1, 42.00), (2, 17.50), (3, 99.99);"
```

The client prints nothing on success. Confirm the rows landed:

```bash
mariadb -h 127.0.0.1 -P 3306 -u root shop -e "SELECT COUNT(*) FROM customers; SELECT COUNT(*) FROM orders;"
```

**What to look for**: `3` from each count.

## Step 3: Write the configuration

Make a working directory and change into it; every path in this track is relative to it:

```bash
mkdir sentinel-mariadb-tutorial && cd sentinel-mariadb-tutorial
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
    type: mariadb
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PWD
    database: shop
    output: shop.sql
    database_options:
      single_transaction: true
      routines: true
      triggers: true
```

`type: mariadb` is what selects the `mariadb-dump` and `mariadb` binaries. Everything else on this
job has the same meaning it has on MySQL.

Five of those keys deserve an explanation, because each one is a decision you will make again on
every real deployment.

**`password_env: MYSQL_PWD` names an environment variable; it is not the password.** In YAML,
`password_env` is the only key that supplies a password directly: there is no key that takes a
literal one. The MariaDB-specific alternative, a `my.cnf` file, is covered in Step 8 below. On the
command line the equivalents are `--password-env <VAR>` and `--password-file <PATH>`.

**`history_db_path: ./history.db` keeps the execution history local.** The default is
`~/.sentinel/history.db`, shared by every configuration on the machine.

**`scheduler.lock_dir: ./locks` keeps the job locks local.** The default is `/var/run/sentinel`,
which an unprivileged user cannot create. You will meet the failure this avoids on the
[restore page](./restore.md).

**`output: shop.sql` names the artifact, and that is what produces the manifest.** A value with no
file extension gets `.sql` appended; `shop.sql` is kept verbatim.

**`database_options` are dump-tool flags, expressed as booleans.** MariaDB and MySQL share this
table exactly; Sentinel recognises four keys and translates each true value into one `mariadb-dump`
flag:

| Key | Flag added |
|---|---|
| `single_transaction` | `--single-transaction` |
| `routines` | `--routines` |
| `triggers` | `--triggers` |
| `events` | `--events` |

Anything else in the block is ignored silently. `single_transaction` is the one that matters most on
a live InnoDB database: it takes the dump from a consistent snapshot without locking every table. For
flags outside that list, use `additional_args`; see the
[additional arguments reference](../../reference/additional-args.md).

:::note `output` is what gives you a manifest

Without `output`, the storage backend invents a timestamped name but Sentinel does not learn it, so
it writes no `.manifest.json` sidecar. `sentinel backup verify` then has nothing to compare against
and reports `missing_manifest`. Setting `output` explicitly is what makes Step 7 on this page work.

The trade-off is that a fixed name is overwritten on every run. That is fine for a tutorial; for a
real repository, run backups through the scheduler, which builds a timestamped name and records it.
:::

Check the file before going further:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
2026/08/05 20:58:09 WARN TLS not configured for database event=tls_not_configured database=shop
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
(printed by the MariaDB dump adapter once the write returns). On the very first run two further lines
report that Sentinel created `./history.db` and migrated it to the schema version this binary
requires; they do not appear again.

:::note A MariaDB backup is probed as MySQL
Before running `mariadb-dump`, Sentinel pings the server to check it is reachable. That ping is
issued with the database type set to `mysql` even for a MariaDB job, because the two speak the same
wire protocol and share one Go driver. It is a deliberate reuse rather than a bug, but it explains
why a connection failure at this stage may name MySQL.

If the dump itself fails, the error reads `failed to execute maridb-dump command`, with `maridb`
misspelled. Search for that exact string rather than the correct spelling; the standard error from
the dump tool follows it, with any credentials redacted.
:::

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

The fields it carries, and what each one means for MariaDB:

| Field | Value on this artifact |
|---|---|
| `backup_id` | `shop`, the job name |
| `database_type` | `mariadb` |
| `hash.value` | SHA-256 of the stored bytes |
| `hash.plaintext_value` | SHA-256 of the dump before encryption |
| `advanced_restore.capabilities` | `["full", "incremental"]`, written as a fixed pair for every engine |
| `advanced_restore.incremental_lineage.engine` | `mariadb` |

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

This is the one credential mechanism MariaDB and MySQL have that PostgreSQL and MongoDB do not.

```bash
cat > client.cnf <<'EOF'
[client]
host = 127.0.0.1
port = 3306
user = root
password = tutorial
EOF

chmod 0600 client.cnf
```

Then replace the four connection keys on the job with one:

```yaml
databases:
  shop:
    type: mariadb
    defaults_file: ./client.cnf
    database: shop
    output: shop.sql
```

Sentinel reads the file once, at configuration load, and parses only its `[client]` section, taking
`host`, `port`, `user`, and `password`. Four things follow from that:

- **The values seed the whole pipeline, not just the dump subprocess.** They fill in `host`, `port`,
  `username`, and the password for every command that reads this job, so a job with a
  `defaults_file` needs no `password_env` at all. Validation, which normally insists on
  `password_env`, accepts its absence once the file has supplied a password.
- **Explicit YAML wins.** Each field is filled only when the corresponding job key is empty, so you
  can keep `defaults_file` for the password and still override the host in YAML.
- **A file with no `[client]` section is ignored, not rejected.** A typo in the section header
  surfaces later as `password_env is required` rather than as a file error.
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
direction: `defaults_file` is accepted only on a `mysql` or `mariadb` backup job and rejected
everywhere else.
:::

:::note One argument MariaDB does not get
When the resolved password is empty, the shared argument builder appends `--skip-password` for a
MySQL job and does not for a MariaDB one. The consequence is that a MariaDB job configured with no
password at all leaves `mariadb-dump` to decide what to do about authentication, where the MySQL
equivalent explicitly tells `mysqldump` not to prompt. Give MariaDB jobs a password, through
`password_env` or a `defaults_file`, rather than relying on the empty case.
:::

## What just happened

Sentinel resolved the password, pinged the server, ran `mariadb-dump` with the flags your
`database_options` produced, hashed the output on its way to `./backups/shop.sql`, wrote the digest
and metadata into `shop.sql.manifest.json`, and appended a row to `./history.db`. The verify step
re-read the artifact from disk and recomputed the digest independently.

:::caution `--compress` does nothing on MariaDB
The `sentinel backup --compress` flag is read only by the PostgreSQL and MongoDB dump paths. The
MariaDB and MySQL argument builders have no compression field, so the flag is accepted and silently
discarded. For a smaller artifact, use the pipeline `compression:` block, which runs after the dump
and is engine-agnostic; see [Backup compression](../../guides/backup-compression.md).
:::

See [Backup](../../concepts/backup.md) for the model behind what you just ran.

## Next

- **[Restoring into a second database](./restore.md)**: prove the artifact is usable, which is the
  only test of a backup that counts.

{/* sources: internal/cli/backup.go, internal/cli/backup_verify.go, internal/cli/monitor.go, internal/cli/config.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go, internal/config/marshal.go, internal/config/defaults_file_resolve.go, internal/adapters/mysqlargs/core.go, internal/adapters/mysqlargs/defaults_file.go, internal/adapters/dump/mariadb/mariadb_dump.go, internal/adapters/dump/mariadb/args_builder.go, internal/domain/backup/pipeline.go, internal/utils/default.go, infra/docker/docker-compose.yml */}
