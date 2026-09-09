# PRD I03 — Secret redaction + MongoDB TLS wiring

**Severity:** High (credential disclosure; a security control that is configured but never applied)
**Status:** Open
**Issues:** #156, #189
**Area:** `internal/adapters/dump/mongo/`, `internal/adapters/restore/mongo/`, `internal/cli/backup.go`,
`internal/config/validator.go`

## Problem

Two independent security defects sharing one shape: the control was implemented on the dump-side
happy path and never extended to the error paths or to the restore side.

### #156 — MongoDB URI with password printed unredacted

`internal/adapters/dump/mongo/mongo_dump.go:105` interpolates a raw `mongodb://` URI into an error
string via its own `strings.Join(args, ...)`. Nine lines earlier, at line 96, the same file calls
`sanitize.RedactStderr` correctly. The pattern is right there.

`internal/adapters/restore/mongo/mongo_restore.go:163` has **no `sanitize` import at all** and
interpolates `ra.URI` directly.

Third path, lower severity, confirmed: `internal/adapters/restore/mongo/oplog_replay.go:52` embeds
raw `mongorestore` stderr with no `RedactStderr` call.

### #189 — MongoDB TLS configuration is never applied

`internal/adapters/dump/mongo/args_builder.go:20` declares `DumpMongoArgs.TLS`. **Nothing sets it.**
Both construction sites were checked — `internal/cli/backup.go:225` and
`internal/adapters/dump/mongo/args_factory.go`'s `ArgsFactory.BuildDumpArgs` — and both omit the
field.

`internal/adapters/restore/mongo/args_builder.go:12`'s `PrepareTLS` has **zero callers repo-wide**;
`grep -rn "PrepareTLS(" . --include=*.go` returns only its own definition.

And the part that makes it worse: `internal/config/validator.go:113-129` warns only when
`job.TLS == nil`. **Configuring TLS suppresses the warning that would tell you TLS is not being
applied.** A user who does the right thing gets less signal than one who does nothing.

## Root cause

Pattern **D**: wired on one axis, forgotten on the other. Dump got redaction, restore did not. The
TLS struct field was declared alongside the other engines' and never threaded from config through
the args factory.

## Fix

1. `mongo_dump.go:105` — wrap args in `sanitize.RedactArgs` before joining.
2. `mongo_restore.go:163` — run `ra.URI` through `sanitize` before interpolating.
3. `oplog_replay.go:52` — add `sanitize.RedactStderr`.
4. Thread `job.TLS` through `ArgsFactory.BuildDumpArgs` and `backup.go`'s CLI path into
   `DumpMongoArgs.TLS`.
5. Call `PrepareTLS` on the restore path.
6. `validator.go:113-129` — until the wiring lands, the validator must warn **"tls configured but
   not applied"** rather than falling silent on presence. After the wiring lands, the warning
   reverts to the `nil` case.

Item 6 ships **first and separately** if items 4–5 take longer than a release: a user configuring
Mongo TLS today believes they have transport encryption and does not.

## Cross-reference

`internal/adapters/restore/mongo/args_builder.go` retains a documented cross-axis import of
`internal/adapters/dump/mongo` for `PrepareMongoTLS` (CLAUDE.md, "Import-cycle rule"). Wiring #189
touches that boundary — verify `make lint` still passes the `adapter-restore-axis` depguard group.

## Definition of done

- **Code:** the six fixes above.
- **e2e:**
  - Trigger a Mongo dump and a Mongo restore connect failure against a URI carrying credentials;
    assert the error string does not contain the password substring. No live DB needed — a bad host
    is enough to reach the error path.
  - Assert the validator still emits a warning when `tls:` is present but unwired.
  - **NEEDS_LIVE** for "the connection is actually TLS": requires a TLS-enabled `mongod`. The
    warning half and the redaction half are provable without one.
- **Docs:** `website/docs/tutorials/mongodb/restore.md`, `website/docs/tutorials/mongodb/index.md`.
  #189 has no doc pages today — **add one**: the MongoDB TLS section currently describes a control
  that does not exist.
- **Security review:** run `/security-review` on the diff. The `crypto-reviewer` agent covers
  `internal/sanitize/` and credential-interpolating arg builders.
