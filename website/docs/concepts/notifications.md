---
title: Notifications
description: "How Sentinel tells you a run finished: the four channels, the event vocabulary, and which outcomes actually fire."
sidebar_position: 14
---

A job can declare notification channels, and when the job finishes Sentinel sends each of them a message
describing what happened. Four channel types exist: Slack, Discord, a generic JSON webhook, and SMTP
email. Channels are declared per job, filtered by event category, delivered concurrently with a deadline,
and are deliberately unable to fail the run they are reporting on.

## Why it exists

The failure mode that matters for a backup tool is not a loud one. It is a job that quietly stops
running, or starts failing every night, while the artifacts from last month sit in the bucket looking
reassuring. The execution history records that, but only for someone who goes and looks.

Notifications invert that: the tool reaches out instead of waiting to be queried. The design choice
worth noting is what happens when a channel is unreachable. A Slack outage must not turn a successful
backup into a failed one, so delivery errors are collected, logged, and reported alongside the run
result, but never propagated into the run's exit status. The backup succeeded; the message about it did
not arrive.

## How it works

When a job has a `notifications:` list, Sentinel builds a dispatcher for it before the run starts.
Building the dispatcher resolves each channel's secrets from the environment: the webhook URL for
Slack, Discord and webhook channels, and the SMTP password and sender address for email. There are no
inline credential keys; every secret is named by an environment variable whose name must match
`^[A-Z_][A-Z0-9_]*$` and must actually be set. A channel that cannot be constructed is reported as a
warning on stderr and the job runs with no notifications at all.

At the end of the run the dispatcher fans out. Every channel is invoked concurrently in its own
goroutine under a shared 30 second deadline, on top of each HTTP channel's own client timeout
(`timeout_seconds`, default 10). Failures from individual channels are gathered into one aggregate
error, logged as a warning, and attached to the run result, where the CLI prints them as
`notification error: ...`. The run's own outcome is untouched.

### Which outcomes fire

Every message carries one of three event categories, and a channel receives it only if that exact
category appears in the channel's `events` list. There is no wildcard and no implicit default; the
`events` key is required and must name at least one category.

For a **backup**, the mapping is binary. The run either completed without an error, which sends
`success`, or it did not, which sends `failure`. Backups never emit `warning`. A run that produced a
manifest warning, or that skipped a database during auto-discovery, still reports `success`, because
the run itself did not error.

For a **restore**, all three are reachable. A completed restore sends `success`; a failed, timed out,
or interrupted restore sends `failure`; a restore that was skipped, or whose result could not be
determined, sends `warning`. This is the only path on which a `warning` subscription receives anything.

Notification is the last step of the backup run, after the artifact has been written and the history row
recorded. It follows the outcome; it does not gate it.

### What each channel sends

| Channel | Transport | Message shape |
|---|---|---|
| `slack` | `POST` to an incoming webhook URL | One attachment with a status colour, title, text body, and short fields for database, type, duration and size, plus an error field when present |
| `discord` | `POST` to a webhook URL | An embed with the same fields plus an explicit status field, using Discord's integer colour codes |
| `webhook` | `POST` of a flat JSON object | Keys including `event`, `status`, `timestamp`, `backup_name`, `database`, `database_type`, `duration_ms`, `file_size`, `file_path`, `error`, `message`. Restores send `restore_name`, `bytes_restored`, `source_backup`, and `verification_passed` instead |
| `email` | SMTP, with STARTTLS by default | A plain-text report with a `Status`, `Timestamp`, details block, and an error section when present |

The status colour is always paired with the status word in the title and body, so a message remains
readable where colour is not rendered.

## Configuration

Channels live under a job's `notifications:` key, or under `defaults.notifications` to apply to every
backup job that does not declare its own list. Restore jobs carry the same structure under
`restores.<name>.notifications`.

```yaml
version: "1.0"
history_db_path: ~/.sentinel/history.db

databases:
  prod-postgres:
    type: postgres
    host_env: PROD_DB_HOST
    port: 5432
    username_env: PROD_DB_USER
    password_env: PG_PASSWORD
    database: myapp_prod
    schedule: "0 2 * * *"
    storage:
      type: local
      local_path: ./backups
    notifications:
      - type: slack
        webhook_url_env: SLACK_WEBHOOK_URL
        events: [failure]
        timeout_seconds: 10

      - type: webhook
        webhook_url_env: SENTINEL_WEBHOOK_URL
        events: [success, failure]

      - type: email
        smtp_host: smtp.example.com
        smtp_port: 587
        smtp_username_env: SENTINEL_SMTP_USER
        smtp_password_env: SENTINEL_SMTP_PASSWORD
        from_address_env: SENTINEL_ALERT_FROM
        to_addresses: ["sre@example.com"]
        use_tls: true
        events: [failure]
```

The keys that are easy to get wrong:

- **Email sender and recipients are not symmetrical.** The sender is `from_address_env`, an environment
  variable name. The recipients are `to_addresses`, a literal list. There is no `from:` key and no `to:`
  key; a configuration using them fails validation.
- **`webhook_url_env` names a variable, never the URL.** The same is true of every credential here. A
  webhook URL is a bearer secret, so it is treated like a password.
- **`events` is required** and accepts only `success`, `failure`, and `warning`. An empty or omitted list
  is a validation error rather than a silent "everything".
- **`enabled` defaults to true**, so a channel is live as soon as it is declared. Set `enabled: false` to
  park one without deleting it.
- **`smtp_port` defaults to 587 and `use_tls` to true**, meaning STARTTLS on the SMTP connection.
  `smtp_username_env` is optional; omitting it sends unauthenticated.
- **`timeout_seconds` defaults to 10** for the three HTTP channels and must fall between 1 and 60.

The full key list is in the [configuration reference](../reference/configuration.md).

## Example

Set the secrets, then confirm the configuration is accepted before relying on it:

```bash
export SLACK_WEBHOOK_URL="$(cat /run/secrets/slack_webhook)"
export SENTINEL_WEBHOOK_URL="$(cat /run/secrets/sentinel_webhook)"
export SENTINEL_SMTP_PASSWORD="$(cat /run/secrets/smtp_password)"
export SENTINEL_ALERT_FROM=sentinel@example.com

sentinel config validate --config sentinel.yaml
```

```
configuration is valid
```

Run the job, and the generic webhook channel receives a body of this shape:

```json
{
  "event": "success",
  "status": "success",
  "timestamp": "2026-08-05T02:00:31Z",
  "backup_name": "prod-postgres",
  "database": "myapp_prod",
  "database_type": "postgres",
  "duration_ms": 31402,
  "file_size": 184320117,
  "file_path": "./backups/prod-postgres.sql",
  "error": "",
  "message": "Backup Execution Report\nDatabase: myapp_prod\n..."
}
```

Because the Slack channel above subscribes only to `failure`, it receives nothing from this run. That is
the intended behaviour, not a delivery problem.

## Failure modes

**A backup that was skipped because the job was already running notifies nothing.** When the per-job
lock is held, the run returns early, before both the history record and the notification step. The
overlapping run is invisible to every channel, and the only trace is the CLI output of the skipped
process ([issue #180](https://github.com/denisakp/sentinel/issues/180)). If overlapping runs are a real
risk for a job, watch its cadence in `sentinel monitor list` rather than relying on an alert.

**A channel subscribed only to `warning` never fires for backups.** Backup outcomes map to `success` or
`failure` and nothing else. The runbook at `docs/runbooks/alerting-setup.md` implies otherwise; it is
wrong for backups, and correct only for restores, where `skipped` maps to `warning`. Subscribe backup
channels to `failure`, and add `warning` only on restore jobs
([issue #180](https://github.com/denisakp/sentinel/issues/180)).

**An email example copied from `docs/runbooks/alerting-setup.md` does not load.** That runbook uses
`from:` and `to:`, which are not keys in the schema. The validator requires `from_address_env` and
`to_addresses`, along with `smtp_host` and `smtp_password_env`. Use the excerpt on this page instead
([issue #180](https://github.com/denisakp/sentinel/issues/180)).

**The job runs but every channel is silent, with `notification error:` on stderr.** The dispatcher could
not be constructed. Almost always this is a missing environment variable, reported as
`environment variable not found: NAME`, or a variable name that does not match `^[A-Z_][A-Z0-9_]*$`.
Note the blast radius: one unresolvable channel prevents *all* channels on that job from being
registered, because construction fails as a unit.

**Slack or Discord returns a non-2xx status.** The error text includes the status code, the response
body, and the `Retry-After` header when the remote sent one. There is no retry; a rate-limited or
briefly unavailable webhook simply drops that message. The backup itself is unaffected.

**Email delivery fails with a TLS error.** `use_tls: true` issues STARTTLS on a plain connection, which
suits port 587. A server expecting implicit TLS on port 465 will not negotiate; use the submission port
instead.

## Related

- [Backup](./backup.md): the run sequence whose final step is notification.
- [Restore](./restore.md): the only path that emits `warning`.
- [Schedule](./schedule.md): unattended runs, where notifications matter most.
- [Locking and concurrency](./locking.md): why a skipped run exists and what it does instead.
- [Monitoring and execution history](./monitoring-history.md): the durable record that notifications
  are a live view of.
- [Setting up notifications](../guides/alerting-setup.md): wiring a first channel end to end.
- [Credential sanitization](./credential-sanitization.md): why webhook URLs and SMTP passwords are
  environment-only.
- [Configuration reference](../reference/configuration.md): every notification key.
- [`sentinel config` reference](../reference/cli/config.md): validating a configuration before trusting it.
- [`sentinel schedule` reference](../reference/cli/schedule.md): running jobs unattended.

<!-- sources: internal/adapters/notifier/dispatcher.go, internal/adapters/notifier/converter.go, internal/adapters/notifier/types.go, internal/adapters/notifier/format.go, internal/adapters/notifier/slack.go, internal/adapters/notifier/discord.go, internal/adapters/notifier/email.go, internal/adapters/notifier/webhook.go, internal/ports/notifier.go, internal/config/types.go, internal/config/validator.go, internal/config/loader.go, internal/config/restore_types.go, internal/domain/backup/executor.go, internal/cli/restore.go, internal/cli/backup_factory.go, internal/cli/backup.go, docs/runbooks/alerting-setup.md -->
