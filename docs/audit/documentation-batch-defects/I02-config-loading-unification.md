# PRD I02 — Config-loading unification + read-only monitor open

**Severity:** Medium (surprising behaviour, one command mutates state just by being inspected)
**Status:** Open
**Issues:** #160, #171, #173, #179.3, #181.4
**Area:** `internal/cli/{security,security_reencrypt,backup_verify,db,monitor_doctor,config_resolver}.go`,
`internal/adapters/monitor/init.go`

## Problem

`internal/cli/config_resolver.go` exists to give every command one config path, one loader and one
validation gate (`ResolveConfigPath`, `LoadAndValidateConfig`, which calls `config.ValidateConfig`).
**Four commands bypass it**, each reinventing a different subset — and they disagree with each other
about the default path, about whether the config is validated, and about whether opening the history
database mutates it.

## Confirmed defects

| # | Defect | Cause |
|---|---|---|
| #160 | `security init-key`'s overwrite guard checks a hard-coded env var name | `security.go:29` reads `os.Getenv("SENTINEL_MASTER_KEY")`. The command **has no `--config` flag at all** — it structurally cannot consult `encryption_key_env` |
| #171 | `security reencrypt` uses a different default config path and skips validation | `security_reencrypt.go:174-178` hardcodes `$HOME/.sentinel/config.yaml` and calls `config.LoadConfig` directly. Everything else uses `config_resolver.go:12`'s `./sentinel-config.yaml` and gets `ValidateConfig` for free |
| #181.4 | `backup verify` tolerates an invalid config, `monitor` does not | `backup_verify.go:121` calls `config.LoadConfig`; `monitor.go:31/71/112/146` call `loadConfigFromFlags` → `LoadAndValidateConfig` |
| #173 | `db migrate status` **migrates the database as a side effect of being run** | `monitor.NewMonitor()` unconditionally creates `schema_migrations` and runs `runMigrationsLocked` on every open; there is no read-only mode. Reproduced: `db migrate status` logged "migration starting/complete" and wrote `schema_migrations` rows, exit 0 |
| #179.3 | The gates are inverted | The **mutating** `db migrate status` (`db.go:33`) uses the loose `LoadConfigMinimal`; the **read-only** `monitor doctor` (`monitor_doctor.go:20`) uses the strict `LoadAndValidateConfig` |

Two further findings inside #173:

- `db.go:33-39` checks `cfg.HistoryDBPath == ""` to refuse. **That branch is unreachable**:
  `loader.go:64-65` always defaults the path to `~/.sentinel/history.db` before the check runs. So
  with the key omitted, the command migrates a database in the user's home directory.
- `LoadConfigMinimal` (`config_resolver.go:64-76`) is not actually minimal: it calls
  `config.LoadConfig`, which still enforces `len(cfg.Databases) == 0` and runs
  `applyEnvOverrides` / `interpolateConfig`. It skips only `ValidateConfig`.

Two extra defects found inside #171 that the issue text does not cover:

- `--new-key-env` overrides `targetCfg` **regardless of `legacyMode`** (`security_reencrypt.go:~197`),
  so it rekeys in legacy mode too. The flag help at line 748 does not say so.
- `swapArtifact`'s **remote** branch (`security_reencrypt.go:503-534`) returns `destObject` (the
  artifact key) where `RecordSecurityInfo` expects `manifestPath`. The local branch at line 552
  correctly returns `destManifest`. Remote backends record a wrong manifest path in history.

## Root cause

Pattern **A**. The shared helper landed after these commands were written and was never
retro-applied. Each command's divergence is individually small and collectively means there is no
answer to "which config does Sentinel read, and is it validated?"

## Fix

1. Add a **read-only open** to `internal/adapters/monitor/init.go` — a `NewMonitor` variant that
   does not create tables or run migrations. Route `db migrate status` through it. This fixes #173
   and #179.3 together and is the only structural item here.
2. Route `security init-key`, `security reencrypt` and `backup verify` through
   `config_resolver.ResolveConfigPath` + `LoadAndValidateConfig`. Add the missing `--config` flag to
   `security init-key`.
3. `security init-key`: resolve the env var name from `encryption_key_env`, falling back to
   `SENTINEL_MASTER_KEY` only when no config or no field is present.
4. `security_reencrypt.go`: return `destManifestObject` from the remote branch of `swapArtifact`.
5. `security_reencrypt.go:748`: document `--new-key-env`'s legacy-mode rekey behaviour in the flag
   help.
6. Remove or fix the unreachable `HistoryDBPath == ""` check in `db.go`; require an explicit
   `history_db_path` for `db migrate status` rather than silently defaulting into `$HOME`.

## DECISION REQUIRED

`backup verify` currently tolerates an invalid config. That may be **deliberate** — verify is a
recovery-time command, and refusing to run because an unrelated job has a bad cron expression would
be hostile exactly when it is needed. Decide: route it through the strict gate, or keep the loose
one and document why. Do not change it silently.

## Definition of done

- **Code:** the six fixes above.
- **e2e:**
  - `security init-key` with a config setting `encryption_key_env: ACME_KEY` and `ACME_KEY` set,
    without `--force` — warns/refuses instead of generating a new key.
  - `db migrate status` against a stale-pending fixture DB — `schema_version` **unchanged** after
    the call.
  - `backup verify --all` against a validator-rejected config — behaves the same way as
    `monitor doctor` against the same config (whichever way the decision above goes).
  - `security reencrypt` against a remote-backed backup — the history row's `manifest_path` ends in
    `.manifest.json`, not the artifact key. **NEEDS_LIVE** (object store) for this leg only.
- **Docs:** `operations/key-loss-incident.md`, `reference/cli/security.md`,
  `operations/recover-legacy-envelope.md`, `guides/db-migration-status.md`,
  `guides/monitoring-history.md`.
