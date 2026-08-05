---
title: Your first MongoDB backup
description: Build a MongoDB backup configuration from nothing, take an archive backup, and verify its recorded SHA-256.
sidebar_position: 2
---

By the end of this page you will have a MongoDB database called `catalog`, a Sentinel configuration
that keeps every file it creates inside one directory, a single-file backup archive with a manifest
beside it, and proof from Sentinel's own integrity check that the archive has not changed since it
was written.

Budget about twenty-five minutes. Everything you create here is reused by the next three pages, so
do not delete the directory when you finish.

## What you need

- Sentinel installed: see [Installation](../../intro/installation.md).
- **MongoDB Database Tools** on your `PATH`: `mongodump` at minimum. Check with
  `mongodump --version`.
- Docker, for a throwaway server.

The commands below start a single container. The repository's
[`infra/docker/docker-compose.yml`](https://github.com/denisakp/sentinel/blob/develop/infra/docker/docker-compose.yml)
brings up MongoDB, PostgreSQL, MySQL, MariaDB, and an S3-compatible object store on a shared
`sentinel` network, but its `mongo` service publishes no port to the host and starts a standalone
`mongod`, so it cannot be driven from your shell and cannot serve the
[oplog page](./oplog-incremental.md) later. Start the container below instead.

## Step 1: Start a throwaway MongoDB

```bash
docker run --name sentinel-mongo-tutorial \
  -p 27017:27017 -d mongo:noble --replSet rs0
```

`--replSet rs0` is not needed for this page. It is needed for
[oplog archival](./oplog-incremental.md) two pages from now, because there is no `local.oplog.rs`
collection on a standalone `mongod`. Turning it on at creation time saves you rebuilding the
container later.

A member started with `--replSet` refuses most operations until the set is initiated. Give the
server a few seconds, then initiate a single-node set that advertises an address reachable from both
inside and outside the container:

```bash
docker exec sentinel-mongo-tutorial mongosh --quiet --eval \
  'rs.initiate({_id: "rs0", members: [{_id: 0, host: "127.0.0.1:27017"}]})'
```

`mongosh` prints the initiation result as a document. Confirm the node has become primary:

```bash
docker exec sentinel-mongo-tutorial mongosh --quiet --eval 'db.hello().isWritablePrimary'
```

This should print `true`. If it prints `false`, wait a few seconds and run it again; election takes
a moment.

## Step 2: Put some data in it

```bash
docker exec sentinel-mongo-tutorial mongosh --quiet catalog --eval '
db.products.insertMany([
  { sku: "A-1001", name: "Analytical engine card punch", price: 4200 },
  { sku: "A-1002", name: "Compiler manual, first edition", price: 175 },
  { sku: "A-1003", name: "Bombe rotor, replica",          price:  990 }
]);
db.products.countDocuments();'
```

The final expression prints the document count, which should be `3`.

## Step 3: Write the configuration

Make a working directory and change into it; every path in this track is relative to it:

```bash
mkdir sentinel-mongodb-tutorial && cd sentinel-mongodb-tutorial
```

Export the connection URI. It goes in the environment, never in the configuration file and never on
a command line:

```bash
export MONGO_URI='mongodb://127.0.0.1:27017/?directConnection=true'
```

`directConnection=true` tells the driver and the tools to talk to this one node rather than
discovering the replica set topology, which matters because the set advertises `127.0.0.1:27017`.

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
  catalog:
    type: mongodb
    uri_env: MONGO_URI
    database: catalog
    output: catalog.archive
    database_options:
      archive: true
```

Five of those keys deserve an explanation, and three of them are MongoDB-specific.

**`uri_env: MONGO_URI` names an environment variable; it is not the URI.** A MongoDB job takes no
`host`, `port`, `username`, or `password_env`: the whole connection, credentials included, is one
URI. A literal `uri:` key does exist and is accepted, but putting a URI with a password there writes
the password into your configuration file. Use `uri_env`, or
[`mongo_secrets_file`](../../guides/database-credentials.md) when the password must come from a
mounted secret. If the named variable is unset, loading fails before validation:
`environment variable 'MONGO_URI' is not set`.

**`database_options: {archive: true}` is what makes the artifact a single file.** This is the most
important line on the page, and it has no equivalent on any other engine.

:::caution Without `archive: true`, the backup is a directory and is not restorable

By default Sentinel invokes `mongodump --out=<path>`, which writes a *directory tree* of BSON files.
Three things follow, all of them bad:

- The manifest's `size_bytes` is `0`, because a directory has no size to record.
- The recorded hash is the SHA-256 of `mongodump`'s standard output, which in directory mode is
  empty. Every directory-mode MongoDB backup therefore records the same constant digest,
  `e3b0c442…b855`, which fingerprints nothing.
- `sentinel backup verify` cannot re-read a directory as a byte stream, so it reports an operational
  failure rather than `ok`, and the local storage backend's listing skips directories entirely, so a
  restore job can never find the artifact.

`archive: true` adds `--archive` to the `mongodump` invocation, which makes it write a single archive
stream to standard output instead. Sentinel hashes that stream and writes it to `output`. That
artifact has a real size, a real digest, verifies, and restores.
:::

**`output: catalog.archive` names the artifact, and that is what produces the manifest.** Without
`output`, Sentinel does not learn the artifact's name and writes no `.manifest.json` sidecar, so
`sentinel backup verify` reports `missing_manifest` and a restore has no hash to check. Unlike the
SQL engines, MongoDB jobs do not get a `.sql` extension appended, so the name you give is the name
you get.

**`history_db_path: ./history.db` keeps the execution history local.** The default is
`~/.sentinel/history.db`, shared by every configuration on the machine.

**`scheduler.lock_dir: ./locks` keeps the job locks local.** The default is `/var/run/sentinel`,
which an unprivileged user cannot create. You will meet the failure this avoids on the
[restore page](./restore.md).

Check the file before going further:

```bash
sentinel config validate --config sentinel.yaml
```

You should see:

```text
2026/08/05 20:59:05 WARN TLS not configured for database event=tls_not_configured database=catalog
configuration is valid
```

:::caution The TLS warning is honest; a `tls:` block would not be

That warning is correct here, because you are talking to a container over loopback. On a real
deployment you would reach for a `tls:` block, and Sentinel accepts and validates one on a MongoDB
job. But in v1.4.0 nothing carries it into the dump: the MongoDB dump arguments are built from the
job spec, which has no TLS field, so `mongodump` is invoked without any `--tls` flag. Adding `tls:`
silences the warning and changes nothing about the connection.

To actually get TLS on MongoDB today, put it in the URI itself, for example
`mongodb://…/?tls=true&tlsCAFile=/etc/ssl/ca.pem`, which is the connection string the tools and the
driver both honour.
:::

## Step 4: Take the backup

```bash
sentinel backup --config sentinel.yaml
```

Sentinel resolves `MONGO_URI`, pings the server with the Go driver, runs `mongodump`, hashes the
archive stream, writes it to `./backups/catalog.archive`, writes the manifest sidecar, and appends a
row to `./history.db`. On success it prints two lines built from the storage backend and the dump
adapter:

```text
Backup successfully written to /path/to/sentinel-mongodb-tutorial/backups/catalog.archive
Backup complete !
```

On a first run those are preceded by the TLS warning and followed by two `monitor schema migration`
log lines: Sentinel has just created `./history.db` and brought it up to the schema version this
binary requires. They appear only once.

## Step 5: Look at what was written

```bash
ls backups/
```

There should be exactly two entries: `catalog.archive` and `catalog.archive.manifest.json`. The
archive is `mongodump`'s own format, not BSON files you can read directly; `mongorestore` is what
consumes it.

The sidecar is what makes the artifact verifiable:

```bash
cat backups/catalog.archive.manifest.json
```

It is the same shape as on every other engine. `database_type` is `mongodb`, `hash.value` and
`hash.plaintext_value` are identical because this artifact is not encrypted, `size_bytes` is the
archive's real size, and `advanced_restore.capabilities` is the fixed pair `["full", "incremental"]`
that the backup pipeline writes for every artifact. The [PITR page](./pitr.md) explains why that
fixed pair matters more than it looks.

## Step 6: Check the execution history

Every run is recorded, whether it succeeded or failed:

```bash
sentinel monitor list --config sentinel.yaml
```

One row, with `JOB` of `catalog`, `TYPE` of `full`, an empty `CHAIN`, and `STATUS` of `success`. The
`ID` is a fresh UUID per execution; note it down, the next step uses it. `CHAIN` stays empty and
`TYPE` stays `full` until you enable incremental backup on the
[oplog page](./oplog-incremental.md).

For an aggregate view of one job:

```bash
sentinel monitor stats --job catalog --config sentinel.yaml
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

The output is a table followed by a counter line whose exact format is fixed:

```text
ID  JOB  STATUS  HASH_MATCH  TIMESTAMP
1 checked · 1 ok · 0 corrupted · 0 missing_artifact · 0 missing_manifest
```

Or verify a single backup by its execution ID:

```bash
sentinel backup verify <execution-id> --config sentinel.yaml
```

A passing single verification prints `PASS: Backup <id> integrity verified` followed by the
database, file, hash, and status.

The sweep exits non-zero when anything is wrong, so it works as a cron or CI gate. The four counters
are the four things it distinguishes: `ok`, `corrupted` (hash mismatch), `missing_artifact` (the
file is gone), and `missing_manifest` (there is nothing to compare against). Use
`--ignore-missing-manifest` to downgrade the last one to a warning when a repository contains older
artifacts written before you set `output`.

If this step reports a failure to compute a hash rather than a clean result, you almost certainly
left `archive: true` out of `database_options` and are looking at a directory. Re-read the caution
in Step 3.

## What just happened

Sentinel resolved `MONGO_URI`, opened a driver connection and pinged it, ran `mongodump` in archive
mode, hashed the archive stream on its way to `./backups/catalog.archive`, wrote the digest and
metadata into `catalog.archive.manifest.json`, and appended a row to `./history.db`. The verify step
re-read the archive from disk and recomputed the digest independently.

Nothing was compressed, encrypted, chained, or uploaded anywhere; those are configuration, not
different code. Two MongoDB-specific notes on what changes when you turn them on:

- **Compression.** `database_options: {gzip: true}` adds `--gzip` to `mongodump`, which compresses
  inside the archive. That is engine-native compression, distinct from Sentinel's own pipeline
  `compression:` block. See [Compression](../../concepts/compression.md).
- **Remote storage.** A MongoDB job pointed at S3, GCS, Google Drive, or Azure cannot stream into
  the bucket. Sentinel writes the archive into a transient staging directory first, then uploads it.
  Which directory, and who cleans it up, is documented in
  [MongoDB staging directories](../../reference/mongo-staging.md).

See [Backup](../../concepts/backup.md) for the model behind what you just ran.

## Next

- **[Restoring into a second server](./restore.md)**: prove the archive is usable, which is the only
  test of a backup that counts.

<!-- sources: internal/cli/backup.go, internal/cli/backup_verify.go, internal/cli/backup_factory.go, internal/cli/monitor.go, internal/config/types.go, internal/config/loader.go, internal/config/env.go, internal/config/marshal.go, internal/config/validator.go, internal/adapters/dump/mongo/mongo_dump.go, internal/adapters/dump/mongo/args_builder.go, internal/adapters/dump/mongo/args_factory.go, internal/adapters/db_probe/mongo.go, internal/adapters/storage/local/local.go, internal/adapters/storage/local/backend.go, internal/adapters/manifest_store/store.go, internal/domain/backup/pipeline.go, internal/utils/file.go, infra/docker/docker-compose.yml -->
