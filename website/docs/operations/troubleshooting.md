---
title: Troubleshooting common errors
description: "An index of the exact error strings Sentinel prints, what each one means, and the shortest fix: paste your error and scroll to it."
sidebar_position: 9
---

The errors you are most likely to hit, indexed by the exact text Sentinel prints.

## Symptoms

You have an error string in front of you and want the fix, not a diagnosis. Search this page for the leading fragment of your error. If nothing matches, or the failure is a job-level `failure` row rather than a message on your terminal, start at [Triaging a failed backup](./failed-backup-triage.md).

## Before you start

Have the exact text. Sentinel truncates the `ERROR` column in `sentinel monitor list`, so pull the full message first:

```bash
sentinel monitor show --config sentinel.yaml --id <execution-id> 2>&1 | tee error.txt
```

Nothing on this page changes data, with the single exception of `sentinel security init-key --force`, which is flagged where it appears.

:::caution Output redirection does not work on the monitor commands
`monitor list`, `show`, `stats` and `export` write to stderr, so `… > error.txt` yields an empty file. Use `2>&1 | tee error.txt`, or `sentinel monitor export --output error.json`, which writes the file itself and does work. Tracked as issue #165.
:::

## Resolution

### `Error: unknown flag: --config`

On `sentinel backup chain-status`, `backup chain-list`, `backup force-full`, or `schedule stop`.

These subcommands read `--config` but never register it, and the parent's flag is local rather than persistent, so it is not inherited. Dropping the flag does not help either:

```text
$ sentinel backup chain-status --job shop --config sentinel.yaml
Error: unknown flag: --config

$ sentinel backup chain-status --job shop
Error: --config is required
```

No argument ordering satisfies both. There is no workaround; the commands cannot run. Tracked as issue #136.

Use `sentinel monitor list --config sentinel.yaml --job <name>` instead of `chain-status` and `chain-list`; its `CHAIN` column shows `<chain-id>#<index>` for every run. To force a fresh chain in place of `force-full`, see [Recovering a broken incremental chain](./chain-corruption-recovery.md).

`schedule stop` has its own message, below.

### `Error: --config is required`

Same three `backup` subcommands, invoked without the flag. See above. Every other command that needs a configuration file accepts `--config` normally.

### `Error: failed to load config "<path>": failed to read config file: open <path>: no such file or directory`

The path is wrong or relative to a different working directory. Pass an absolute path.

### `Error: failed to load config "<path>": backup 'shop': environment variable 'PGPASSWORD' is not set`

Raised at configuration load, before any job runs, so it blocks every command that reads the config, including `monitor list`. The variable named in `password_env` (or `host_env`, `username_env`, `uri_env`) is not set in the environment Sentinel is running in.

Under a scheduler or a container this is usually an environment that is present in your shell and absent in the service unit. Confirm with the same environment the process gets, not your interactive one. See [Database credentials](../guides/database-credentials.md).

### `Error: failed to ping database - failed to ping database: dial tcp 127.0.0.1:5432: connect: connection refused`

The doubled phrase is expected; the connectivity gate wraps its own error. The host and port in the message are what Sentinel actually dialled, which is the useful part.

1. From inside a container, `localhost` is the container. Use `host.docker.internal` or the compose service name.
2. Confirm the database is up: `docker ps`.
3. Test the socket independently: `nc -zv host.docker.internal 5432`.

### `failed to execute pg_dump command - exec: "pg_dump": executable file not found in $PATH`

The engine client binary is not installed on the host running Sentinel. Sentinel shells out to `pg_dump`, `pg_dumpall`, `mysqldump`, `mariadb-dump`, and `mongodump`, and needs them on `PATH`. Install per [Environment setup](../guides/environment-setup.md).

:::note MariaDB spells its own error two ways
Single-database MariaDB dumps report `failed to execute maridb-dump command`, with the typo, while the all-databases path reports `failed to execute mariadb-dump command`. The binary invoked is `mariadb-dump` in both cases. Search for both spellings.
:::

### `failed to execute pg_dump command - exit status 1, <stderr>`

The tool ran and refused. Its stderr is already appended to the message, with credentials redacted, so there is nothing further to enable to see it. Read the tail of the message; it is usually a permission or an object-name problem in `additional_args`. See [Additional arguments](../reference/additional-args.md).

:::caution `log_format` does not change this
`log_format: text` is a real top-level key, but it does not affect logging. It supplies the default `--output` format for `sentinel backup verify`, `backup diff`, `storage status`, and `security reencrypt`. Setting it will not surface any extra dump output.
:::

### `Error: schedule stop is not supported without a running daemon; use Ctrl+C on 'schedule start'`

`schedule stop` only works inside the same process session as `schedule start`. For a background scheduler use a process manager (systemd, supervisor) and stop the unit. See [Recovering from a scheduler crash](./scheduler-crash-recovery.md).

`schedule stop` also never registers `--config`, so passing it returns `unknown flag: --config`. The command needs no configuration.

### `Error: accepts 1 arg(s), received 0` on `schedule status`

`schedule status` takes a job name as a positional argument: `sentinel schedule status <job-name> --config sentinel.yaml`. To see every scheduled job, use `sentinel schedule list --config sentinel.yaml`.

### `Error: --job is required` on `sentinel monitor stats`

`monitor stats` requires `--job`, despite the command's own help text showing an example without it. Pass a job name:

```bash
sentinel monitor stats --config sentinel.yaml --job prod-postgres
```

`--last` on this command resolves to whole days, so `--last 1h` rounds to zero days and returns nothing. Use `--last 1d` or longer.

### `no matching records`

The query ran and the history is empty for that filter. Widen `--last` first; the default window is 7 days.

If it is empty for every window, check two things. `history_db_path` must be set in the configuration and writable. And history is only written for config-driven runs: a flag-only `sentinel backup --type postgres …` invocation writes no history row, no manifest, and no encryption. See [Inspect monitor history](../guides/inspect-monitor-history.md).

### `2026/08/05 21:44:36 WARN TLS not configured for database event=tls_not_configured database=shop`

Informational, on stderr, emitted at config load by any command that reads the configuration. The job
will connect in the clear: either it has no `tls:` block, or it has one with `enabled: false`. It does
not fail anything. Set `tls.enabled: true` on the job to silence it.

On v1.4.0 and earlier the warning depended on the block being **present**, so `tls: {enabled: false}`
silenced it while the connection stayed in plaintext, and so did a fully configured block, because the
block never reached any engine. See [#189](https://github.com/denisakp/sentinel/issues/189).

### `Warning: SENTINEL_MASTER_KEY is already configured.`

```text
Warning: SENTINEL_MASTER_KEY is already configured.
  Use --force to generate a new key (this will invalidate existing encrypted backups).
```

This is a warning on stderr and the command **exits 0**. No key was generated and nothing changed.

:::danger `--force` does not migrate anything
`sentinel security init-key --force` prints a new key. Every artifact encrypted with the old key stays encrypted with the old key, and becomes unreadable the moment you stop being able to produce it. Audit which jobs and schedules depend on the current key before forcing, and re-encrypt deliberately rather than by replacement. See [Key rotation](../guides/key-rotation.md) and [Key loss incident](./key-loss-incident.md).
:::

### `file corrupt or wrong key`

One of three things: the key is wrong, the artifact uses the pre-v2 envelope, or the file is truncated or tampered with. Distinguish them by checking the envelope version first, per [Recovering a pre-v2 envelope](./recover-legacy-envelope.md), then the bytes, per [Verify backup integrity](../guides/verify-backup-integrity.md).

`--allow-legacy-envelope` is the flag for the second case on the restore path. On `backup verify` it is accepted and has no effect, because verification hashes stored bytes and never decrypts.

### `lock: held by pid=4821 host="db01" age=3m12s (live=true)`, or `restore execution lock conflict`

Another holder has the job lock. The restore history records the execution as `skipped` with reason `lock_conflict`.

If the named PID is alive, wait. If it is not, the lock is stale: see [Recovering from a stale lock](./stale-lock-recovery.md), and `sentinel repair --config sentinel.yaml --dry-run` will report it as a `stale_lock` finding.

Locks apply to restore jobs. Backup jobs take no lock on any current code path, so a backup is never blocked or skipped this way.

### `Error: backup "deadbeef": backup not found` (exit 2)

The ID is not in the history database. `sentinel backup verify` takes an **execution ID**, which `sentinel monitor list` prints in its `ID` column, not a job name and not the `backup_id` field inside a manifest (which holds the job name).

### `verification skipped: no manifest` (exit 3), or `missing_manifest` in a `--all` sweep

The artifact exists and has no `.manifest.json` sidecar, so nothing can be checked. Almost always this means the job omits `output:`: the dump engine invents a filename at dump time that never reaches the manifest writer, so no manifest is created and the history row records the artifact as `unknown`. Scheduled runs are unaffected, because the scheduler fills in a timestamped name first. Tracked as issue #151.

Set `output:` on the job and re-run. Existing artifacts cannot be retrofitted with a manifest.

### `Error: restore job "nope" not found`

The name must match a key under `restores:` in the configuration passed to `--config`. `sentinel restore list --config sentinel.yaml` prints the valid names. Note that `sentinel restore` uses its own `--config` flag, described as "Path to restore config file"; point it at the same file.

### `Error: restore execution failed: restore planning rejected: missing_advanced_metadata`

The restore job requests `restore_mode: pitr`. No manifest Sentinel writes declares the `pitr` capability, and nothing populates the recoverable window, so the planner rejects every point-in-time request regardless of engine, WAL configuration, or timestamp. No configuration works around it. Tracked as issue #148, and explained in [Incremental backup and point-in-time recovery](../concepts/incremental-pitr.md).

### `Error: invalid config "sentinel.yaml": restore 'app-inc': restore_mode pitr is currently supported only for postgres`

Raised at configuration load for MySQL, MariaDB, and MongoDB. It is the earlier of the two rejections above; PostgreSQL jobs get past this one and are then rejected by the planner.

### `Error: chain validation failed: status=rejected reason=unsupported_database_type`

The restore job requests `restore_mode: incremental` on MySQL, MariaDB, or MongoDB. Configuration validation accepts those engines; the planner accepts only PostgreSQL. The two layers disagree, and the planner wins.

### `Error: verification handler is required for restore mode "incremental"`

An incremental restore that planned, staged its whole chain, and ran the engine restore, then stopped at the post-restore verification gate. Incremental mode requires a verification handler and no call site supplies one. Tracked as issue #149.

**The target database has already been written to when this is reported.** The command exits non-zero, but the restore itself happened. Check the target before retrying, and do not assume the failure means nothing changed.

This message replaced `Error: backup "shop.sql" not found in local source: restore source object not found: backups/shop.sql`, which was the same job failing earlier, during staging, because the baseline was recorded from the backup's root and resolved against the restore source's root. That was issue #150 and it is fixed.

To recover data from a chain predictably today, point a restore job at the target artifact with `restore_mode: full`.

### `required_tool_missing: pg_combinebackup`, or `required_tool_missing: mysqlbinlog`

The incremental replay path needs an external binary that is not on `PATH` on the machine running the restore. `pg_combinebackup` ships with the PostgreSQL 17+ client package; `mysqlbinlog` ships with the MySQL or MariaDB client package.

### `No drift detected.` from `sentinel repair --job <name>`, on a job you know is broken

`repair` does not validate the `--job` value. An unknown or misspelled name reports `No drift detected.` and exits `0`, indistinguishable from a healthy job. Check the spelling against `sentinel monitor list`. Tracked as issue #170.

### `Restore Mode: incremental` from `sentinel restore dry-run`, on a job that cannot run

`restore dry-run` prints the job's configured fields and does not invoke the planner, so it reports success for configurations the planner will reject. It is not a pre-flight check. Use `sentinel restore validate-chain <job>` for incremental jobs. Tracked as issue #152.

### A backup file is created but is 0 bytes

The dump tool exited without writing anything. For PostgreSQL plain format (`pg_out_format: p`), even an empty database produces a non-zero header, so a genuinely empty file means the tool failed. The tool's stderr is already in the error message on the failing run; re-read that rather than the file.

### `mysqldump: [Warning] Using a password on the command line interface can be insecure`

Sentinel does not cause this. MySQL and MariaDB passwords are passed through the `MYSQL_PWD` environment variable, never on the command line. If you see this warning, a `--password=` is coming from your own `additional_args`, and it is exposing the credential in `ps` and in `/proc/<pid>/cmdline`.

Remove it and use `password_env`, `password_file`, or `defaults_file` instead. See [Database credentials](../guides/database-credentials.md).

## Verify recovery

Re-run the command that failed, then confirm the history agrees:

```bash
sentinel monitor list --config sentinel.yaml --last 15m 2>&1 | tee after.txt
```

A `success` row with a non-zero `DURATION`. For anything that touched an artifact, follow with an integrity check, because a run that succeeds is not yet a backup you have proven:

```bash
sentinel backup verify <execution-id> --config sentinel.yaml
echo "exit=$?"
```

Exit `0` is a hash match. Exit `3` is `missing_manifest`. Exit `5` on a `--all` sweep means at least one artifact is a genuine integrity problem.

## Prevent recurrence

- **Set `output:` on every backup job.** It is the single configuration mistake behind the largest share of the entries above.
- **Resolve credentials from the environment the process actually gets**, not your shell. Most `environment variable … is not set` failures are a service-unit environment, not a typo.
- **Pin the client binaries.** Install the engine client packages as part of the host image, and check them after an upgrade with [Upgrade the Sentinel binary](../guides/upgrade-sentinel-binary.md).
- **Do not plan around point-in-time recovery or incremental restore** in this release. Both are documented honestly in [Incremental backup and point-in-time recovery](../concepts/incremental-pitr.md).
- **Sweep on a schedule**, so integrity problems surface before a recovery does: `sentinel backup verify --all --since 30d --config sentinel.yaml` and `sentinel repair --config sentinel.yaml --dry-run`.

## Related

- [Triaging a failed backup](./failed-backup-triage.md): the systematic route when the error is not on this page.
- [Recovering a broken incremental chain](./chain-corruption-recovery.md): the chain and lineage errors above, in full.
- [Recovering from a stale lock](./stale-lock-recovery.md) and [Recovering from a scheduler crash](./scheduler-crash-recovery.md).
- [Repairing repository state drift](./state-repair.md): what `sentinel repair` detects and what it will not fix.
- [Recovering a pre-v2 envelope](./recover-legacy-envelope.md) and [Key loss incident](./key-loss-incident.md).
- [Environment setup](../guides/environment-setup.md) and [Database credentials](../guides/database-credentials.md).
- [Inspect monitor history](../guides/inspect-monitor-history.md) and [Verify backup integrity](../guides/verify-backup-integrity.md).
- [Configuration reference](../reference/configuration.md) and the [glossary](../reference/glossary.md).

{/* sources: internal/cli/backup.go, internal/cli/monitor.go, internal/cli/schedule.go, internal/cli/restore.go, internal/cli/repair.go, internal/cli/security.go, internal/cli/crypto_errors.go, internal/cli/backup_verify.go, internal/cli/exit_codes.go, internal/config/loader.go, internal/config/validator.go, internal/config/types.go, internal/domain/restore/executor.go, internal/domain/restore/planner.go, internal/domain/restore/job.go, internal/ports/lock.go, internal/ports/recorder.go, internal/adapters/db_probe/ping.go, internal/adapters/dump/pg/pg_dump.go, internal/adapters/dump/mariadb/mariadb_dump.go, internal/adapters/dump/mysql/mysql_dump.go, internal/adapters/restore/incremental/pgcombine/combinebackup.go, internal/adapters/restore/incremental/mysqlbinlog/replay.go, internal/adapters/mysqlargs/core.go, docs/runbooks/troubleshooting.md */}
