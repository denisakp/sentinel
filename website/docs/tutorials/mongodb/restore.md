---
title: Restoring a MongoDB backup
description: Rehearse a restore with a dry run, then restore the archive into a second MongoDB server and confirm the documents came back.
sidebar_position: 3
---

By the end of this page you will have a second MongoDB server holding the `catalog` collection you
backed up on the previous page, plus a recorded restore execution you can point at when someone asks
whether the backups actually work.

Budget about twenty minutes.

## What you need

- The working directory, configuration, and `backups/catalog.archive` artifact from
  [Your first MongoDB backup](./first-backup.md). This page edits that same `sentinel.yaml`.
- The `sentinel-mongo-tutorial` container still running. If you stopped it, restart the track from
  the previous page; a restore drill needs both an artifact and somewhere to put it.
- **`mongorestore` and `mongosh` on your `PATH`.** Both are hard requirements here: Sentinel runs
  `mongosh --eval "db.version()"` as a connectivity check before it will invoke `mongorestore`, and
  a missing `mongosh` fails the restore with
  `connectivity check failed - cannot connect to MongoDB at URI … exec: "mongosh": executable file not found in $PATH`.
- `MONGO_URI` still exported in your shell:

  ```bash
  export MONGO_URI='mongodb://127.0.0.1:27017/?directConnection=true'
  ```

If you need a fresh source server, the repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up one alongside the other supported engines, though as the previous page noted its `mongo`
service is not reachable from the host. Prefer the `docker run` command from that page.

## Step 1: Start a second server as the restore target

```bash
docker run --name sentinel-mongo-drill -p 27018:27017 -d mongo:noble
```

Confirm it is up:

```bash
docker exec sentinel-mongo-drill mongosh --quiet --eval 'db.hello().isWritablePrimary'
```

This should print `true`. This container is a plain standalone `mongod`: a restore target does not
need a replica set.

Restoring over the source would prove nothing and destroy your data. A restore drill is only
meaningful against a target you are willing to lose.

:::note Why a second server rather than a renamed database

On the SQL engines a restore drill points at a second database on the same server. A `mongodump`
archive is different: it carries its own namespaces, so the databases and collections inside it are
restored under the names they were captured with. Using a second server keeps the artifact applied
exactly as captured, with no renaming step to get wrong.
:::

Export its URI:

```bash
export MONGO_DRILL_URI='mongodb://127.0.0.1:27018/?directConnection=true'
```

## Step 2: Describe the restore job

Restores are configured, not improvised. Append to `sentinel.yaml`:

```yaml
restore:
  staging_dir: ./staging

restores:
  catalog-drill:
    type: mongodb
    enabled: true
    uri_env: MONGO_DRILL_URI
    schedule: "0 4 * * 0"
    conflict_strategy: error
    restore_options:
      archive: true
    backup_source:
      type: local
      local_path: ./backups
      backup_path: catalog.archive
```

`restores` is a separate top-level block from `databases`: a restore job has its own target,
credentials, and source, because the whole point of a restore drill is that it does not run against
the database you backed up.

Five keys are worth pausing on, and two of them behave differently on MongoDB than anywhere else.

- **`uri_env` replaces the whole connection block.** A MongoDB restore job is validated only for a
  `uri` or `uri_env`; `host`, `username`, and `database` are not required. The same credential
  caution as on the backup side applies, with an extra edge described below.
- **There is no `database:` key here, deliberately.** For MongoDB it is optional, and the archive
  supplies the namespace. `sentinel restore list` and `sentinel restore status` will therefore print
  an empty `Database:` field for this job, which is expected rather than a misconfiguration.
- **`restore_options: {archive: true}` tells Sentinel the artifact is an archive**, so it is passed
  as `--archive=<staged-path>` rather than as a positional dump directory. It has to match how the
  backup was taken. If your backup used `database_options: {gzip: true}`, add `gzip: true` here too.
- **`schedule` is required even when you only ever run the job by hand.** Omit it and validation
  fails with `restore 'catalog-drill': restore schedule (cron) is required`. It is the cron
  expression `sentinel schedule start` would use; `sentinel restore run` ignores it.
- **`restore.staging_dir`** is where artifacts are staged before being applied. The default is
  `/tmp/sentinel`; pointing it at the working directory keeps the tutorial self-contained.

:::danger A failed connectivity check prints the URI unredacted

Before invoking `mongorestore`, Sentinel runs `mongosh` against the target and, on failure, reports
`cannot connect to MongoDB at URI <uri>` with the URI exactly as configured. If your URI carries a
password, that password is now in your terminal scrollback, your CI job log, and wherever those get
shipped. Tracked as issue 156.

Nothing on this page uses a password, which is the safest way to run a drill. On a real target:
supply the URI through `uri_env` so it never enters the configuration file or shell history, treat
any restore failure log as credential-bearing, and see
[Credential sanitization](../../concepts/credential-sanitization.md) for what Sentinel does and does
not redact.
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
  Name: catalog-drill
    Type: mongodb
    Schedule: 0 4 * * 0
    Status: enabled
    Database: 
```

:::note Restore commands log as JSON

`sentinel restore …` emits structured JSON log lines regardless of the top-level `log_format: text`
setting, so its output looks different from `backup` and `monitor`. The human-readable report is
still printed to standard output underneath.
:::

## Step 3: Rehearse with a dry run

A dry run resolves the job and the source and reports what would happen without touching the target:

```bash
sentinel restore dry-run catalog-drill --config sentinel.yaml
```

You should see:

```text
Dry-run: Job "catalog-drill"
  Type: mongodb
  Database: 
  Backup Source Type: local
  Backup Path: catalog.archive
  Restore Mode: full
  Timeout: 0 seconds

NOTE: This is a dry-run. No data will be restored.
```

`Restore Mode: full` is the default when `restore_mode` is unset. It is the only mode that reaches
execution on MongoDB; the [oplog page](./oplog-incremental.md) shows what happens to the other two.

:::caution A dry run does not plan

The dry run echoes the resolved job configuration and stops. It does not stage the artifact and does
not invoke the restore planner, so it cannot tell you whether a restore will actually proceed. Use it
to check connection details and source resolution, not as a prediction.
:::

## Step 4: Run the restore

:::danger Destructive
This writes into the `sentinel-mongo-drill` server and can overwrite whatever is there. Confirm the
archive you are about to apply is intact first:

```bash
sentinel backup verify --all --config sentinel.yaml
```

Never point a restore job at a server you cannot afford to lose. `conflict_strategy: error` is the
default and the safe one: it neither drops nor ignores, so an existing collection makes
`mongorestore` fail rather than clobber. It is a guard rail, not a substitute for choosing the right
target.
:::

```bash
sentinel restore run catalog-drill --config sentinel.yaml
```

Sentinel takes a per-job file lock, stages `catalog.archive` and its manifest into `./staging`,
re-reads the staged copy and recomputes its SHA-256 against the manifest, runs `mongosh` to check
connectivity, invokes `mongorestore --archive=<staged-path>`, removes the staged copy, and records
the execution. A hash mismatch aborts before anything is applied, which is exactly why the previous
page insisted on `output` and on `archive: true`.

On success the last line is:

```text
Restore job "catalog-drill" completed successfully
```

Above it, a JSON log line reports `hash verification passed` with the backup ID and the hash that
matched.

## Step 5: Confirm the documents came back

```bash
docker exec sentinel-mongo-drill mongosh --quiet catalog --eval \
  'db.products.find({}, {_id: 0, sku: 1, name: 1}).sort({sku: 1}).toArray()'
```

You should get the three documents you inserted on the previous page, with the same SKUs and names.
That is the loop closed: the artifact is not just present and hash-clean, it reconstructs the data.

## Step 6: Check the restore history

Restores are recorded in the same history database as backups, in their own table:

```bash
sentinel restore history catalog-drill --config sentinel.yaml
```

The output is a pipe-separated table:

```text
RESTORE | DATABASE | MODE | PLAN | STATUS | DURATION | TIMESTAMP | REASON | FALLBACK
```

For this run, `MODE` is `full`, `PLAN` is `ready`, `STATUS` is `success`, and `REASON` and `FALLBACK`
carry nothing interesting. `DATABASE` is blank, because the job has no `database:` key. `PLAN` and
`REASON` are the planner's verdict, and they become the most informative columns on the
[oplog page](./oplog-incremental.md), where the planner refuses.

For the job's current configuration rather than its history:

```bash
sentinel restore status catalog-drill --config sentinel.yaml
```

You should see:

```text
Restore Job: catalog-drill
  Type: mongodb
  Database: 
  Schedule: 0 4 * * 0
  Status: enabled
  Restore Mode: full
  Verify After Restore: false
  Timeout: 0 seconds
  Keep File: false
```

## If it goes wrong

Four failures are likely on a first run.

**`exec: "mongosh": executable file not found in $PATH`**, wrapped in
`connectivity check failed - cannot connect to MongoDB at URI …`. Sentinel's pre-restore check runs
`mongosh`, not the Go driver. Install the MongoDB Shell; `mongorestore` alone is not enough.

**`backup "catalog.archive" not found in local source: restore source object not found`**. The local
storage backend lists files only, never directories. If you took the backup without
`database_options: {archive: true}`, the artifact is a directory tree and the restore can never find
it. Retake the backup in archive mode.

**`failed to acquire restore lock: … mkdir /var/run/sentinel: permission denied`**: you left
`scheduler.lock_dir` unset and are not running as root. Restores always take a per-job file lock;
backups in this configuration do not. Set `lock_dir` to a writable path, as the configuration on the
[previous page](./first-backup.md) does.

**`verification handler is required for restore mode "full"`**: you set `verify_after_restore: true`.
In v1.4.0 the restore runtime does not supply a post-restore verification handler, so any job that
requests one fails at the final step. The restore itself has already completed by then: the data is
in the target, but the run is recorded as `failed`. Leave `verify_after_restore` unset and verify
with a `mongosh` query, as Step 5 does.

## What just happened

Sentinel resolved the configured object in `./backups`, staged it and its manifest under `./staging`
with mode `0600`, verified its SHA-256, checked connectivity, ran `mongorestore` against the drill
server, removed the staged copy, and recorded the execution. The plan (`full`) was computed before
any of that, from the restore job's mode and the artifact's manifest; the same planner the next page
pushes until it refuses.

See [Restore](../../concepts/restore.md) for the model, and
[`sentinel restore`](../../reference/cli/restore.md) for every flag.

## Next

- **[Oplog archival and incremental chains](./oplog-incremental.md)**: what MongoDB's incremental
  backup actually captures, and why the restore half of it cannot be reached.

<!-- sources: internal/cli/restore.go, internal/config/restore_types.go, internal/config/validator.go, internal/config/marshal.go, internal/config/loader.go, internal/domain/restore/executor.go, internal/domain/restore/planner.go, internal/adapters/restore/runtime/executor.go, internal/adapters/restore/runtime/staging.go, internal/adapters/restore/runtime/preflight.go, internal/adapters/restore/mongo/mongo_restore.go, internal/adapters/restore/mongo/args_factory.go, internal/adapters/storage/local/backend.go, docs/runbooks/restore-from-backup.md -->
