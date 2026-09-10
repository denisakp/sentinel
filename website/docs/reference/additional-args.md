---
title: additional_args parsing
description: How Sentinel tokenises operator-supplied extra arguments with POSIX shell quoting, what it never expands, and where parse errors surface.
sidebar_position: 3
---

Sentinel forwards operator-supplied extra arguments to the underlying dump and restore binaries: `pg_dump`, `pg_dumpall`, `mysqldump`, `mariadb-dump`, `mongodump`, and their restore counterparts. The string is tokenised with POSIX shell quoting rules, by `github.com/google/shlex`, and never by a shell.

## Where extra arguments come from

| Surface | Applies to | Notes |
|---|---|---|
| `sentinel backup --args "…"` | Every job in the run | The only free-form argument surface for backups. When `--config` is also given, the flag replaces whatever the job would otherwise have contributed, for that run only. |
| `restore_options.additional_args` in the YAML | One restore job | Validated at configuration load and forwarded to the restore tool. Dropped on v1.4.0 and earlier; see the note below. |

There is **no `additional_args` key on a backup job**. `database_options:` is an allowlist of typed keys per engine, so a free-form string is rejected:

```text
Error: invalid config "sentinel.yaml": backup 'demo': unsupported postgres option 'additional_args'
```

| Engine | Allowed `database_options` keys |
|---|---|
| PostgreSQL | `pg_out_format`, `compress`, `pg_compression_algo`, `pg_compression_level` |
| MySQL, MariaDB | `single_transaction`, `routines`, `triggers`, `events` |
| MongoDB | `gzip`, `oplog`, `archive` |

:::note `restore_options.additional_args` is forwarded, since the fix for issue #172
The value reaches the restore tool. It is appended **after** the flags derived from the boolean restore options (`clean`, `if_exists`, `no_owner`, `no_privileges`, `gzip`), so where a tool honours the later occurrence of a flag your own arguments win.

Those derived flags are now emitted in a fixed order. They previously came out in Go map order, which is randomised, so the same configuration produced a different argument string from one run to the next.

**On v1.4.0 and earlier the string was dropped.** The loader parsed and validated it, and the translation into the restore spec built its argument string from the boolean options only, so `sentinel restore run` never passed it to `pg_restore`, `mysql`, `mariadb` or `mongorestore`. A configuration containing it validated cleanly and appeared to work. On those versions treat the key as inert.
:::

## Tokenisation rules

| Construct | Behaviour |
|---|---|
| Whitespace | Separates tokens. One or more spaces or tabs. |
| Double quotes `"…"` | Group a token. `\"` and `\\` are escapes; any other `\<c>` stays literal. Surrounding quotes are stripped. |
| Single quotes `'…'` | Group a token with no escape processing at all. The next `'` ends the span. Surrounding quotes are stripped. |
| Backslash `\<c>` outside quotes | Emits `<c>` literally, including whitespace. |
| `$VAR`, `` `cmd` ``, `$(cmd)` | Literal. No variable expansion, no command substitution, no subshell. |
| `*`, `?`, `[`, `]` | Literal. No glob expansion. |
| `#` at the start of a word | POSIX comment introducer. It and everything after it is discarded. |
| `#` inside a token | Literal. `--prefix=#tag` works as written. |
| NUL byte | Rejected as a configuration error. |
| Empty or whitespace-only input | Yields zero tokens, no error. |

Because the arguments are handed to the dump binary through `exec`, and never through `/bin/sh`, there is no shell to expand anything. The quoting rules exist to let you group a token that contains spaces, not to give you shell features.

:::warning A leading `#` silently discards the rest of the string
`#tag --real` tokenises to an **empty** argument list, with no error and no warning. The comment runs to the end of the input, so a stray `#` at the start swallows every argument after it. To pass a literal leading `#`, escape it (`\#tag`) or quote it (`"#tag"`, `'#tag'`).
:::

## Worked examples

Every row below is the verified output of Sentinel's parser.

| Input | Tokens produced |
|---|---|
| `--flush-privileges` | `["--flush-privileges"]` |
| `--port=3306 --single-transaction` | `["--port=3306", "--single-transaction"]` |
| `--exclude-table-data="audit logs"` | `["--exclude-table-data=audit logs"]` |
| `--where='id > 100'` | `["--where=id > 100"]` |
| `--where="updated_at > '2026-01-01'"` | `["--where=updated_at > '2026-01-01'"]` |
| `"a \"b\" c"` | `["a \"b\" c"]` |
| `--name=foo\ bar` | `["--name=foo bar"]` |
| `--prefix=#tag` | `["--prefix=#tag"]` |
| `\#hash` | `["#hash"]` |
| `#tag --real` | `[]` |
| `--x=$HOME` | `["--x=$HOME"]` |
| `--x=*.sql` | `["--x=*.sql"]` |

The surrounding quotes are stripped, which is what an operating system `argv` requires: there is no notion of quoting once a process has been executed. A dump tool never wanted the literal quote characters.

## Errors

Two sentinel errors, both raised before any process is spawned.

| Condition | Message |
|---|---|
| Unterminated quote, or a trailing backslash escape at end of input | `unterminated quote in additional_args: <input>` |
| A NUL byte anywhere in the input | `NUL byte in additional_args: <input>` |

A trailing backslash reports the quote error rather than a distinct one, because the tokeniser reports both as end-of-input inside a token.

### Where they surface

Parsing happens at two points, and both run before the dump or restore binary is invoked.

1. **Configuration load.** Restore job validation parses `restore_options.additional_args`. The failure names the job and the field:

   ```text
   Error: invalid config "sentinel.yaml": restore 'demo_restore': restore_options.additional_args: unterminated quote in additional_args: --where="x > 1
   ```

2. **Job execution.** Every dump and restore adapter re-parses its argument string while building `argv`, so an operator who edited the configuration between the last validation and the run is still protected. The job fails with `failed to parse additional_args: …` before the tool starts.

Run `sentinel config validate --config sentinel.yaml` to catch the problem eagerly rather than at 02:00.

## Placement in the final argument list

Extra arguments are appended after the arguments Sentinel builds itself, and every engine then removes duplicate tokens from the whole list before executing.

| Engine | Order |
|---|---|
| PostgreSQL | Connection and format flags, compression flags, **extra arguments**, TLS flags, then de-duplication. |
| MySQL, MariaDB | `--host`, `--port`, `--user`, `--skip-password` when no password is set on MySQL, **extra arguments**, TLS flags, de-duplication, then the database name last. |
| MongoDB | `--uri`, output mode, `--quiet`, `--db`, `--gzip`, **extra arguments**, TLS flags, then de-duplication. |

De-duplication compares whole tokens for exact equality. It removes a repeat of a flag Sentinel already emitted in exactly the same form; it does not reconcile `--compress=gzip:1` against `--compress=zstd:3`, and it does not detect a flag passed in two different spellings. Passing an option Sentinel also sets can therefore produce a genuinely contradictory command line that the dump tool itself rejects.

MongoDB has one extra rule: if your extra arguments contain `--archive` or `--archive=…`, Sentinel suppresses its own `--archive=` or `--out=` and lets yours stand. See [MongoDB staging directories](./mongo-staging.md).

## Credentials in extra arguments

Do not put a password in `--args`. Everything in `argv` is visible through `ps`, `/proc/<pid>/cmdline`, and shell history, and extra arguments are the one part of the command line Sentinel does not construct for you. Use `--password-env`, `--password-file`, or the `*_env` configuration keys instead; see [credential sanitization](../concepts/credential-sanitization.md).

:::warning A `mongodump` failure can echo the full argument list
When `mongodump` exits non-zero and writes nothing to stdout or stderr, Sentinel includes the complete constructed command in the error text without passing it through the redactor. Because the MongoDB argument list contains `--uri=`, a URI holding userinfo credentials is printed in full. The dump adapters for PostgreSQL, MySQL, and MariaDB do not have this path; they only ever emit redacted stderr. Prefer a URI without embedded credentials for MongoDB jobs.
:::

## Related

- [Configuration reference](./configuration.md)
- [MongoDB staging directories](./mongo-staging.md)
- [`sentinel backup`](./cli/backup.md)
- [`sentinel restore`](./cli/restore.md)
- [`sentinel config`](./cli/config.md)
- [Credential sanitization](../concepts/credential-sanitization.md)
- [Backups: what Sentinel captures and how](../concepts/backup.md)

{/* sources: internal/domain/backup/args.go, internal/config/restore_types.go, internal/config/marshal.go, internal/config/validator.go, internal/cli/backup.go, internal/adapters/dump/pg/args_builder.go, internal/adapters/dump/mongo/args_builder.go, internal/adapters/dump/mongo/mongo_dump.go, internal/adapters/mysqlargs/core.go, internal/adapters/restore/pg/args_factory.go, internal/sanitize/sanitize.go, docs/runbooks/additional-args.md */}
