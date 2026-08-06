---
title: sentinel schedule
description: Reference for sentinel schedule and its start, stop, list, and status subcommands, with every flag and the accepted cron format.
sidebar_position: 4
---

Runs the cron loop that fires the backup and restore jobs declared in a YAML configuration, and groups the subcommands that inspect what is scheduled and how it last ran.

## Synopsis

```text
sentinel schedule [command]
```

`schedule` has no runnable form of its own; it is a container for the four subcommands below. Every subcommand except `stop` loads and fully validates a configuration file first.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `start` | Runs the cron loop in the **foreground**, firing enabled backup and restore jobs at their configured schedules. Not a daemon; see [Foreground execution](#foreground-execution). |
| `stop` | Always fails with an explanatory error. There is no running daemon to signal; see [How `stop` behaves](#how-stop-behaves). |
| `list` | Prints every scheduled job, backups, restores, and the integrity sweep, with its cron expression and next execution time, as a table or JSON. |
| `status` | Prints the schedule, next execution, last execution, last status, and last error for one **backup** job named as a positional argument. |

### Foreground execution

`sentinel schedule start` is a foreground process. It registers every enabled job that has a `schedule:`, starts the cron loop, then blocks on `SIGINT` / `SIGTERM`. **If the process stops, nothing runs.** Sentinel ships no daemon, no service manager integration, and no background mode: keeping the loop alive is the operator's responsibility, via systemd, a container restart policy, a supervisor, or an equivalent.

One `start` covers both axes. Backup jobs come from `databases:`, restore jobs from `restores:`, and; when `integrity.scheduled_check.enabled` is true and `integrity.scheduled_check.cron` is set; a reserved `__integrity_check` job is registered alongside them. Jobs with `enabled: false` or an empty `schedule:` are skipped.

Before the loop starts, `start` opens the SQLite history database and reconciles executions left in a running state by a previous shutdown, reporting the count when it is non-zero.

While the loop is running, a job whose previous run has not finished is **not** started again; that occurrence is recorded with status `skipped`. Failed backup runs are retried up to three times with 1s / 2s / 4s backoffs, except for errors that look like TLS, certificate, authentication, or configuration problems, which fail immediately.

### How `stop` behaves

`sentinel schedule stop` does not read a PID file, does not signal a process, and does not touch the history database. Its implementation is a single unconditional error:

```text
Error: schedule stop is not supported without a running daemon; use Ctrl+C on 'schedule start'
```

It exits non-zero every time, with or without a configuration file. To stop the scheduler, send `SIGINT` (`Ctrl+C`) or `SIGTERM` to the `schedule start` process. On either signal the loop stops accepting new occurrences and waits for in-flight jobs to finish before exiting, printing `scheduler stopping...` and then `scheduler stopped`.

### Cron format

Schedules are parsed as **standard five-field cron**: minute, hour, day-of-month, month, day-of-week.

| Form | Accepted | Example |
|---|---|---|
| Five fields | Yes | `0 2 * * *` |
| Step, range, and list values | Yes | `*/15 * * * *` |
| Three-letter month and day names | Yes | `0 3 * * SUN` |
| Six fields (leading seconds) | No: `expected exactly 5 fields, found 6` | `0 0 2 * * *` |
| Descriptors such as `@daily`, `@hourly`, `@every 1h` | No: `parser does not accept descriptors` | `@daily` |

Expressions are evaluated in the **local timezone of the `sentinel` process**. Setting `TZ` changes when jobs fire and changes the offset printed in `list` and `status` output. An invalid expression is rejected at configuration load, before the scheduler starts.

### Concurrency

Two independent limits bound how many jobs run at once:

| Setting | Applies to | Default |
|---|---|---|
| `max_concurrent_backups` (top level) | Backup jobs fired by the cron loop | `3` |
| `scheduler.max_concurrent_restores` | Restore jobs fired by the cron loop | `1` |

The `scheduler:` block also carries `job_timeout_minutes`, `stale_lock_threshold`, and `lock_dir`. See the [configuration reference](../configuration.md) for the full schema and defaults.

:::note Backup concurrency reads the top-level key
The cron loop is constructed from the **top-level** `max_concurrent_backups`. When `scheduler.max_concurrent_backups` is unset it inherits the top-level value, so the two agree; setting only the nested key does not change scheduler backup concurrency.
:::

## Flags

### `sentinel schedule`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `schedule`. |

### `sentinel schedule start`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | `./sentinel-config.yaml` | Path to the YAML configuration file. When omitted, `./sentinel-config.yaml` in the working directory is used; if that file is absent the command fails with a message naming the path it searched. |
| `-h`, `--help` | bool | `false` | Print help for `start`. |

### `sentinel schedule stop`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `stop`. There is no `--config` flag; the command fails before reading any configuration. |

### `sentinel schedule list`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | `./sentinel-config.yaml` | Path to the YAML configuration file. Same resolution as `start`. |
| `--format` | string | `table` | Output format: `table` or `json`. Any other value renders the table. The JSON form emits one object per job with `type`, `name`, `schedule`, `next_execution`, and `last_status`. |
| `-h`, `--help` | bool | `false` | Print help for `list`. |

### `sentinel schedule status <job-name>`

Requires exactly one positional argument. Zero or more than one argument fails with `accepts 1 arg(s)`.

| Flag | Type | Default | Description |
|---|---|---|---|
| `-c`, `--config` | string | `./sentinel-config.yaml` | Path to the YAML configuration file. Same resolution as `start`. |
| `-h`, `--help` | bool | `false` | Print help for `status`. |

:::note `status` covers backup jobs only
`status` registers only the enabled, scheduled entries from `databases:`. A restore job name or the `__integrity_check` job returns `job 'NAME' not found`, even though `list` shows them. Use `list` for restore schedules.
:::

Because `list` and `status` build a fresh in-memory scheduler from the configuration and exit, `last_status`, `Last Execution`, and `Last Error` reflect the current process only. They are empty unless the same process has already executed the job. For durable execution history across runs, query the history database with `sentinel monitor`.

## Examples

Run the scheduler in the foreground:

```bash
sentinel schedule start --config sentinel.yaml
```

Expected output:

```text
Scheduler started with 2 backup job(s) and 1 restore job(s)
```

Run it under systemd so it survives a reboot, keeping the database password in an environment file rather than on the command line:

```ini
[Service]
ExecStart=/usr/local/bin/sentinel schedule start --config /etc/sentinel/sentinel.yaml
EnvironmentFile=/etc/sentinel/sentinel.env
Restart=always
```

List everything that is scheduled:

```bash
sentinel schedule list --config sentinel.yaml
```

```text
TYPE     NAME            SCHEDULE   NEXT EXECUTION
backup   prod-postgres   0 2 * * *  2026-08-06T02:00:00Z
restore  nightly-verify  0 4 * * *  2026-08-06T04:00:00Z
```

Machine-readable form, for a monitoring check:

```bash
sentinel schedule list --config sentinel.yaml --format json
```

Inspect one backup job:

```bash
sentinel schedule status prod-postgres --config sentinel.yaml
```

```text
Job: prod-postgres
Schedule: 0 2 * * *
Next Execution: 2026-08-06T02:00:00Z
Last Execution: 0001-01-01T00:00:00Z
Last Status:
```

Confirm a cron expression before deploying it; an invalid schedule fails at load:

```bash
sentinel schedule list --config sentinel.yaml
```

```text
Error: invalid config "sentinel.yaml": backup 'prod-postgres': invalid cron expression '@daily': parser does not accept descriptors: @daily
```

## Related

- [Scheduling concepts](../../concepts/schedule.md): why the scheduler exists and how job execution is bounded.
- [Configuration reference](../configuration.md): `schedule:`, `max_concurrent_backups`, and the `scheduler:` block.
- [`sentinel backup`](./backup.md): the command the scheduler invokes for `databases:` jobs.
- [`sentinel restore`](./restore.md): the command the scheduler invokes for `restores:` jobs.
- [CLI reference index](./index.md): every Sentinel command.

{/* sources: internal/cli/schedule.go, internal/scheduler/scheduler.go, internal/scheduler/executor.go, internal/domain/schedule/types.go, internal/domain/schedule/next_run.go, internal/config/types.go, internal/config/loader.go, internal/cli/config_resolver.go */}
