---
title: Restoring a PostgreSQL backup
description: Rehearse a restore with a dry run, then restore the artifact into a second PostgreSQL database and confirm the rows came back.
sidebar_position: 3
---

By the end of this page you will have a second database, `shop_drill`, populated from the artifact
you produced on the previous page; plus a recorded restore execution you can point at when someone
asks whether the backups actually work.

Budget about fifteen minutes.

## What you need

- The working directory, configuration, and `backups/shop.sql` artifact from
  [Your first PostgreSQL backup](./first-backup.md). This page edits that same `sentinel.yaml`.
- The `sentinel-pg-tutorial` container still running. If you stopped it, restart the track from the
  previous page; a restore needs both a live server and an artifact.
- `PGPASSWORD` still exported in your shell:

  ```bash
  export PGPASSWORD=tutorial
  ```

If you need a fresh server, the repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up a PostgreSQL 17 instance alongside the other supported engines.

## Step 1: Describe the restore job

Restores are configured, not improvised. Append to `sentinel.yaml`:

```yaml
restore:
  staging_dir: ./staging

restores:
  shop-drill:
    type: postgres
    enabled: true
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: PGPASSWORD
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

Three keys are worth pausing on:

- **`schedule` is required even when you only ever run the job by hand.** Omit it and validation
  fails with `restore 'shop-drill': restore schedule (cron) is required`. It is the cron expression
  `sentinel schedule start` would use; `sentinel restore run` ignores it.
- **`conflict_strategy: error`** is the default and the safe one: fail rather than overwrite. The
  alternatives are `replace` and `ignore`. PostgreSQL `replace` operations that could drop dependent
  objects additionally require `allow_cascade: true`.
- **`restore.staging_dir`** is where artifacts are staged before being applied. The default is
  `/tmp/sentinel`; pointing it at the working directory keeps the tutorial self-contained.

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
    Type: postgres
    Schedule: 0 4 * * 0
    Status: enabled
    Database: shop_drill
```

:::note Restore commands log as JSON

`sentinel restore …` emits structured JSON log lines regardless of the top-level `log_format: text`
setting, so its output looks different from `backup` and `monitor`. The human-readable report is
still printed to standard output underneath.
:::

## Step 2: Create the target database

```bash
psql -h 127.0.0.1 -U postgres -d postgres -c "CREATE DATABASE shop_drill;"
```

You should see `CREATE DATABASE`.

Restoring over `shop` would prove nothing and destroy your source data. A restore drill is only
meaningful against a target you are willing to lose.

## Step 3: Rehearse with a dry run

A dry run resolves the job, the source, and the restore plan, and reports what would happen without
touching the target:

```bash
sentinel restore dry-run shop-drill --config sentinel.yaml
```

You should see:

```text
Dry-run: Job "shop-drill"
  Type: postgres
  Database: shop_drill
  Backup Source Type: local
  Backup Path: shop.sql
  Restore Mode: full
  Timeout: 0 seconds

NOTE: This is a dry-run. No data will be restored.
```

`Restore Mode: full` is the default when `restore_mode` is unset. The other two modes,
[`incremental`](./incremental-wal.md) and [`pitr`](./pitr.md), come later in this track.

## Step 4: Run the restore

:::danger Destructive
This writes into `shop_drill` and can overwrite whatever is there. Confirm the artifact you are
about to apply is intact first:

```bash
sentinel backup verify --all --config sentinel.yaml
```

Never point a restore job at a database you cannot afford to lose. `conflict_strategy: error` makes
Sentinel refuse rather than clobber, but it is a guard rail, not a substitute for choosing the right
target.
:::

```bash
sentinel restore run shop-drill --config sentinel.yaml
```

You should see:

```text
{"time":"2026-08-05T17:44:20.612807Z","level":"INFO","msg":"hash verification passed","backup_id":"shop","hash":"5d28103153eea0f1a5445ad5e5a9a99cf54ec701e77acbb19cbf7f4971c8767d"}
Restore job "shop-drill" completed successfully
```

The `hash verification passed` line is the manifest doing its job: before applying anything, Sentinel
re-read `backups/shop.sql`, recomputed its SHA-256, and compared it against
`backups/shop.sql.manifest.json`. A mismatch aborts the restore. That check is exactly why the
previous page insisted on setting `output`.

## Step 5: Confirm the rows came back

```bash
psql -h 127.0.0.1 -U postgres -d shop_drill -c \
  "SELECT c.name, o.total FROM customers c JOIN orders o ON o.customer_id = c.id ORDER BY o.id;"
```

You should see:

```text
     name     | total
--------------+-------
 Ada Lovelace | 42.00
 Grace Hopper | 17.50
 Alan Turing  | 99.99
(3 rows)
```

That is the loop closed: the artifact is not just present and hash-clean, it reconstructs the data.

## Step 6: Check the restore history

Restores are recorded in the same history database as backups, in their own table:

```bash
sentinel restore history shop-drill --config sentinel.yaml
```

You should see:

```text
RESTORE | DATABASE | MODE | PLAN | STATUS | DURATION | TIMESTAMP | REASON | FALLBACK
shop-drill | shop_drill | full | ready | success | 102ms | 2026-08-05T17:44:20Z | - | none
```

`PLAN` and `REASON` are the planner's verdict. Here the plan was `ready` with no reason code because
a full restore has nothing to decide. On the [incremental](./incremental-wal.md) and
[PITR](./pitr.md) pages those columns carry the interesting information.

For the job's current configuration rather than its history:

```bash
sentinel restore status shop-drill --config sentinel.yaml
```

You should see:

```text
Restore Job: shop-drill
  Type: postgres
  Database: shop_drill
  Schedule: 0 4 * * 0
  Status: enabled
  Restore Mode: full
  Verify After Restore: false
  Timeout: 0 seconds
  Keep File: false
```

## If it goes wrong

Three failures are likely on a first run.

**`failed to acquire restore lock: … mkdir /var/run/sentinel: permission denied`**: you left
`scheduler.lock_dir` unset and are not running as root. Restores always take a per-job file lock;
backups in this configuration do not. Set `lock_dir` to a writable path, as the configuration on the
[previous page](./first-backup.md) does.

**`failed to run psql restore command - exit status 3`**: the target already contains the objects
in the artifact, and `conflict_strategy: error` refused to overwrite them. This is what you get from
running Step 4 twice. Drop and recreate the target:

```bash
psql -h 127.0.0.1 -U postgres -d postgres \
  -c "DROP DATABASE shop_drill;" -c "CREATE DATABASE shop_drill;"
```

**`verification handler is required for restore mode "full"`**: you set `verify_after_restore:
true`. In v1.4.0 the restore runtime does not supply a post-restore verification handler, so any job
that requests one fails at the final step. The restore itself has already completed by then: the
data is in the target, but the run is recorded as `failed`. Leave `verify_after_restore` unset and
verify with a `psql` query, as Step 5 does.

## What just happened

Sentinel resolved the newest matching object in `./backups`, staged it under `./staging`, verified
its SHA-256 against the manifest, ran `psql` against `shop_drill`, removed the staged copy, and
recorded the execution. The plan (`full`) was computed before any of that, from the restore job's
mode and the artifact's manifest; the same planner the next two pages push harder.

See [Restore](../../concepts/restore.md) for the model, and
[`sentinel restore`](../../reference/cli/restore.md) for every flag.

## Next

- **[Incremental backup chains](./incremental-wal.md)**: stop taking a full dump every time, and
  learn what Sentinel's chain metadata does and does not give you on PostgreSQL.

<!-- sources: internal/cli/restore.go, internal/config/restore_types.go, internal/config/validator.go, internal/config/loader.go, internal/domain/restore/executor.go, internal/domain/restore/planner.go, internal/adapters/restore/runtime/executor.go, internal/adapters/restore/pg/pg_restore.go, docs/runbooks/restore-from-backup.md, docs/runbooks/restore-rehearsal.md -->
