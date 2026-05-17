# ADR 0005 — Manifest v1 format and home in `internal/domain/manifest`

- **Status**: Accepted
- **Date**: 2026-05-17
- **Deciders**: Denis AKPAGNONITE
- **Tags**: manifest, integrity, format, domain

## Context

Every Sentinel backup is accompanied by a manifest written as `<backup-filename>.manifest.json` alongside the artefact. The manifest is the single source of truth for integrity (SHA-256), restore metadata (advanced restore capabilities, incremental lineage, PITR boundaries), and encryption parameters when the backup is encrypted.

The current implementation lives at `internal/manifest/` with two files:

- `internal/manifest/types.go` — `BackupManifest`, `HashInfo`, `EncryptionInfo`, `AdvancedRestoreMetadata`, `PostgresRecoveryMetadata`, `IncrementalLineageMetadata`.
- `internal/manifest/manifest.go` — `WriteManifest`, `ReadManifest`, `LoadRestoreManifest`, `ValidateIncrementalLineageContract`, `VerifyBackupHash`.

`VerifyBackupHash` accepts only `sha256` (case-insensitive). Missing manifests raise `ErrNoManifest`, which callers treat as a pre-v1.1 backup and proceed with a WARN.

The format is JSON with `json.MarshalIndent(m, "", "  ")` and file mode `0600`. There is no explicit format version field in the manifest payload.

Two questions this ADR resolves:

1. Where does the manifest package belong under the hexagonal layout?
2. What is the v1 manifest format, and what does v1 promise (vs. what is open for v2)?

## Decision drivers

- The manifest is the **integrity contract** of the product. Restorability of older backups depends on it being readable forever.
- Hash verification, lineage validation, and encryption-metadata reading are pure logic — no I/O beyond opening the JSON file. They belong in the domain layer.
- The current JSON shape is already shipped in v1.1+ backups and cannot be broken without a migration story.
- A future v2 (per-chunk digests, Merkle tree, alternative hash) is foreseeable and should be addable without breaking v1 readers.

## Options considered

### Option A — Keep the package at `internal/manifest/` outside the domain tree

Treat it as a shared utility.

**Cons**
- The domain (`internal/domain/backup`, `internal/domain/restore`) is the primary consumer; placing it outside the domain forces an extra layer of indirection.
- "Shared utility" is the same shape as `internal/utils/`, which ADR 0003 removes.

### Option B — Move to `internal/domain/manifest/`

Manifest logic is pure; it has no SDK dependencies and no I/O beyond `os.ReadFile`/`os.WriteFile`. It expresses an integrity invariant — a domain concept.

**Pros**
- Aligns with ADR 0001's dependency rule.
- The domain owns its own integrity contract; adapters only stream bytes.

**Cons**
- `os.ReadFile`/`os.WriteFile` are arguably I/O. Acceptable: they operate on the local filesystem path provided by the caller, and the domain does not choose the path. If stricter purity is needed later, the read/write functions can move to an adapter and the package keeps only types and validators.

### Option C — Add an explicit `manifest_version` field now, defaulting to `1`

Force every reader to check version before parsing.

**Pros**
- Forward-compatible signal.

**Cons**
- The current v1.1+ backups in the wild do **not** carry this field. Adding it as required would break them. Adding it as optional changes nothing today.

## Decision

Sentinel places the manifest package at **`internal/domain/manifest/`** and defines the v1 format as follows:

- **Filename**: `<backup-filename>.manifest.json`, written alongside the backup artefact.
- **Encoding**: JSON, two-space indented (`json.MarshalIndent(m, "", "  ")`).
- **File mode**: `0600`.
- **Schema**: the `BackupManifest` struct as currently defined in `internal/manifest/types.go`, comprising `backup_id`, `database`, `database_type`, `created_at` (RFC 3339 UTC), `size_bytes`, `hash`, optional `encryption`, optional `advanced_restore`.
- **Hash**: only `sha256` is supported in v1. `hash.algorithm` and `hash.value` are required. `hash.plaintext_value` is optional and used when the artefact is encrypted (it then carries the pre-encryption digest).
- **Missing manifest**: a backup without a manifest is treated as pre-v1.1 and restored with a WARN, via `ErrNoManifest`. This behaviour is part of the v1 contract.
- **Unknown fields**: tolerated on read (`json.Unmarshal` skips them). v2 readers may add fields without breaking v1 readers.
- **Version field**: v1 does **not** carry an explicit `manifest_version`. v2 introduces one with the rule that absence means v1. This keeps existing backups readable indefinitely.

The package exposes:

- `BackupManifest` and friend types (data).
- `WriteManifest(path, *BackupManifest) error`.
- `ReadManifest(path) (*BackupManifest, error)` returning `ErrNoManifest` for missing files.
- `VerifyBackupHash(path, algorithm, expected) error` (SHA-256 only).
- `ValidateIncrementalLineageContract(*BackupManifest) error`.

I/O is limited to reading and writing a single JSON file at a path the caller provides. The package never opens network connections, never calls any cloud SDK, and never spawns processes.

## Consequences

### Positive
- Manifest format is now an ADR-governed contract; any future change is documented.
- Domain ownership: backup/restore use cases call into `internal/domain/manifest/` without reaching into adapters.
- v1.1+ backups remain readable indefinitely; the absence of a version field is the version signal.

### Negative
- The "no version field in v1" rule means v2 introduces a parsing branch. Acceptable: detecting "v1 = no field, v2 = field present" is trivial.
- Tolerating unknown fields on read means malformed manifests fail later (at hash verify) rather than at parse. Acceptable: integrity is checked at restore time anyway.

### Neutral / to watch
- Whether `WriteManifest` / `ReadManifest` should move to an adapter port (`internal/ports/manifest_io`) to keep `internal/domain/manifest/` strictly pure. Today they stay; revisit if a non-filesystem manifest store is introduced (e.g. embedding the manifest in the storage backend's object metadata).
- Whether `plaintext_value` in `HashInfo` should become required when `Encryption` is present. Tracked separately; v1 keeps it optional.

## Compatibility & migration

- **On-disk format**: v1 is what the codebase already writes since v1.1. No migration is required.
- **Restorability**: every backup produced by v1.1, v1.2, v1.3 remains restorable by current code. ADR 0001 (layout migration) does not change the format.
- **Sentinel.yaml**: unaffected.
- **CLI**: `sentinel backup verify` keeps its current contract.

A future ADR introducing manifest v2 must:

1. Add a `manifest_version` field (absence ⇒ v1).
2. Provide a dual-reader for at least one major release.
3. Document the v1 → v2 conversion path (re-read v1, re-emit v2) for `sentinel backup verify --upgrade-manifest` or equivalent.

## Implementation checklist

- [ ] Move `internal/manifest/` to `internal/domain/manifest/`.
- [ ] Update every importer (`internal/cli/backup_verify.go`, `internal/cli/backup.go`, `internal/cli/restore.go`, `internal/scheduler/restore_executor.go`).
- [ ] Add a `Format` package doc comment in `internal/domain/manifest/manifest.go` summarising the v1 contract from this ADR.
- [ ] Add a regression test that reads a v1.1 manifest fixture and asserts every field of `BackupManifest` is parsed (catches accidental field renames).
- [ ] Cross-reference this ADR from `docs/testing-guide.md` in the integrity section.

## References

- ADR 0001 — Adopt hexagonal architecture.
- ADR 0006 — Encryption envelope v1 (companion: encryption parameters live inside the manifest's `encryption` block).
- `internal/manifest/types.go`, `internal/manifest/manifest.go` (source of the v1 shape).
- `docs/testing-guide.md` — integrity, encryption sections.
