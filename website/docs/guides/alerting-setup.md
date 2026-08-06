---
title: Setting up notifications
description: "Wire Sentinel to Slack, Discord, email, or a generic webhook, and prove an alert reaches the channel."
sidebar_position: 6
---

Configure notification channels so that a failed backup reaches a human instead of sitting in a log
file nobody reads.

## When to use this

Use this when you are enabling notifications for the first time, or adding a channel to a
configuration that already has one. It covers the four channel types Sentinel supports: Slack,
Discord, email over SMTP, and a generic JSON webhook.

Do not use it as a monitoring strategy on its own. Sentinel notifies about runs it actually
performed; a scheduler that is not running produces silence, and silence is indistinguishable from
success. Pair channels with an external check that the scheduler process is alive, and with
[Inspecting monitor history](./inspect-monitor-history.md) for the runs themselves.

## Before you start

- The channel's endpoint, held in an environment variable rather than in the configuration file. A
  webhook URL is a credential: anyone holding it can post to your channel.
- Network egress from the Sentinel host to the endpoint. SMTP in particular is often blocked
  outbound.
- Permission to edit the configuration and restart whatever runs Sentinel.

One design decision shapes everything below. A channel subscribes to *event categories*, not to
jobs, and the categories are fixed: `success`, `failure`, and `warning`. At least one must be
listed. An omitted or empty `events:` list is a configuration error, not a default:

```
notification events must include at least one of: success, failure, warning
```

## Steps

### 1. Choose where the block lives

`notifications:` is a list, and it can sit under `defaults:` or on an individual job. Inheritance
replaces the **whole list**: a job that declares any `notifications:` entry of its own does not
receive `defaults.notifications` in addition, it receives only its own. Restate every channel you
want on that job.

Restore jobs under `restores:` carry their own `notifications:` list and never inherit from
`defaults:`.

### 2. Add a webhook-shaped channel

Slack, Discord, and the generic webhook share one shape. Only `type` and the variable name change.

```yaml
defaults:
  notifications:
    - type: slack
      webhook_url_env: SENTINEL_SLACK_WEBHOOK_URL
      events: [failure]
      timeout_seconds: 10

    - type: discord
      webhook_url_env: SENTINEL_DISCORD_WEBHOOK_URL
      events: [failure]

    - type: webhook
      webhook_url_env: SENTINEL_OPS_WEBHOOK_URL
      events: [success, failure, warning]
```

`webhook_url_env` is required and names an environment variable; there is no key that accepts a URL
inline. The name must match `^[A-Z_][A-Z0-9_]*$`. `timeout_seconds` is the HTTP timeout, defaults to
10, and must fall between 1 and 60.

Export the values from your secret store, never in a file that gets committed:

```bash
export SENTINEL_SLACK_WEBHOOK_URL="$(vault kv get -field=url secret/sentinel/slack)"
```

Slack and Discord receive a formatted, colour-coded message. The generic webhook receives a JSON
POST with `Content-Type: application/json`, carrying `backup_name`, `database`, `database_type`,
`status`, `event`, `timestamp`, `duration_ms`, `file_path`, `file_size`, `error`, and a rendered
`message` string.

### 3. Or add an email channel

```yaml
defaults:
  notifications:
    - type: email
      smtp_host: smtp.example.com
      smtp_port: 587
      smtp_username_env: SENTINEL_SMTP_USER
      smtp_password_env: SENTINEL_SMTP_PASSWORD
      from_address_env: SENTINEL_ALERT_FROM
      to_addresses:
        - sre@example.com
        - dba@example.com
      use_tls: true
      events: [failure]
```

The required keys are `smtp_host`, `smtp_password_env`, `from_address_env`, and a non-empty
`to_addresses`. Note that the sender is supplied through an environment variable and there is no
plain `from:` key; `to_addresses` is a list and there is no plain `to:` key.

`smtp_port` defaults to 587 and must be between 1 and 65535. `smtp_username_env` is optional, and
omitting it sends unauthenticated. `use_tls` defaults to true and upgrades the connection with
STARTTLS after connecting, so it is a plain SMTP connect followed by an upgrade rather than implicit
TLS on port 465.

### 4. Understand which events actually fire

The category is derived from the run's outcome, and the mapping is narrower than the three names
suggest.

| What happened | Category dispatched |
|---|---|
| Backup run succeeds | `success` |
| Backup run fails at any stage, including the connectivity check | `failure` |
| Backup run skipped because another process holds the job lock | none, nothing is dispatched |
| `sentinel restore run` reports `success` or `completed` | `success` |
| `sentinel restore run` reports `failed`, `failure`, `timeout`, or `interrupted` | `failure` |
| `sentinel restore run` reports `skipped`, or any unrecognised status | `warning` |
| Scheduled restore succeeds or fails | `success` or `failure` |
| Scheduled integrity sweep passes or finds a problem | `success` or `failure` |

:::caution Backups never emit `warning`
A backup run resolves to exactly `success` or `failure`. Subscribing a channel to
`events: [warning]` and expecting to hear about degraded backups produces a channel that is silent
forever. `warning` reaches you only from `sentinel restore run`, and in practice only for a skipped
restore. For on-call, `events: [failure]` is the useful subscription; add `warning` alongside it
only if you also run restore jobs.
:::

A backup skipped by the lock is worth calling out separately, because it is the case where you most
want to hear something and hear nothing. The run returns early, before the notification step, so no
channel is told. Detect it through [Inspecting monitor history](./inspect-monitor-history.md)
instead.

### 5. Force a failure and watch it arrive

Point a throwaway job at a closed local port. Connection refused is instant, whereas an unroutable
address makes you wait out a TCP timeout of over a minute.

```yaml
version: "1.0"
history_db_path: ./alert-probe.db

defaults:
  storage:
    type: local
    local_path: ./backups
  notifications:
    - type: webhook
      webhook_url_env: SENTINEL_OPS_WEBHOOK_URL
      events: [success, failure, warning]

databases:
  alert-probe:
    type: postgres
    host: 127.0.0.1
    port: 1
    username: nobody
    password_env: SENTINEL_PROBE_PASSWORD
    database: nothing
    output: alert-probe.sql
```

```bash
export SENTINEL_PROBE_PASSWORD=not-a-real-password
sentinel backup --config alert-probe.yaml
```

The command exits non-zero, and the channel receives a `failure`. For the generic webhook the body
looks like this:

```json
{
  "backup_name": "alert-probe",
  "database": "nothing",
  "database_type": "postgres",
  "status": "failure",
  "event": "failure",
  "duration_ms": 0,
  "file_path": "backups/alert-probe.sql",
  "file_size": 0,
  "error": "failed to ping database - failed to ping database: dial tcp 127.0.0.1:1: connect: connection refused"
}
```

`file_path` names where the artifact would have gone. Nothing was written. Delete the probe job once
the alert has landed.

## Verify

Confirm the configuration resolves every channel's environment variable. This is a real check, not a
formality: an unset variable fails the load outright, so a passing validation means the names are
right and exported.

```bash
sentinel config validate --config sentinel.yaml
```

```
configuration is valid
```

Then confirm the failure was recorded as well as announced, so you can tell an alerting problem from
a backup problem:

```bash
sentinel monitor list --config sentinel.yaml --status failure --last 1h
```

One `failure` row per failed run, and one message per subscribed channel per run. Channels are
dispatched concurrently and independently: a broken Slack webhook does not stop the email going out.

## If it goes wrong

**`environment variable 'SENTINEL_SLACK_WEBHOOK_URL' is not set`.** Configuration loading resolves
notification variables eagerly, so this stops the whole run before any backup starts. Check the
environment the scheduler runs in, which under systemd or cron is not your login shell.

**`notification events must include at least one of: success, failure, warning`.** The channel has
no `events:` key, or an empty list. There is no implicit default.

**`unsupported notification type '<x>'`.** Only `slack`, `discord`, `webhook`, and `email` exist.
Note that a `defaults.notifications` block is validated only through the jobs that inherit it; if
every job declares its own list, an invalid channel sitting in `defaults:` is never reached and
never reported.

**The backup succeeded but nothing arrived.** Check `events:` first, since `[failure]` correctly
stays silent on success. Then check `enabled:`, which defaults to true but can be set false per
channel. Delivery failures are logged as warnings and never turn a successful backup into a failed
one, so an unreachable endpoint is invisible in the exit code.

**Email connects and then fails to upgrade.** Sentinel dials plain SMTP and issues STARTTLS. A
server expecting implicit TLS on port 465 will not complete that handshake. Use the submission port
your provider documents for STARTTLS, usually 587.

## Related

- [Backup](../concepts/backup.md): the run stages that decide whether a notification says success or
  failure.
- [Restore](../concepts/restore.md): where the `warning` category comes from.
- [Running a backup from a configuration file](./run-backup-from-config.md): the command used to
  trigger the test alert.
- [Inspecting monitor history](./inspect-monitor-history.md): the record of runs, including the
  skipped ones no channel hears about.
- [Environment setup](./environment-setup.md): exporting variables into the environment the scheduler
  actually sees.
- [Configuration reference](../reference/configuration.md): every key in the `notifications:` block.
- [`sentinel monitor` reference](../reference/cli/monitor.md): filters for confirming what ran.

<!-- sources: internal/config/types.go, internal/config/validator.go, internal/config/loader.go, internal/config/env.go, internal/adapters/notifier/converter.go, internal/adapters/notifier/dispatcher.go, internal/adapters/notifier/types.go, internal/adapters/notifier/webhook.go, internal/adapters/notifier/email.go, internal/adapters/notifier/slack.go, internal/adapters/notifier/discord.go, internal/domain/backup/executor.go, internal/cli/restore.go, internal/cli/integrity_scheduled.go, internal/scheduler/restore_integration.go, docs/runbooks/alerting-setup.md -->
