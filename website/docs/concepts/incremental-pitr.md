---
title: Incremental backup and point-in-time recovery
description: "How Sentinel builds incremental chains per engine, and the honest state of the restore half: chains build, chain restore and PITR do not run."
sidebar_position: 11
---

Incremental backup lets a job record each artifact's place in a lineage rather than treating every
run as an island. Point-in-time recovery is the intended counterpart: recover not to the moment a
backup was taken, but to an arbitrary timestamp between backups. In Sentinel v1.4.0 the two halves
are in very different states, and you need to know which is which before you build a recovery plan
on either.

:::warning Read this before planning a recovery strategy
**Incremental backup works.** Chains build, they reset at their configured depth, the lineage is
written into every manifest and every history row, and `sentinel restore validate-chain` correctly
accepts a sound chain and rejects a broken one.

**The restore half does not.** Point-in-time recovery cannot be planned for any engine, on any
artifact, at any timestamp (issue #148). Incremental restore is accepted by the planner on
PostgreSQL, then fails while staging its baseline (issue #150). On MySQL, MariaDB and MongoDB it is
never planned at all. Treat incremental backup as lineage bookkeeping that makes a *full* restore
better informed, not as a recovery mechanism you can execute today.
:::

## Why it exists

A full dump every night answers one question: what did the database look like at 02:00? It cannot
answer what it looked like at 09:47, which is the question you get asked after someone runs a
`DELETE` without a `WHERE` clause at 09:48. Closing that gap needs two things: a way to capture
change between full backups, and a way to know which artifacts belong together and in what order.

Every supported engine already produces the change data. PostgreSQL 17 summarises its write-ahead
log, MySQL and MariaDB write binary logs, MongoDB keeps an oplog. What none of them provide is
lineage: which dump is the baseline for which increments, in what order they must be applied, and
when a chain has grown long enough that walking it has become the bigger risk. That bookkeeping is
what Sentinel's `incremental_backup` block adds, and it is real value even in a release where the
execution path is not finished. Knowing that five artifacts form one chain, and which of them is the
baseline, is the difference between a considered recovery and a guess.

## How it works

### The backup side

The decision is made before the dump runs, from the job's own execution history rather than from the
database. Sentinel reads the job's recent successful executions, takes the newest one carrying a
chain ID, and resolves that chain's baseline artifact path by walking back to the row at chain index
zero.

From that state it decides. A **full** backup starting a fresh chain is produced when there is no
previous chain, when the previous chain has no baseline to build on, when the previous chain index
has reached `max_chain_depth`, or when a full run was forced. Otherwise an **incremental** backup
extends the existing chain by one index and records the baseline it depends on. The default
`max_chain_depth` is 6.

The consequence is that chain length is bounded by policy rather than by time. With
`max_chain_depth: 3` a chain is one full plus three incrementals, then the next run starts over. A
restore never has to walk an unbounded lineage, and a corrupt link can only invalidate a bounded
number of artifacts. Incremental artifacts additionally have their hash re-verified immediately
after the pipeline runs, because a bad link poisons everything after it.

That decision is written in two places: the history row (visible in `sentinel monitor list` as the
`CHAIN` column) and the artifact's manifest, under `advanced_restore.incremental_lineage`. The
manifest copy is what a restore reads.

:::note What "incremental" does not mean here
A PostgreSQL incremental backup in v1.4.0 is a full `pg_dump` with chain metadata recorded around
it. Sentinel does not invoke `pg_basebackup --incremental`; the string does not appear in the
codebase. `Size` and `Delta Size` come out equal, and that is not a display bug. You gain lineage,
not a smaller artifact. Budget storage accordingly.
:::

### The restore side

Restore planning is pure and happens before anything touches the target database. The planner reads
the staged artifact's manifest and compares the requested `restore_mode` against what the manifest
declares it supports.

For `incremental` it requires the engine to be `postgres`, an `incremental_from_backup` value that
matches the manifest's `baseline_backup_id` exactly, and an `incremental` entry in the manifest's
capability list. When those hold it resolves the ordered chain, checks contiguous indices, a
consistent baseline, a single timeline, and a verified hash for every link, then hands the staged
artifacts to `pg_combinebackup` to be combined before the restore tool runs.

For `pitr` it requires the engine to be `postgres`, a `pitr_timestamp`, a `pitr` entry in the
capability list, and both ends of a recoverable window recorded in the manifest.

That last requirement is where PITR ends.

### Point-in-time recovery cannot be planned, on any engine

The backup pipeline hard-codes the manifest's capability list to exactly `["full", "incremental"]`.
It never writes `pitr`, and nothing anywhere populates `recoverable_window_start_utc` or
`recoverable_window_end_utc`. The planner checks for the `pitr` capability before it looks at your
timestamp, so every `restore_mode: pitr` job is rejected with reason code
`missing_advanced_metadata`, regardless of engine, WAL configuration, or the time requested:

```text
Error: restore execution failed: restore planning rejected: missing_advanced_metadata
```

There is no configuration that works around this, because the rejection does not depend on any
configuration you control. The project README advertises point-in-time recovery as a headline
capability; it is not usable in this release. Tracked as issue #148.

### Incremental restore fails while staging its baseline

On PostgreSQL, planning succeeds. Execution does not. The baseline is recorded in the manifest as
the path the backup was written to, for example `backups/shop.sql`, and staging then resolves that
value again relative to the source root:

```text
Error: backup "shop.sql" not found in local source: restore source object not found: backups/shop.sql
```

The same planning that `restore validate-chain` reports as valid therefore cannot be executed.
Tracked as issue #150. To recover data from a chain today, restore the target artifact with a plain
`restore_mode: full` job. The chain metadata still earns its keep by telling you which artifacts
belong together.

### The other three engines never reach the executor

The backup side archives binary logs for MySQL and MariaDB and an oplog for MongoDB, and the restore
executor carries working replay steps for both. Neither is reachable from a configured restore job,
for two different reasons.

`restore_mode: pitr` is rejected at configuration load for any engine but PostgreSQL:

```text
Error: invalid config "sentinel.yaml": restore 'app-inc': restore_mode pitr is currently supported only for postgres
```

`restore_mode: incremental`, by contrast, is *accepted* at configuration load for all four engines,
and then rejected by the planner:

```text
Error: chain validation failed: status=rejected reason=unsupported_database_type
```

The two layers disagree: the configuration validator lists MySQL, MariaDB and MongoDB as supported
for incremental planning, while the planner accepts only PostgreSQL. `sentinel restore dry-run`
does not plan, so it prints `Restore Mode: incremental` for a MySQL job without any warning that the
mode cannot execute. The binlog and oplog replay code is dead from the configuration surface.

## Per-engine behaviour

| Engine | Change mechanism | Side artifact written at backup time | Incremental backup | Incremental restore | PITR plannable |
|---|---|---|---|---|---|
| PostgreSQL | WAL summarisation (PostgreSQL 17+, server-side `summarize_wal=on`) | None: change data stays in the WAL | Yes | No: plans, then fails staging the baseline (issue #150) | No (issue #148) |
| MySQL | Binary logs | `<artifact>.binlogs.tar` | Yes, requires `mysql.binlog_path` | No: planner rejects with `unsupported_database_type` | No: rejected at config load, and issue #148 |
| MariaDB | Binary logs | `<artifact>.binlogs.tar` | Yes, requires `mysql.binlog_path` | No: planner rejects with `unsupported_database_type` | No: rejected at config load, and issue #148 |
| MongoDB | Oplog | `<artifact>.oplog.archive` | Yes, requires a replica set | No: planner rejects with `unsupported_database_type` | No: rejected at config load, and issue #148 |

Any engine outside these four is rejected at configuration load with `incremental backup is not
supported for database type '<type>'`.

MySQL and MariaDB are deliberately identical here: the same archiver walks the binlog directory,
tars every `mysql-bin.*` or `mariadb-bin.*` file it finds, and records the first and last filenames
in the manifest. PostgreSQL is the outlier in the other direction, writing no side artifact at all.

### Prerequisites that are declared but not checked

The domain layer contains prerequisite checks for every engine. Most of them are not wired to
anything that can observe a live server, so configuration validation is weaker than it appears.

| Check | State in v1.4.0 |
|---|---|
| PostgreSQL 17+ and `summarize_wal=on` | Never called. `wal_summary_check: true` is parsed, stored, and read by nothing (issue #155). The server GUC is a genuine prerequisite; verify it yourself with `SHOW summarize_wal;`. |
| MySQL/MariaDB `log_bin` on | Called with the answer hard-coded to "on", so `log_bin_off` can never fire. |
| MySQL/MariaDB `binlog_path` readable | Genuinely checked at configuration load: it must exist, be absolute, and be a directory. |
| MongoDB replica set present | Called with the answer hard-coded to "yes", so `oplog_unavailable_standalone` can never fire. A standalone `mongod` passes validation and fails at backup time. |
| MongoDB oplog window vs `oplog_window_warn_hours` | Never called. The value is defaulted to 24 and bounds-checked, never compared to a real oplog window. |
| `binlog_check` | The key parses and is read by nothing at all. |

Treat configuration validation as a check on your YAML, not on your servers.

## Configuration

Incremental backup is enabled per backup job under `incremental_backup`.

| Key | Notes |
|---|---|
| `enabled` | Off by default. Without it every run is a standalone full backup. |
| `max_chain_depth` | Chain length before a reset. Default 6 when omitted. Must be zero or greater. |
| `wal_summary_check` | PostgreSQL. Parsed and stored; nothing probes the server (issue #155). |
| `binlog_check` | MySQL and MariaDB. Parsed and read by nothing. |
| `oplog_window_warn_hours` | MongoDB. Defaults to 24; only bounds-checked, never compared to the live window. |

MySQL and MariaDB additionally need `mysql.binlog_path` on the backup job, pointing at a directory
that is readable by the Sentinel process itself. If Sentinel runs in a container and the logs live
in the database container, that path exists for the database and not for Sentinel.

On the restore job, the advanced modes are selected by these keys.

| Key | Notes |
|---|---|
| `restore_mode` | `full` (default), `pitr`, or `incremental`. |
| `pitr_timestamp` | Required for `pitr`. RFC3339 with a timezone. PostgreSQL only. |
| `pitr_target_timeline` | Optional recovery timeline for `pitr`. |
| `incremental_from_backup` | Required for `incremental`. Must match the manifest's `baseline_backup_id` byte for byte. |
| `confirm_full_fallback` | Pre-authorises substituting a full restore from the baseline when the chain cannot be executed. Valid only with `incremental`. |
| `mysql.binlog_target_time`, `mysql.binlog_target_position` | Binlog replay stop point. Mutually exclusive. Unreachable today. |
| `mongodb.oplog_target_timestamp` | Oplog replay stop point. Unreachable today. |

`confirm_full_fallback` is a decision about acceptable data loss, not a convenience flag: a full
restore from the baseline discards everything the chain would have applied.

The complete key list is in the [configuration reference](../reference/configuration.md).

## Example

A PostgreSQL job with a deliberately short chain, so a reset is observable:

```yaml
databases:
  shop:
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: postgres
    password_env: PGPASSWORD
    database: shop
    output: shop.sql
    storage:
      type: local
      local_path: ./backups
    incremental_backup:
      enabled: true
      max_chain_depth: 3
      wal_summary_check: true
```

After five runs of `sentinel backup --config sentinel.yaml`, the history shows the chain building and
then resetting:

```bash
sentinel monitor list --config sentinel.yaml
```

```text
ID                                    JOB   TYPE         CHAIN               STATUS   TIMESTAMP            DURATION  DELTA  ERROR
128554ba-d89c-4f0e-a146-b7395138e985  shop  full         chain-1785952000#0  success  2026-08-05 17:46:40  46ms      -
2a4e1f06-4f8c-4fc8-a04b-72e1248643f6  shop  incremental  chain-1785951976#3  success  2026-08-05 17:46:40  61ms      4028
55c3fe32-4447-42f6-b254-5b694718f751  shop  incremental  chain-1785951976#2  success  2026-08-05 17:46:16  53ms      3989
7bdc33d4-4a27-41f5-ba51-fc808437f3b8  shop  incremental  chain-1785951976#1  success  2026-08-05 17:46:16  57ms      3949
f590c7fc-560e-477e-85a8-6fceb011d7a2  shop  full         chain-1785951976#0  success  2026-08-05 17:46:16  77ms      -
```

A depth of 3 yields four artifacts per chain: one full at index 0 and three incrementals. The fifth
run started `chain-1785952000` with a new full.

With a matching restore job in `incremental` mode, the lineage can be checked without executing
anything:

```bash
sentinel restore validate-chain shop-chain --config sentinel.yaml
```

```text
Incremental chain is valid for restore job "shop-chain"
  Baseline: backups/shop.sql
  Target: shop
  Depth: 2
  Artifacts: backups/shop.sql, shop
```

This is the useful end of incremental support today: the chain is real, ordered, and checkable.
Running `sentinel restore run shop-chain` against that same validated chain fails while staging the
baseline, as described above.

## Failure modes

**`restore planning rejected: missing_advanced_metadata` on a `pitr` job.** Expected, and not
fixable by configuration. No manifest Sentinel writes declares the `pitr` capability. Issue #148.

**`status=rejected reason=unsupported_database_type`.** The restore job requests `incremental` on
MySQL, MariaDB, or MongoDB. Configuration validation accepted it; the planner does not.

**`backup "<name>" not found in local source`, on an incremental restore that validated.** The
baseline path is resolved twice against the source root. Issue #150. Restore the artifact with a
`full` job instead.

**`status=rejected reason=missing_incremental_baseline`.** The artifact the job points at has no
baseline. This is the correct answer immediately after a chain reset, when the newest artifact is a
full at index 0 and there is no chain to walk. Take another backup so the newest artifact is an
incremental again.

**`incompatible_incremental_baseline`.** The `incremental_from_backup` you configured is not the
value in the manifest's `baseline_backup_id`. The comparison is exact string equality, so
`backups/shop.sql` and `./backups/shop.sql` are different values.

**`restore execution requires explicit fallback confirmation`.** The chain cannot be executed and
Sentinel refuses to silently substitute a full restore from the baseline. Decide whether the
resulting data loss is acceptable, then set `confirm_full_fallback: true`.

**A chain rule fires: `rule_4_non_contiguous_chain_index`, `rule_3_baseline_mismatch`,
`rule_5_timeline_divergence`, `rule_7_hash_verification_failed`.** The lineage is broken rather than
unsupported. A retention sweep that removed a mid-chain artifact is the usual cause; see
[Retention](./retention.md).

**`binlog_path_missing`, `binlog_path_not_found`, `binlog_path_not_directory`,
`binlog_path_unreadable`.** MySQL or MariaDB incrementals cannot read the binary log directory. These
fire at configuration load, which makes them the one prerequisite class you can rely on.

**`no_binlog_files_found in <dir>`.** The directory exists but holds nothing matching `mysql-bin.*`
or `mariadb-bin.*`. Binary logging is probably off; nothing checked, because the `log_bin` check is
hard-coded to pass.

**`mongodump oplog capture failed`.** Almost always a standalone `mongod`, which has no
`local.oplog.rs` to dump. Configuration validation does not catch this.

**`required_tool_missing: pg_combinebackup`.** The chain assembler needs the PostgreSQL 17+ client
package on the machine running the restore.

**`sentinel backup chain-status`, `chain-list`, and `force-full` cannot be invoked.** All three
require `--config`, but the flag is registered only on the parent `backup` command, so the
subcommands never receive it:

```text
$ sentinel backup chain-status --job shop --config sentinel.yaml
Error: unknown flag: --config

$ sentinel backup chain-status --job shop
Error: --config is required
```

No argument ordering satisfies both. Read chain state from `sentinel monitor list` and
`sentinel monitor show` instead, and reset a chain by lowering `max_chain_depth` rather than by
forcing a full.

## Related

- [Backup](./backup.md): the run sequence that produces a chain member.
- [Restore](./restore.md): staging, planning, and the other restore modes.
- [Manifest](./manifest.md): the lineage block itself, and the chain validation rules in full.
- [Retention](./retention.md): why deleting an artifact can break a chain, and how baselines are
  protected.
- [Incremental backup chains on PostgreSQL](../tutorials/postgres/incremental-wal.md): the example
  above, run end to end against a throwaway instance.
- [Point-in-time recovery on PostgreSQL](../tutorials/postgres/pitr.md): a configured `pitr` job and
  exactly where it stops.
- [Configuration reference](../reference/configuration.md): every key named on this page.
- [`sentinel restore` reference](../reference/cli/restore.md): `validate-chain`, `dry-run`, and
  `run`.
- [Verify backup integrity](../guides/verify-backup-integrity.md): hash verification, which chain
  validation depends on.

{/* sources: internal/domain/backup/incremental/chain.go, internal/domain/backup/incremental/planner.go, internal/domain/backup/incremental/prerequisites.go, internal/domain/backup/planner.go, internal/domain/backup/pipeline.go, internal/domain/restore/planner.go, internal/domain/restore/executor.go, internal/domain/restore/incremental/chain_resolver.go, internal/domain/restore/incremental/fallback.go, internal/domain/restore/incremental/preconditions.go, internal/domain/restore/incremental/assemble_chain.go, internal/adapters/restore/chain_assembler/adapter.go, internal/adapters/restore/incremental/pgcombine/combinebackup.go, internal/adapters/restore/incremental/mysqlbinlog/archive.go, internal/adapters/restore/incremental/mysqlbinlog/replay.go, internal/adapters/restore/runtime/executor.go, internal/adapters/dump/mongo/oplog.go, internal/config/types.go, internal/config/restore_types.go, internal/config/validator.go, internal/config/marshal.go, internal/cli/backup.go, internal/cli/restore.go, internal/ports/manifest.go */}
