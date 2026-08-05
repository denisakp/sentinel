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

No directional items are currently queued. Have something in mind? See
"Propose something" below.

---

## Propose something

Have an idea for a capability or milestone? Open a
[roadmap proposal](../../.github/ISSUE_TEMPLATE/3-roadmap-proposal.md) issue.
