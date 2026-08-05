# Runbook: `.staging/` directory for Mongo remote backups

When a MongoDB backup targets a **remote** backend — `sentinel backup --type mongodb --storage s3|gcs|gdrive` (or the equivalent config-file `storage:` block, including `azure`) — `mongodump` is invoked in archive mode against a transient file under:

```
<backup_path>/.staging/<job-id>/dump.archive[.gz]
```

The file is then streamed to the configured remote backend via
`StorageBackend.Upload`. The staging directory is removed on both success and
failure paths.

## What operators may observe

- A brief `.staging/<job-id>/` directory inside the configured local path while
  the backup is running.
- After the backup completes (or fails), `<backup_path>/.staging/` should be
  empty. A long-lived non-empty `.staging/` after a backup completed means a
  process was killed mid-run before `defer` could clean up.

## Manual cleanup

If a crash left orphans:

```sh
rm -rf "<backup_path>/.staging/<job-id>"
```

The directory name uses the pattern `mongo-<unix-nanos>-<6-hex>` when no upstream
job id was plumbed, so it is safe to remove only entries clearly belonging to a
non-running job.

## Local-backend backups

Local backups still use `mongodump --out=<dir>` and do NOT create a `.staging/`
directory. No behavioural change.

## Related code

- `internal/adapters/dump/mongo/` — archive-mode dump + staging file lifecycle
- `internal/adapters/mongo_tls/` — Mongo PEM material (`Register`/`Unregister`/`SweepOrphanMaterial`), swept on the same cleanup path
- `internal/ports/storage.go` — `StorageBackend.Upload` streaming target
