# PRD I10 — Restore/schedule command state + registration

**Severity:** High (a disabled restore job actually starts executing)
**Status:** Open
**Issues:** #136, #137, #138, #139, #140
**Area:** `internal/cli/{backup,restore,schedule}.go`, `internal/scheduler/restore_integration.go`

All five reproduced against the reference binary. None need a database.

## Confirmed defects

### #139 — `restore run <job>` executes a disabled job

`internal/cli/restore.go:333-374` `handleRestoreRun` never checks `job.Enabled`.
`internal/config/restore_types.go:220` `ValidateRestoreJob` does not either. Only
`handleRestoreRunAll` (~line 440) filters on it.

**Reproduction:** config with `restores.myjob.enabled: false`, then `restore run myjob`. The command
passed validation, **acquired the lock**, and reached the staging step before stopping on
`lstat /tmp/nonexistent-backups: no such file or directory`. It had begun executing the disabled
job; only a fake path stopped it. With a real backup source it would have restored.

The check exists. It is simply not called on the single-job branch.

### #136 — chain commands cannot be invoked at all

`internal/cli/backup.go:84,811,836,891` — `chain-status`, `chain-list` and `force-full` read
`cmd.Flags().GetString("config")`, but only the parent `BackupCmd` registers `--config`, as a
**local** flag (backup.go:246). The subcommands register nothing and the parent's flag is not
persistent.

**Reproduction:** `backup chain-status --config x.yaml` → `Error: unknown flag: --config`, exit 1.
Bare `chain-status` → `Error: required flag(s) "job" not set`, exit 1. No combination works.
Confirmed for all three subcommands.

### #140 — `schedule status` cannot see restore jobs

`internal/cli/schedule.go:272-283` registers only `cfg.Databases` before calling `s.JobStatus`.
`scheduleListCmd` (lines 173-194) registers **all three** families — databases, restores and
`config.IntegrityCheckJobName`.

**Reproduction:** config with one backup job and one restore job, both scheduled. `schedule list`
shows both. `schedule status myjob` → `Error: job 'myjob' not found`, exit 1.
`schedule status dummy-backup` → succeeds, exit 0.

### #137 — `restore enable`, `disable`, `pause`, `resume` are no-ops

`internal/cli/restore.go:283-295` logs and prints, persists nothing.
`internal/scheduler/restore_integration.go:536-548` `PauseRestoreJob` / `ResumeRestoreJob` log and
`return nil`, with the comment *"requires extending the Scheduler interface"*. **No state store
exists anywhere in the package.**

### #138 — `schedule stop` always fails

`internal/cli/schedule.go:146-152` `RunE` returns an error unconditionally. Zero conditional paths,
no PID file, no signal logic.

## Root causes

**(a) Flag registration** — #136 alone: local vs persistent `--config`.

**(b) Incomplete state wiring between the CLI handlers and the job registry** — #137, #139, #140.
Pattern **D**: the control exists on one path and not the sibling.

#138 is standalone: a deliberately unimplemented command.

## Fix order

1. **#136** — promote `BackupCmd`'s `--config` to `PersistentFlags()`, or register it on each
   subcommand. Trivial, and it unblocks chain inspection entirely — which several other PRDs need
   for debugging.
2. **#140** — mirror `scheduleListCmd`'s registration loop in `scheduleStatusCmd`. Same file.
3. **#139** — check `job.Enabled` on the single-job branch, as `--all` already does. One line,
   safety-critical, small blast radius.
4. **#137** — needs the decision below.
5. **#138** — needs the decision below.

## DECISION REQUIRED

**#137 and #138 are product decisions, not bugs to be patched.**

- **#137** — implement persistence (write `enabled` back to config, or a sidecar state file, or
  extend the Scheduler interface as the code comment anticipates), **or remove the four
  subcommands**. Printing "enabled" and persisting nothing is the only option that must not survive.
- **#138** — implement real stop semantics (PID file + signal), **or remove the subcommand**.

Recommendation for both: remove now, reintroduce with a design. A command that lies is worse than an
absent one, and both have a clear removal path.

## Definition of done

- **Code:** items 1-3, plus whatever items 4-5 resolve to.
- **e2e:**
  - `backup chain-status --config <valid.yaml> --job <name>` against a seeded history DB — exit 0
    with the expected chain output.
  - A restore job with `enabled: false` **and a valid local backup source** — `restore run <job>`
    refuses with a clear error and a non-zero exit. It must not acquire the lock.
  - `schedule status <restore-job>` and `schedule status __integrity_check` — both succeed with
    correct fields.
  - If #137/#138 are implemented: state survives a process restart. If removed: the subcommand is
    gone and `--help` does not advertise it.
- **Docs:** `operations/troubleshooting.md`, `operations/failed-backup-triage.md`,
  `operations/chain-corruption-recovery.md` (all cite #136);
  `guides/restore-from-gcs.md`, `guides/parallel-restore.md` (#137, #139);
  `operations/scheduler-crash-recovery.md`, `guides/index.md` (#138).
  #140 has no doc page — the `schedule status` reference page currently implies it covers all jobs.
