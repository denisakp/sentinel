---
title: Restoring a MariaDB backup
description: Rehearse a restore with a dry run, then apply the artifact into a second MariaDB database and confirm the rows came back.
sidebar_position: 3
---

By the end of this page you will have a second database, `shop_drill`, populated from the artifact
you produced on the previous page; plus a recorded restore execution you can point at when someone
asks whether the backups actually work.

Budget about fifteen minutes.

## What you need

- The working directory, configuration, and `backups/shop.sql` artifact from
  [Your first MariaDB backup](./first-backup.md). This page edits that same `sentinel.yaml`.
- The `sentinel-mariadb-tutorial` container still running. If you stopped it, restart the track from
  the previous page; a restore needs both a live server and an artifact.
- `mariadb` on your `PATH`. Sentinel applies a MariaDB restore by piping the artifact into the
  `mariadb` client; there is no `mariadb-restore` binary and Sentinel does not look for one.
- `MYSQL_PWD` still exported in your shell:

  ```bash
  export MYSQL_PWD=tutorial
  ```

If you need a fresh server, the repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up a `mariadb:lts` instance on host port `3306` alongside the other supported engines.

## Step 1: Describe the restore job

Restores are configured, not improvised. Append to `sentinel.yaml`:

```yaml
restore:
  staging_dir: ./staging

restores:
  shop-drill:
    type: mariadb
    enabled: true
    host: 127.0.0.1
    port: 3306
    username: root
    password_env: MYSQL_PWD
    database: shop_drill
    schedule: "0 4 * * 0"
    conflict_strategy: error
    backup_source:
      type: local
      local_path: ./backups
      backup_path: shop.sql
```

`restores` is a separate top-level block from `databases`: a restore job has its own target,
credentials, and source, because the whole point of a restore drill is that it does not run against
the database you backed up.

Four keys are worth pausing on:

- **`enabled: true` is not optional.** Restore jobs default to disabled, deliberately, so that a
  configuration file cannot start overwriting databases by being loaded. Omit the key and the job
  appears in `sentinel restore list` as `disabled`.
- **`schedule` is required even when you only ever run the job by hand.** Omit it on an enabled job
  and validation fails with `restore 'shop-drill': restore schedule (cron) is required`. It is the
  cron expression the scheduler would use; `sentinel restore run` ignores it.
- **`conflict_strategy: error`** is the default and the safe one: fail rather than press on. See the
  note below for what the alternatives actually do on MariaDB.
- **`restore.staging_dir`** is where artifacts are staged before being applied. The default is
  `/tmp/sentinel`; pointing it at the working directory keeps the tutorial self-contained. A job may
  override it with its own `staging_dir`, and validation requires that one or the other resolves to
  a value.

:::caution `replace` and `ignore` are the same thing on MariaDB
Of the three strategies the validator accepts, only `error` changes behaviour. Both `replace` and
`ignore` are translated into a single `--force` argument to the `mariadb` client, which tells it to
keep going after an SQL error rather than to drop or skip conflicting objects. There is no
`--clean`-style object removal on this engine, so `replace` does not mean "replace": it means "do
not stop". `allow_cascade`, the PostgreSQL companion to `replace`, is rejected on a MariaDB job.
:::

:::danger `restore_options` are PostgreSQL flags, and they are not engine-gated
Setting `clean`, `if_exists`, `no_owner`, or `no_privileges` to `true` under `restore_options`
appends `--clean`, `--if-exists`, `--no-owner`, or `--no-privileges` to the command line regardless
of engine. Those are `pg_restore` options; the `mariadb` client rejects them and the restore fails
before it applies anything. Leave `restore_options` unset on MariaDB.
:::

Validate, then confirm Sentinel sees the job:

```bash
sentinel config validate --config sentinel.yaml
sentinel restore list --config sentinel.yaml
```

You should see:

```text
Restore Jobs:
=============
  Name: shop-drill
    Type: mariadb
    Schedule: 0 4 * * 0
    Status: enabled
    Database: shop_drill
```

:::note Restore commands log as JSON

`sentinel restore …` emits structured JSON log lines regardless of the top-level `log_format: text`
setting, so its output looks different from `backup` and `monitor`:

```text
{"time":"2026-08-05T20:56:56.769999Z","level":"WARN","msg":"TLS not configured for database","event":"tls_not_configured","database":"shop"}
```

The human-readable report is still printed to standard output underneath.
:::

## Step 2: Create the target database

```bash
mariadb -h 127.0.0.1 -P 3306 -u root -e "CREATE DATABASE shop_drill;"
```

Restoring over `shop` would prove nothing and destroy your source data. A restore drill is only
meaningful against a target you are willing to lose.

Sentinel does not create the target for you. The `mariadb` client is invoked with the database name
as its final argument, so the database must already exist. Its pre-flight connectivity probe will not
catch a missing one: the probe runs `SELECT 1` with no database selected, so it passes, and the
failure arrives later when the dump is applied.

## Step 3: Rehearse with a dry run

A dry run resolves the job and the source and reports what would happen without touching the target:

```bash
sentinel restore dry-run shop-drill --config sentinel.yaml
```

You should see:

```text
Dry-run: Job "shop-drill"
  Type: mariadb
  Database: shop_drill
  Backup Source Type: local
  Backup Path: shop.sql
  Restore Mode: full
  Timeout: 0 seconds

NOTE: This is a dry-run. No data will be restored.
```

`Restore Mode: full` is the default when `restore_mode` is unset. The other two modes,
[`incremental`](./incremental-binlog.md) and [`pitr`](./pitr.md), come later in this track, and
neither of them completes on MariaDB.

:::caution A dry run does not plan
The dry run echoes the resolved job configuration and stops; it does not invoke the restore planner
and it does not connect to anything. Use it to check connection details and source resolution, not
to predict whether a restore will proceed. On the [binary-log page](./incremental-binlog.md) you
will see a job that a dry run reports cleanly and that fails the moment you run it.
:::

## Step 4: Run the restore

:::danger Destructive
This writes into `shop_drill` and can overwrite whatever is there. Confirm the artifact you are
about to apply is intact first:

```bash
sentinel backup verify --all --config sentinel.yaml
```

Never point a restore job at a database you cannot afford to lose. `conflict_strategy: error` makes
the `mariadb` client stop at the first error rather than plough on, but it is a guard rail, not a
substitute for choosing the right target.
:::

```bash
sentinel restore run shop-drill --config sentinel.yaml
```

**What to look for**: a JSON log line at level `INFO` with `"msg":"hash verification passed"`
carrying the backup ID and hash, then `Restore job "shop-drill" completed successfully`.

That hash line is the manifest doing its job: before applying anything, Sentinel re-read
`backups/shop.sql`, recomputed its SHA-256, and compared it against
`backups/shop.sql.manifest.json`. A mismatch aborts the restore. That check is exactly why the
previous page insisted on setting `output`.

## Step 5: Confirm the rows came back

```bash
mariadb -h 127.0.0.1 -P 3306 -u root shop_drill -e \
  "SELECT c.name, o.total FROM customers c JOIN orders o ON o.customer_id = c.id ORDER BY o.id;"
```

**What to look for**: three rows, `Ada Lovelace / 42.00`, `Grace Hopper / 17.50`, and
`Alan Turing / 99.99`, matching what you inserted on the previous page.

That is the loop closed: the artifact is not just present and hash-clean, it reconstructs the data.

## Step 6: Check the restore history

Restores are recorded in the same history database as backups, in their own table:

```bash
sentinel restore history shop-drill --config sentinel.yaml
```

**What to look for**: a header row and one record:

```text
RESTORE | DATABASE | MODE | PLAN | STATUS | DURATION | TIMESTAMP | REASON | FALLBACK
```

with `MODE` `full`, `PLAN` `ready`, `STATUS` `success`, `REASON` `-`, and `FALLBACK` `none`. `PLAN`
and `REASON` are the planner's verdict; a full restore has nothing to decide, so the reason is empty.
On the [binary-log page](./incremental-binlog.md) those two columns carry the whole story.

For the job's current configuration rather than its history:

```bash
sentinel restore status shop-drill --config sentinel.yaml
```

You should see:

```text
Restore Job: shop-drill
  Type: mariadb
  Database: shop_drill
  Schedule: 0 4 * * 0
  Status: enabled
  Restore Mode: full
  Verify After Restore: false
  Timeout: 0 seconds
  Keep File: false
```

## If it goes wrong

Four failures are likely on a first run.

**`failed to acquire restore lock: … mkdir /var/run/sentinel: permission denied`**: you left
`scheduler.lock_dir` unset and are not running as root. Restores always take a per-job file lock;
backups in this configuration do not. Set `lock_dir` to a writable path, as the configuration on the
[previous page](./first-backup.md) does.

**`connectivity check failed - cannot connect to MariaDB database root@127.0.0.1:3306`**: the restore
adapter runs `mariadb … -e "SELECT 1"` against the server before applying anything. This is what you
get when the container is stopped, when the port is wrong, or when the password is wrong. The
password reaching this check comes from `password_env` only; a `defaults_file` on the backup job does
not apply to restore jobs. Note that this message names MariaDB, unlike the backup-side ping, which
names MySQL.

**`failed to start mariadb command`**: the `mariadb` client exited non-zero while applying the dump.
With `conflict_strategy: error` the likeliest cause is that the target already contains the objects
in the artifact, which is what you get from running Step 4 twice. Drop and recreate the target:

```bash
mariadb -h 127.0.0.1 -P 3306 -u root \
  -e "DROP DATABASE shop_drill; CREATE DATABASE shop_drill;"
```

**`verification handler is required for restore mode "full"`**: you set `verify_after_restore: true`.
In v1.4.0 the restore runtime does not supply a post-restore verification handler, so any job that
requests one fails at the final step. The restore itself has already completed by then: the data is
in the target, but the run is recorded as `failed`. Leave `verify_after_restore` unset and verify
with a `mariadb` query, as Step 5 does.

## What just happened

Sentinel resolved the object in `./backups`, staged it under `./staging`, verified its SHA-256
against the manifest, ran `mariadb` with the staged file on standard input, removed the staged copy,
and recorded the execution. The plan (`full`) was computed before any of that, from the restore
job's mode and the artifact's manifest; the same planner the next two pages push until it refuses.

Apart from the binary it executes and the wording of its connectivity error, the MariaDB restore
adapter is line-for-line the MySQL one. Anything you learn here transfers, and vice versa.

See [Restore](../../concepts/restore.md) for the model, and
[`sentinel restore`](../../reference/cli/restore.md) for every flag.

## Next

- **[Binary-log archival](./incremental-binlog.md)**: turn on incremental backup, watch Sentinel pack
  the binary logs beside each artifact, and find out exactly where the restore half stops.

<!-- sources: internal/cli/restore.go, internal/config/restore_types.go, internal/config/validator.go, internal/config/loader.go, internal/config/marshal.go, internal/domain/restore/executor.go, internal/domain/restore/planner.go, internal/adapters/restore/runtime/executor.go, internal/adapters/restore/runtime/preflight.go, internal/adapters/restore/mariadb/mariadb_restore.go, internal/adapters/restore/mariadb/args_factory.go, docs/runbooks/restore-from-backup.md -->
