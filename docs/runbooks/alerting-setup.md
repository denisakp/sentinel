# Runbook — Alerting setup

> **Superseded by the documentation site: [guides/alerting-setup](https://denisakp.github.io/sentinel/guides/alerting-setup).**
>
> This runbook its email example does not load: the keys are `from_address_env` and `to_addresses`. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [failed-backup-triage](./failed-backup-triage.md), [restore-from-backup](./restore-from-backup.md), [start-scheduler](./start-scheduler.md)

## When to use

First-time enablement of notifications, or onboarding a new channel (Slack, Discord, email, generic webhook).

## Preconditions

- Webhook URL / SMTP credentials available and exportable via env var.
- Network egress from the Sentinel host to the channel.

## Steps

### Slack

```yaml
defaults:
  notifications:
    - type: slack
      webhook_url_env: SLACK_WEBHOOK_URL
      events: [failure, warning]
```

```bash
export SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...
```

### Discord

```yaml
defaults:
  notifications:
    - type: discord
      webhook_url_env: DISCORD_WEBHOOK_URL
      events: [failure, warning]
```

### Generic webhook

```yaml
defaults:
  notifications:
    - type: webhook
      webhook_url_env: OPS_WEBHOOK_URL
      events: [success, failure, warning]
```

### Email (SMTP)

```yaml
defaults:
  notifications:
    - type: email
      smtp_host: smtp.example.com
      smtp_port: 587
      smtp_username_env: SMTP_USER
      smtp_password_env: SMTP_PASSWORD
      from: sentinel@example.com
      to:   ["sre@example.com"]
      events: [failure]
```

## Event mapping

| Outcome              | Event triggered |
|----------------------|-----------------|
| backup `success`     | `success`       |
| backup `failure`     | `failure`       |
| restore `success`    | `success`       |
| restore `failed`     | `failure`       |
| restore `timeout`    | `failure`       |
| restore `skipped`    | `warning`       |
| scheduler `skipped`  | `warning`       |

Subscribe a channel to the categories that matter for it (`events: [failure, warning]` is typical for on-call).

## Test the wiring

Point at an unreachable host to force a failure:

```yaml
databases:
  test-fail:
    type: postgres
    host: 192.0.2.1     # TEST-NET-1, guaranteed unreachable
    port: 5432
    username: nobody
    password_env: NOPE
    database: nope
    output: test-fail
```

```bash
sentinel backup --config /tmp/test-alert.yaml --job test-fail
```

Confirm the alert reached the channel. Remove the test job afterward.

## Verification

```bash
sentinel monitor list --config <config> --status failure --last 5m
```

A `failure` row exists and the channel received exactly one notification per failure.

## Rollback / recovery

Remove the `notifications:` block to disable. Per-job overrides (when present) win over `defaults.notifications` — audit those.

## References

- `internal/adapters/notifier/` — Slack, Discord, email, webhook implementations (dispatcher + per-channel notifiers; context/config types in `internal/ports/notifier.go`)
- [failed-backup-triage](./failed-backup-triage.md) for what to do when an alert fires
