# Runbook — `additional_args` quoting reference

> **Superseded by the documentation site: [reference/additional-args](https://denisakp.github.io/sentinel/reference/additional-args).**
>
> This runbook documents `restore_options.additional_args` as working; it is validated then discarded. The site page was checked against the source and describes current
> behaviour. Prefer it. This file is kept as source material and is not maintained in parallel.

Sentinel forwards the operator-supplied `additional_args` string to the
underlying dump or restore tool (`pg_dump`, `mysqldump`, `mariadb-dump`,
`mongodump`, and their restore counterparts) as command-line arguments.
Starting with this release, the string is tokenized using **POSIX shell
quoting rules** (via `github.com/google/shlex`) instead of a naive regex.

## Where `additional_args` is used

- `sentinel backup --args "..."` — operator-supplied string on the CLI.
- `restore_options.additional_args: "..."` — YAML field on every restore
  job in `sentinel.yaml`.

## Quoting syntax

| Construct                  | Behavior                                                          |
|----------------------------|-------------------------------------------------------------------|
| Whitespace                 | Separates tokens (one or more spaces/tabs).                       |
| Double quotes `"..."`      | Group tokens; `\"` and `\\` are escapes; other `\<c>` is literal. |
| Single quotes `'...'`      | Group tokens; NO escape processing; closing `'` ends the span.    |
| Backslash `\<c>`           | Outside quotes: emits `<c>` literally (escapes any character, including whitespace). |
| `$VAR`, `` `cmd` ``, `$()` | Literal — Sentinel does NOT expand variables or run subshells.    |
| `*`, `?`, `[`, `]`         | Literal — no glob expansion.                                      |
| `#` at start of word       | POSIX comment introducer. To pass a literal leading `#`, escape (`\#`) or quote (`"#tag"`, `'#tag'`). |
| `#` mid-token              | Literal (e.g. `--prefix=#tag` works as written).                  |
| NUL byte                   | Rejected as a config error.                                       |

## Examples

| Input                                                     | Tokens produced                                   |
|-----------------------------------------------------------|---------------------------------------------------|
| `--flush-privileges`                                      | `["--flush-privileges"]`                          |
| `--port=3306 --single-transaction`                        | `["--port=3306", "--single-transaction"]`         |
| `--exclude-table-data="audit logs"`                       | `["--exclude-table-data=audit logs"]`             |
| `--where='id > 100'`                                      | `["--where=id > 100"]`                            |
| `--where="updated_at > '2026-01-01'"`                     | `["--where=updated_at > '2026-01-01'"]`           |
| `"a \"b\" c"`                                             | `["a \"b\" c"]`                                   |
| `--name=foo\ bar`                                         | `["--name=foo bar"]`                              |
| `--prefix=#tag`                                           | `["--prefix=#tag"]`                               |
| `\#hash`                                                  | `["#hash"]`                                       |

## Breaking change from previous releases

The prior regex (`"[^"]*"|\S+`) leaked the surrounding quote characters into
the emitted token (e.g. `"audit logs"` became the literal token `"audit logs"`
with quotes attached). The new parser strips the surrounding quotes, matching
how every other CLI tool handles arguments. Operating-system `argv` has no
concept of quoting, so this is almost always a strict correction.

**If your `additional_args` previously depended on quotes leaking through:**

- The dump/restore tools never wanted the literal quote characters anyway —
  the quotes were always cosmetic on the wire.
- Remove the workaround. The unquoted form is what you actually wanted.

**Migration check:**

```bash
sentinel config validate --config sentinel.yaml
```

After upgrading, run the validator. If your `restore_options.additional_args`
contains an unterminated quote that was silently mis-tokenized before, you
will now see:

```
restore_options.additional_args: unterminated quote in additional_args: --where="x > 1
```

with the exact field that needs fixing.

## When validation fires

`additional_args` is parsed at TWO surfaces, both before any dump/restore
process is spawned:

1. **Config load time** — `ValidateRestoreJob` (called by `sentinel config
   validate` and at scheduler startup) parses every `restore_options.additional_args`
   value. A parse error fails validation immediately with the field path
   in the error message.
2. **Job execution time** — every engine adapter re-parses `additional_args`
   when building its `argv`. A parse error fails the job before the dump
   tool is invoked.

Operators who edit configs between loads are protected at execution time;
operators who run `config validate` catch the issue eagerly.

## Related

- ADR 0009 — Adopt `github.com/google/shlex` (`docs/adr/0009-shlex-args-parser.md`)
- Spec 023 — `specs/023-shlex-args-parser/spec.md`
