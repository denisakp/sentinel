# ADR 0009 — Adopt `github.com/google/shlex` for additional_args parsing

- **Status**: Accepted
- **Date**: 2026-05-22
- **Deciders**: Sentinel maintainers
- **Tags**: dependency, cli, backup, restore

## Context

Sentinel exposes a free-form `additional_args` string at two surfaces:
the `--args` CLI flag on `sentinel backup` and the `restore_options.additional_args`
field on every restore job in `sentinel.yaml`. The operator's string is appended
verbatim to the underlying dump/restore tool's `argv`.

Tokenization was previously handled by two divergent helpers:

- `internal/backup/args.go` used a regex `"[^"]*"|\S+` that mis-tokenized
  any quoted value containing spaces (`--exclude-table-data="audit logs"`
  became two tokens, the second still wrapped in a stray quote).
- `pkg/restore/{pg,mysql,mariadb,mongo}_restore/*.go` each shipped their own
  `parseCLIArgs` using `strings.Fields`, which has zero quote handling
  at all and silently splits any quoted value into multiple tokens.

PRD 16 documents the bug. Both helpers must be replaced with a real POSIX
shell tokenizer. The choice of tokenizer needs to be recorded here because
this is Sentinel's first dependency on an argument-parsing library, and
ADR 0003 forbids creating an `internal/utils/`-style junk drawer to house
the result — the parser must live in a package whose domain it serves.

## Decision drivers

- Correctness across the PRD 16 acceptance matrix (`'a b'`, `"a \"b\""`,
  nested single-in-double quotes, embedded whitespace in values).
- Hard fail on malformed input (unterminated quote) — operators must
  not get a silently mis-tokenized argv passed to the dump tool.
- Zero or minimal transitive dependencies; Sentinel ships as a single
  static binary and audits its module graph.
- No shell evaluation (variable expansion, command substitution, glob
  expansion). The parser must be a pure tokenizer; expansion would
  create injection and credential-leak risk.
- Stable, low-maintenance dependency. The POSIX tokenization spec is
  itself stable, so a quiet upstream is acceptable.

## Options considered

### Option A — `github.com/google/shlex`

Pure Go POSIX shell tokenizer. Single function used: `shlex.Split(s string) ([]string, error)`. Apache-2.0.

**Pros**
- Zero transitive dependencies — adds one line to `go.sum`.
- API surface matches the spec FR-001..FR-004 exactly (returns
  `(tokens, error)`, errors on unterminated quote).
- Battle-tested in major Go projects (gVisor, Bazel rules, k8s tools).
- POSIX-correct for every PRD 16 acceptance case.

**Cons**
- Quiet upstream (last commit 2018). Mitigated by the fact that POSIX
  tokenization is a fixed target; there is no behavior to chase.
- Treats `#` as a POSIX comment introducer at word boundaries (spec
  FR-006 reflects this rather than fighting it).

### Option B — `mvdan.cc/sh/v3/syntax`

Full Bash parser by Daniel Martí.

**Pros**
- Actively maintained, comprehensive POSIX + Bash coverage.

**Cons**
- Pulls in a non-trivial transitive dependency tree
  (`golang.org/x/sys`, `golang.org/x/term`, etc.).
- Exposes far more API than we need; tokenization is a small fraction
  of its surface area.
- Encourages later misuse (expansion, command substitution) that
  would violate Sentinel's security posture.

### Option C — Hand-rolled tokenizer (~80 LOC)

Write a POSIX-quote-aware byte scanner in-tree.

**Pros**
- No new dependency.

**Cons**
- Security-sensitive parser; Sentinel inherits ownership of every
  POSIX edge case (backslash inside double quotes, NUL handling,
  escape-at-EOF, single-quote-inside-double).
- The bug class the PRD targets is exactly the kind a hand-rolled
  parser is prone to reintroduce.
- Maintenance cost over the next 5 years vastly exceeds the cost
  of carrying `google/shlex` as a transitive-free dependency.

## Decision

Sentinel adopts `github.com/google/shlex` for tokenizing the
`additional_args` string. The parser lives in `internal/backup/args.go`
(the domain shared by every dump and every restore engine, per ADR 0003).
A pre-check rejects NUL bytes; `shlex.Split` handles every other case.

## Consequences

### Positive

- Single tokenization model across backup and restore. Operators
  learn one quoting syntax, not two.
- Quoted values with embedded whitespace now reach the dump tool intact.
- Unterminated quotes fail fast with `ErrUnterminatedQuote`,
  surfaced at config-load time AND at job-execution time.
- Removes ~40 LOC of duplicated `parseCLIArgs` from four restore packages.

### Negative

- One new module in `go.sum`. Zero transitive deps; impact on binary
  size is negligible (~10 KB stripped).
- Documented breaking change for operators whose configs relied on
  the prior regex leaking surrounding quote characters into argv.
  No real-world tool consumes those leaked quotes, so the change is
  almost always a strict correction; release notes call it out.

### Neutral / to watch

- Treatment of `#` as a comment introducer at word boundaries is
  POSIX-correct but may surprise operators who expected the literal
  `#` behavior of the prior regex. The operator-facing runbook covers
  the workarounds (`\#`, `"#"`, `'#'`).
- If google/shlex ever goes unmaintained AND a POSIX semantics change
  appears (extremely unlikely), Option C becomes the fallback. Until
  then the dependency is effectively frozen, which matches Sentinel's
  needs.

## Compatibility & migration

No change to on-disk backup layout, manifest format, or encryption envelope.
No change to `sentinel.yaml` schema — the field is the same; only its
parsing semantics change. Operators with non-trivial `additional_args` should
run `sentinel config validate` after upgrading; the validator now rejects
malformed input at load time so misconfigurations surface immediately rather
than at the next scheduled run.

## Implementation checklist

- [x] Add `github.com/google/shlex` to `go.mod` / `go.sum`.
- [x] Rewrite `ParseAdditionalArgs` in `internal/backup/args.go` with new
      `(string) ([]string, error)` signature and two exported sentinels.
- [x] Update all 7 backup call sites (`pkg/backup/{pg,mysql,mariadb,mongo}_dump/`).
- [x] Delete `parseCLIArgs` helpers in 4 restore packages and route through `backup.ParseAdditionalArgs`.
- [x] Add config-load-time validation in `ValidateRestoreJob` (`internal/config/restore_types.go`).
- [x] Add table-driven tests in `internal/backup/args_test.go` covering the
      PRD acceptance matrix + edge cases.
- [x] Add validator tests in `internal/config/restore_config_test.go`.
- [x] Document operator-facing quoting in `docs/runbooks/additional-args.md`.
- [x] Release notes entry calling out the breaking quote-stripping change.

## References

- PRD 16 — Replace Naive Args Regex with `shlex` (`.prds/16-shlex-args-parser.md`)
- Spec 023 — `specs/023-shlex-args-parser/spec.md`
- ADR 0003 — Remove `internal/utils/` (parser lives where its consumers live)
- `github.com/google/shlex` — https://github.com/google/shlex
