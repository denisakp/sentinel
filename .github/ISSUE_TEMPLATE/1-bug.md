---
name: Bug report
about: Report a defect in backup, restore, scheduling, storage, or monitoring behavior
title: "bug: "
labels: 'bug'
assignees: denisakp

---

## Summary

Describe the bug clearly and include impact (data loss risk, restore failure, scheduler blocked, etc.).

## Reproduction Steps

Steps to reproduce the behavior:

1. Go to '...'
2. Click on '...'
3. Scroll down to '...'
4. See error

## Expected Behavior

A clear and concise description of what you expected to happen.

## Actual Behavior

A clear and concise description of what actually happens

## Scope

- Affected command(s): `backup` / `restore` / `schedule` / `retention` / `monitor` / other
- Affected database type(s): postgres / mysql / mariadb / mongodb
- Affected storage backend(s): local / s3 / google-drive / azure
- Frequency: always / intermittent / once

## Logs and Errors

Paste relevant error output and logs.

```text
# sentinel command output
```

## Minimal Config (Sanitized)

If applicable, share a minimal `sentinel.yaml` snippet.
Do not include secrets. Keep `_env` references only.

```yaml
version: "1.0"
databases:
  sample:
    type: postgres
    host_env: DB_HOST
    username_env: DB_USER
    password_env: DB_PASSWORD
    database: app
    storage:
      type: local
      local_path: ./backups
```

## Screenshots / Artifacts

If applicable, add screenshots to help explain your problem.

## Environment

- Sentinel version or commit SHA:
- Install method: source build / container / other
- OS: Linux / macOS / Windows
- Database type and version:
- Storage backend:
- Go version (if built from source):
- Docker/Compose/Kubernetes version (if relevant):

## Regression Check

- [ ] This worked in an earlier Sentinel version
- [ ] I can reproduce with latest `main`
- [ ] I reviewed `docs/roadmap/ROADMAP.md` and this is a defect, not an unreleased feature

## Additional Context

Add any other context about the problem here.
