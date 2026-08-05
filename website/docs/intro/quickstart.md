---
title: Quickstart
description: Take a real backup and restore it into a second database, in about fifteen minutes, starting from nothing.
sidebar_position: 3
---

By the end of this page you will have backed up a PostgreSQL database, verified the backup's
integrity, and restored it into a second database — with everything running on your own machine and
nothing left behind.

Budget about fifteen minutes.

## What you need

- Sentinel installed — see **[Installation](./installation.md)**.
- `psql` and `pg_dump` on your `PATH`. Both ship with the PostgreSQL client package.
- Docker, to run a throwaway PostgreSQL. If you already have a database you can afford to
  experiment against, use that instead and adjust the connection details as you go.

## Step 1 — Start a throwaway database

```bash
docker run --name sentinel-quickstart \
  -e POSTGRES_PASSWORD=quickstart \
  -e POSTGRES_DB=quickstart \
  -p 5432:5432 -d postgres:17
```

Give it a few seconds, then put some data in it:

```bash
export PGPASSWORD=quickstart

psql -h 127.0.0.1 -U postgres -d quickstart -c \
  "CREATE TABLE widgets (id serial PRIMARY KEY, name text);
   INSERT INTO widgets (name) VALUES ('alpha'), ('beta'), ('gamma');"
```

You should see `INSERT 0 3`.

## Step 2 — Write a configuration file

Everything Sentinel does is driven by one YAML file. Create `sentinel.yaml`:

```yaml
version: "1.0"
log_format: text

defaults:
  storage:
    type: local
    local_path: ./backups
  retention:
    keep_last: 5

databases:
  quickstart:
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: PGPASSWORD
    database: quickstart

restores:
  quickstart-check:
    type: postgres
    enabled: true
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: PGPASSWORD
    database: quickstart_restored
    schedule: "0 3 * * 0"
    backup_source:
      type: local
      local_path: ./backups
      backup_path: "SENTINEL_*.sql"
      use_latest_match: true
    verify_after_restore: true
```

Two things in there are worth understanding now, because they are Sentinel-wide rules rather than
quickstart shortcuts:

- **`password_env: PGPASSWORD` names an environment variable — it is not the password.** Sentinel
  never accepts a password as a configuration value or a command-line argument, because both end up
  in shell history, process listings, and version control.
- **`use_latest_match: true` makes `backup_path` a pattern.** Backups are named
  `SENTINEL_<timestamp>.sql` by default, so `SENTINEL_*.sql` selects the most recent one without you
  having to know its filename.

Check the file before going further:

```bash
sentinel config validate --config sentinel.yaml
```

You should see `configuration is valid`, along with a warning that TLS is not configured. That
warning is correct and expected here — you are connecting to a local container over a loopback
address. On a real database, configure TLS.

:::note The environment variable must actually be set
Validation resolves `password_env` immediately. If `PGPASSWORD` is not exported, Sentinel fails with
`environment variable 'PGPASSWORD' is not set` rather than deferring the problem to backup time.
:::

## Step 3 — Take a backup

```bash
sentinel backup --config sentinel.yaml
```

Sentinel connects, runs `pg_dump`, writes the artifact to `./backups/`, records a SHA-256 manifest
beside it, and logs the run to its history database.

Confirm the artifact exists:

```bash
ls backups/
```

You should see a file named `SENTINEL_<timestamp>.sql`.

## Step 4 — Check it in the history

Every execution is recorded, whether it succeeded or failed:

```bash
sentinel monitor list --config sentinel.yaml
```

You should see one row for the `quickstart` job with a success status. Note its backup ID — the next
step uses it.

## Step 5 — Verify the backup's integrity

A backup you have not verified is a hope, not a backup. Sentinel hashes every artifact at write time
and can re-check it later:

```bash
sentinel backup verify --all --config sentinel.yaml
```

This re-reads each stored artifact, recomputes its SHA-256, and compares it against the manifest
recorded when the backup was taken. A mismatch means the file changed after Sentinel wrote it.

To verify one specific backup instead, pass its ID:

```bash
sentinel backup verify <backup-id> --config sentinel.yaml
```

## Step 6 — Restore into a second database

Restoring over your source database would prove nothing and destroy your data. Create a separate
empty target:

```bash
psql -h 127.0.0.1 -U postgres -d postgres -c "CREATE DATABASE quickstart_restored;"
```

The `quickstart-check` job in your configuration already points at it. Rehearse first — a dry run
resolves the source and reports what would happen, without touching the target:

```bash
sentinel restore dry-run quickstart-check --config sentinel.yaml
```

Then run it for real:

```bash
sentinel restore run quickstart-check --config sentinel.yaml
```

## Step 7 — Confirm the data came back

```bash
psql -h 127.0.0.1 -U postgres -d quickstart_restored -c "SELECT * FROM widgets;"
```

You should see the three rows — `alpha`, `beta`, `gamma` — that you inserted in Step 1.

That is the whole loop: back up, verify, restore, confirm.

## Clean up

```bash
docker rm -f sentinel-quickstart
rm -rf backups sentinel.yaml
```

## What just happened

You ran the same code path Sentinel uses in production. The difference between this and a real
deployment is configuration, not mechanism:

- **The backup ran once because you asked it to.** In production you would add a `schedule` and run
  `sentinel schedule start` to keep a cron loop going.
- **The artifact stayed on local disk.** Storage is a configuration choice — S3, Google Cloud
  Storage, Google Drive, and Azure Blob are all supported.
- **The dump was a full backup.** For large databases, incremental backup uses PostgreSQL's
  write-ahead log so daily backups do not mean daily full dumps.
- **The artifact was written in plaintext.** Encryption is opt-in and off by default.
- **You verified manually.** Integrity checks can run on a schedule instead.

## Next

- **[How Sentinel fits together](./architecture-overview.md)** — the mental model behind what you
  just ran.

<!-- sources: README.md §Quick start, internal/config/types.go, internal/config/restore_types.go, internal/cli/backup.go, internal/cli/restore.go, internal/cli/config.go, internal/utils/default.go, docs/runbooks/run-backup-from-config.md -->
