# PRD I08 — Compression wiring + validation ordering

**Severity:** Medium (a flag that does nothing; a guard that fires on a valid config)
**Status:** Open
**Issues:** #183, #188
**Area:** `internal/cli/backup.go`, `internal/adapters/dump/{mysql,mariadb}/`,
`internal/config/{loader,validator}.go`, `internal/cli/config_resolver.go`

## Problem

### #183 — `backup --compress` is a no-op on MySQL and MariaDB

`buildSingleDump`'s `mysql` / `mariadb` branches in `internal/cli/backup.go` (~393-400) never read
`cmd.Flags().Changed("compress")`. The `pg` and `mongo` branches do. `MySqlDumpArgs` and
`MariaDBDumpArgs` have **no `Compress` field at all** —
`grep -n "Compress" internal/adapters/dump/{mysql,mariadb}/*.go` returns zero matches.

### The deeper finding — validation runs before CLI overrides

`internal/cli/config_resolver.go:53` calls `config.ValidateConfig` inside `LoadAndValidateConfig`.
That runs **before** `applyCLIOverrides` (`backup.go:657`), which is where the flag sets
`job.DatabaseOptions["compress"]`.

The double-compress guard at `validator.go:563` therefore validates a config that has not yet
received its CLI overrides. **This is wider than compression: every CLI override escapes
validation.** It is the reason #183's second half (bypassing the guard) happens, and it is worth
fixing as its own item.

### #188 — `defaults.compression` inherits into engines that compress natively

`internal/config/loader.go:103-105`:

```go
if job.Compression == nil && cfg.Defaults.Compression != nil { job.Compression = &inherited }
```

Unconditional. Nothing exempts jobs that already have engine-native compression configured
(`pg_compression_algo`, mongo `compress`, `gzip`). Then `validator.go:563-564` sees
`hasNativeCompression` **and** `job.Compression.Enabled` both true and hard-errors. The user never
wrote a conflicting config — the defaults block created one.

## Secondary findings

Both inspected and confirmed in shape, not deep-dived:

- pg's `--compress=algo:level` (`args_builder.go:90-126`) is appended with no `pg_out_format` gate.
- restore's `restore_options.gzip` is manual-only for native mongo gzip, whereas pipeline
  compression is auto-reversed. Asymmetric.

## Fix

1. **#188 first.** Skip inheritance (or auto-disable it) when `hasNativeCompression(job)` is true.
   This gives one coherent "does this job already compress natively?" check.
2. **#183** — wire compress into the MySQL/MariaDB dump args, **or** reject the flag for those
   engines with a clear error. Silently accepting it is the current behaviour and the worst option.
3. **Validation ordering** — re-validate after `applyCLIOverrides`, or move override application
   ahead of validation. Scope this deliberately: it will surface other configs that were passing
   validation only because their overrides were invisible to it.

## DECISION REQUIRED

**#183 direction.** Implement compression for MySQL/MariaDB (they support `--compress` for the
client-server protocol, which is **not** the same as compressing the dump output — check which the
flag is meant to mean before implementing), or reject the flag for those engines. The two produce
very different artifacts.

**Validation ordering blast radius.** Fixing item 3 may cause configs that load today to start
failing. That is correct behaviour but it is a breaking change. Confirm the release it lands in.

## Definition of done

- **Code:** the three fixes.
- **e2e:**
  - `backup -t mysql --compress` — the artifact has gzip magic bytes, or the command is rejected
    with a clear message. Whichever the decision above picks, assert it.
  - `defaults.compression.enabled: true` plus a postgres job with `pg_compression_algo: gzip` and no
    per-job override — loads and validates without the double-compress error.
  - A CLI override that would fail validation — is now caught at validation time, not at run time.
- **Docs:** `website/docs/concepts/compression.md` references #183. Update it with the fix.
