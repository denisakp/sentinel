# Monitor Schema Migration

How Sentinel manages the schema of its monitor (history) SQLite database, and how operators inspect or repair it.

## What `schema_version` is

The monitor database carries a singleton table `schema_version (id=1, version, updated_at)`. The integer `version` records the migration level the DB is currently at. The running binary declares `BinarySchemaVersion`; the two are compared on every monitor open.

`schema_migrations` (existing table) is the per-file ledger — one row per applied migration file. The two stay in sync because the migration runner advances them in the same transaction.

## When migrations run

Every call to `monitor.NewMonitor(path)` invokes the migration runner before any read or write:

1. Ensures `schema_version` exists.
2. Reads the current version.
3. Forward-incompatible (`current > BinarySchemaVersion`) → returns `monitor.ErrForwardIncompatible`. No writes.
4. Current (`current == BinarySchemaVersion`) → no-op.
5. Stale (`current < BinarySchemaVersion`) → acquires `<db>.migrate.lock` via `internal/adapters/lock`, re-checks under the lock, applies each pending migration in its own transaction, stamps `schema_version`.

Backup, restore, schedule, retention, and monitor commands all hit this path on startup.

## `sentinel monitor doctor`

Diagnostic command. Default output is a human-readable table; `--json` emits a stable wire shape (see `specs/017-monitor-schema-migration/contracts/monitor-doctor.json.schema.json`).

```bash
sentinel monitor doctor --config sentinel.yaml
sentinel monitor doctor --json --config sentinel.yaml
sentinel monitor doctor --repair --config sentinel.yaml
```

`--repair` re-runs pending migrations under the same file lock. It refuses on forward-incompatible / missing / corrupt databases (exit codes 2 / 3 / 4 respectively).

## Exit codes

| Code | Meaning                                                              |
|------|----------------------------------------------------------------------|
| 0    | Schema current, or repair succeeded.                                 |
| 1    | Schema stale; pending migrations exist (inspect mode).               |
| 2    | DB schema is ahead of the binary's required version.                 |
| 3    | Monitor database file does not exist at the configured path.         |
| 4    | DB exists but is corrupt or otherwise unreadable.                    |

These are stable across releases.

## Recovery from forward-incompat

The DB was written by a newer Sentinel build than the one you are running. Two options:

1. **Upgrade the binary** to a release that ships the required schema version.
2. **Restore a prior monitor database** (the operator-owned `history_db_path`) from backup, then re-run.

Never delete the monitor DB to "fix" forward-incompat — that loses history. If you must reset, archive the file first.

## Recovery from corrupt

`status: corrupt` means SQLite cannot open or query the file. Restore the file from the most recent snapshot, or remove it (after archival) to let `NewMonitor` recreate a fresh DB on the next backup run.

## See also

- Source PRD: `.prds/11-monitor-schema-fallback.md`
- Spec: `specs/017-monitor-schema-migration/`
- CLI contract: `specs/017-monitor-schema-migration/contracts/monitor-doctor-cli.md`
- JSON schema: `specs/017-monitor-schema-migration/contracts/monitor-doctor.json.schema.json`
