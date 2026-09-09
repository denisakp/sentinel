# Sentinel Roadmap

This is a curated, high-level view of where Sentinel is and where it's headed.
For full technical detail on any shipped change, see [`release-notes.md`](../../release-notes.md).

---

## Shipped

**Latest tagged release: v1.4.0** (August 5, 2026).

Since v1.3.0, Sentinel has shipped:

- **Supply-chain signing** for released binaries: cosign keyless signing of
  `checksums.txt`, and SLSA build provenance alongside it
- **A guided re-encryption path** (`sentinel security reencrypt`) for
  operators migrating backups off the legacy (pre-v2) encryption envelope or
  rotating keys
- **Optional at-rest encryption for DB-credential secrets files** (the
  MySQL/MariaDB defaults file and the MongoDB secrets file), decrypted only
  in memory at config-load time, plus the credential-source flexibility work
  it builds on: my.cnf-based MySQL/MariaDB credentials, a Sentinel-native
  MongoDB secrets file, and environment-variable indirection for both files'
  paths (containerized/Kubernetes deployments)

Since v1.1.1 (restore observability — real execution history for
`sentinel restore history`, normalized status values), Sentinel has also
shipped:

- Backup compression (gzip/zstd) across all supported engines
- Grandfather-Father-Son (GFS) retention alongside flat keep-last/keep-days rules
- A repository-wide integrity sweep (`backup verify --all`) plus a scheduled,
  cron-driven version with audit trail and failure notifications
- `sentinel backup diff` for comparing two backups' metadata and catching
  silent security regressions (e.g. encryption silently disabled)
- `sentinel repair` for reconciling backup repository state after a crash or
  manual artifact deletion
- Cross-platform prebuilt binary distribution (linux/macOS/Windows,
  amd64/arm64) with SHA-256 checksums, published on every tagged release
- Post-upload integrity verification (re-download + re-hash) for remote
  storage backends
- A published, reproducible performance benchmark suite

A security advisory regarding the encryption envelope used by v1.1.1 and
earlier is documented in [`release-notes.md`](../../release-notes.md); the
current envelope format (v2) closes it.

---

## Next

**Correctness remediation.** Writing the documentation site surfaced 73 distinct
defects across the CLI, the config plumbing, retention, restore planning and the
storage backends. Every one was confirmed against the code on 2026-08-06; none
was a false alarm. They are tracked as 54 `bug` issues labelled `found-by-docs`
(#136 to #197, tracking issue #176), and the remediation plan is 15 grouped work
items, `I00` to `I14`, in
[`docs/audit/documentation-batch-defects/`](../audit/documentation-batch-defects/README.md).

The plan groups by root cause rather than by issue number, because six patterns
explain most of the batch:

- commands that bypass the shared config resolver and reinvent their own path
  and validation
- success and failure that are indistinguishable, with exit 0 on failure and
  output written to stderr
- config keys that are parsed and validated but never threaded to a consumer
- behaviour wired on one axis and forgotten on the other, for example dump but
  not restore, or `--all` but not the single-job path
- a validator that accepts what the runtime then rejects
- artifact identity lost between the dump and the manifest

`I00` and `I01` land first and block the rest. The end-to-end harness cannot
currently tell a fix from a no-op: `scripts/e2e.sh` runs `go test ./...`, which
silently skips every `//go:build integration` file; every scenario under
`tests/integration/incremental/` is a `t.Skip` scaffold that reads as coverage;
and the default assertion is `assert_exit_ok`, under which a command that
validates a key, ignores it, and exits 0 is indistinguishable from success. That
is the dominant shape of the whole batch. `I01` then makes every later failure
visible.

Two commitments govern this work. PITR is a required feature and is not to be
withdrawn or descoped; if the recoverable-window plumbing outgrows `I09` it gets
its own spec. And each fix ships its end-to-end assertion and its documentation
update in the same pull request, since deferring coverage is how the batch
happened.

Where a defect affects a reader today, the documentation site already says so
and names the issue number. `grep -rl "<issue>" website/docs/` finds every page
that has to change when it is fixed.

## Feature proposals

No directional feature items are currently queued beyond the remediation above.
Have something in mind? See "Propose something" below.

---

## Propose something

Have an idea for a capability or milestone? Open a
[roadmap proposal](../../.github/ISSUE_TEMPLATE/3-roadmap-proposal.md) issue.
