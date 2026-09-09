# PRD I12 — Dead config keys + notification isolation

**Severity:** Medium (keys that read as supported features; one bad secret silences every channel)
**Status:** Open
**Issues:** #141, #143, #146, #155, #172, #187
**Area:** `internal/config/{types,loader,marshal,restore_types}.go`, `internal/cli/schedule.go`,
`internal/adapters/notifier/converter.go`

A key that parses, validates and does nothing is worse than an absent one: it reads as a supported
feature. Each item below states explicitly whether the fix is to **implement** or to **remove**.

## Confirmed defects

| # | Defect | Cause | Direction |
|---|---|---|---|
| #141 | `scheduler.max_concurrent_backups` is ignored | `internal/cli/schedule.go:64,165,272` — all three `NewScheduler()` calls pass the **top-level** `cfg.MaxConcurrentBackups`, never `cfg.Scheduler.MaxConcurrentBackups`. `loader.go:72-73` copies top-level into nested as a default; nothing ever reads the nested field afterwards | **IMPLEMENT** — pass the nested value; the loader already supplies the right fallback |
| #143 | `restore_defaults` is parsed but unreachable | `config/restore_types.go:167-183` — `RestoreConfiguration` / `RestoreDefaults` are never constructed; `LoadConfig` builds only `*Configuration`. `grep "RestoreConfiguration\b"` and `"ApplyRestoreDefaults"` outside their own file → zero non-test hits | **REMOVE** — wiring it needs a second config path that does not exist |
| #155 | `wal_summary_check` is stored but never probes the server | `config/types.go:126` has no reader. The related `ValidatePostgresIncrementalPrerequisites` (`domain/backup/incremental/prerequisites.go:26`) takes a `walSummaryEnabled` param and is called only from `planner_test.go` — and not even fed from the config field | **DECISION** — see below |
| #146 | `tls.enabled`'s doc comment claims a default of `true`; the loader defaults it to `false` | `config/types.go:9-10` vs `loader.go` `applyTLSDefaults:139-147`, which sets only `Mode` and never touches `Enabled`, so the zero value wins | **DECISION** — see below |
| #172 | `restore_options.additional_args` is validated at load then silently discarded | `config/marshal.go:294-317` `BuildRestoreAdditionalArgs` iterates `job.RestoreOptions` for known **bool** flags only (`clean`, `if_exists`, `no_owner`, `no_privileges`, `gzip`) and never reads the `additional_args` **string** key that `restore_types.go:271-278` validates via `backup.ParseAdditionalArgs` | **IMPLEMENT** — thread the string through, reusing the already-invoked parser |
| #187 | One unresolvable notification secret silences every channel on the job | `adapters/notifier/converter.go:14-92` `NewDispatcherFromConfig` returns `(nil, err)` on the **first** channel construction failure, discarding notifiers already built and never processing the rest. The caller (`cli/backup_factory.go:120-129`) then sets `notif = nil` — no notifications at all | **IMPLEMENT** — per-channel isolation |

## On #187

Architecturally distinct from the rest of this PRD: it is an error-handling blast-radius bug, not a
dead key. It is grouped here only because it is a config-resolution failure.

**Fail-fast at config load is the wrong fix.** Env var presence is a runtime fact resolved at
dispatch construction; the static validator's `isValidEnvVarName` can only check the *name format*,
never whether the variable is set at run time. The correct fix is log-and-skip the failing channel
and keep building the rest — which is what the issue itself proposes.

**Fix this first in this PRD.** It is the only item where current behaviour actively breaks an
unrelated, correct configuration: a mistyped Slack secret silences a working email channel.

## DECISION REQUIRED

- **#155** — implement (probe the server for `summarize_wal` via `db_probe` and call
  `ValidatePostgresIncrementalPrerequisites` from the backup planner when the key is true), or
  remove the key. It currently reads as a safety check on incremental backup prerequisites and
  performs none. Pairs with **#185** in I09 — the same dead prerequisite functions. **Decide both
  together.**
- **#146** — correcting the comment is honest and risk-free. Correcting the loader to default
  `Enabled: true` means **`tls:` alone silently enables TLS for every existing config** — a
  behaviour change with security implications the issue does not ask for. Recommendation: fix the
  comment. Security call, not a technical one.

## Fix order

1. **#187** — per-channel isolation in `converter.go`.
2. **#172** and **#141** — small mechanical wiring fixes.
3. **#143** — deletion, no behaviour risk.
4. **#146** — whichever the decision resolves to.
5. **#155** — largest surface; needs new probe wiring and live validation.

## Definition of done

- **Code:** the six items.
- **e2e** (uses `assert_config_key_changes_behavior` from I00 — this PRD is the reason that helper
  exists):
  - Two notification channels, one with an unresolvable `*_env` — run a backup, assert the **other**
    channel still received its notification.
  - `scheduler.max_concurrent_backups` set differently from the top-level key — the effective
    concurrency limit matches the **nested** value.
  - `restore_options.additional_args: "--foo"` — `--foo` appears in the actual restore-tool
    invocation. Needs a capture or dry-run seam.
  - `wal_summary_check: true` against postgres:17 with `summarize_wal=off` — the backup fails or
    warns instead of silently proceeding. **NEEDS_LIVE.** Only if #155 resolves to "implement".
  - `config load` with a `tls:` block and no `enabled` key — asserts whichever default #146 resolves
    to.
  - After the #143 deletion, no config key named `restore_defaults` is accepted.
- **Docs:** `concepts/backup.md:101`, `concepts/incremental-pitr.md:167,184` already cite #155 by
  number and document it as broken. #141, #143, #146, #172 and #187 have **no** doc pages —
  `alerting-setup.md` covers notifications and is tracked separately as #180 (closed). If #187's fix
  changes behaviour, the notifications page needs it.
- **Runbooks:** `docs/runbooks/additional-args.md` is wrong per #172; update it with the fix.
