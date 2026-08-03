# ADR 0004 — Storage backend port lives in `internal/ports`; dispatcher stays as a thin adapter

- **Status**: Accepted
- **Date**: 2026-05-17
- **Deciders**: Denis AKPAGNONITE
- **Tags**: storage, layering, ports

## Context

Today, the `StorageBackend` interface lives in `internal/storage/backend.go` and is implemented by sibling sub-packages: `internal/storage/local`, `internal/storage/sentinel_s3`, `internal/storage/gcs`, `internal/storage/gdrive`, `internal/storage/azure`. A dispatcher in `internal/storage/storage.go` imports every backend and routes requests by configuration.

This works but creates a coupling: `internal/storage` is both the interface owner and the multiplexer. A second rule had to be added by hand — "sub-packages MUST NOT import `internal/storage`" — to keep cycles out. The rule is enforced socially, not by the type system.

ADR 0001 adopts hexagonal layering. The question is where the interface (the port) should live and what to do with the dispatcher.

## Decision drivers

- The dependency rule in ADR 0001 requires ports to be importable by both the domain and any adapter, without either side importing the other.
- Existing backend code must not need rewrites beyond the move; the change should be mechanical.
- The dispatcher is currently the only place that knows the full set of available backends. Moving the backend list (e.g. to a registry pattern) is desirable but out of scope for this ADR.
- New backends must be addable with a single sub-tree under `internal/adapters/storage/` plus one wiring edit.

## Options considered

### Option A — Keep the interface in `internal/storage/backend.go`

Leave the interface where it is; move only the sub-packages.

**Pros**
- Smaller diff.

**Cons**
- The domain (`internal/domain/backup`) would have to import `internal/storage`, which also contains the dispatcher and therefore transitively every backend SDK. Defeats the point of the port.

### Option B — Move the interface to `internal/ports/storage.go`

The port is interface-only. Domain imports `internal/ports/storage`. Adapters under `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/` implement the port. A thin dispatcher at `internal/adapters/storage/storage.go` selects the adapter at wiring time based on config.

**Pros**
- Domain depends on an interface file with zero SDK transitive imports.
- Adapter sub-packages no longer need the "do not import parent" social rule; the type system enforces it (the parent contains nothing the children need).
- Matches the layout defined in `docs/architecture/project-layout.md` §2.

**Cons**
- The dispatcher still imports siblings (it is the multiplexer). This is the one allowed inter-adapter import; called out explicitly in `project-layout.md` §3.

### Option C — Replace the dispatcher with a registry

Each backend sub-package registers itself in an `init()` against a registry exposed by the port package. The dispatcher disappears; wiring chooses by name.

**Pros**
- No sibling imports anywhere.
- Adding a backend is purely additive.

**Cons**
- `init()`-time registration hides the active set of backends from the type system and from `go vet`.
- A future build that omits a backend (e.g. via build tags) must still satisfy the registry contract.
- Larger change than needed for the immediate hexagonal migration.

## Decision

Sentinel places the storage port at `internal/ports/storage.go` and keeps the dispatcher as a thin adapter at `internal/adapters/storage/storage.go`.

- `internal/ports/storage.go` defines `StorageBackend` and the minimal value types (e.g. `BackupObject`) it returns. No SDK imports.
- `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/` each implement the port. They depend on their cloud SDKs and on `internal/ports/storage`.
- `internal/adapters/storage/storage.go` is the dispatcher: it imports the siblings and exposes a `Resolve(cfg) StorageBackend` function called by driving adapters (`internal/cli`, `internal/scheduler`).
- The sibling-import exception is documented once in `project-layout.md` §3 and applies to this dispatcher only.

A future registry-based design is not blocked. If sibling imports become a recurring problem (multiple dispatchers, conditional builds), Option C may supersede this ADR.

## Consequences

### Positive
- The domain depends on a 1-file interface package; no transitive SDK weight.
- Backends are pluggable in their own sub-tree under `internal/adapters/storage/`.
- The ad-hoc "do not import parent" rule in `internal/storage/` becomes unnecessary; it is replaced by the global hexagonal rule.
- Existing tests in `internal/storage/backend_test.go` and `internal/storage/validation.go` move with the dispatcher and continue to apply.

### Negative
- The dispatcher is the one allowed sibling-importer in the whole adapter tree; new contributors may not realise this is a deliberate exception.
- Adding a new backend still requires editing the dispatcher (one line). Acceptable until Option C is justified.

### Neutral / to watch
- Whether the dispatcher grows responsibilities beyond resolution (validation, retry composition). If so, those responsibilities move to a driving adapter (`internal/cli`) or to the domain.

## Compatibility & migration

No effect on the binary, the CLI, the YAML config, the on-disk backup layout, the manifest, or the encryption envelope.

`internal/storage/types/` (today's home for shared types) merges into `internal/ports/storage.go` for the interface types and `internal/adapters/storage/types/` for adapter-private types.

## Implementation checklist

- [ ] Create `internal/ports/storage.go` with the `StorageBackend` interface and its value types.
- [ ] Move `internal/storage/{local,sentinel_s3,gcs,gdrive,azure}/` to `internal/adapters/storage/{local,s3,gcs,gdrive,azure}/`; update imports to depend on `internal/ports/storage`.
- [ ] Move `internal/storage/storage.go` (the dispatcher) to `internal/adapters/storage/storage.go`; keep its sibling imports.
- [ ] Move `internal/storage/types/` either into `internal/ports/storage.go` (public types) or `internal/adapters/storage/types/` (adapter-private types).
- [ ] Move `internal/storage/backend_test.go` and `internal/storage/validation.go` next to the relevant new location.
- [ ] Delete `internal/storage/` once empty.
- [ ] Update `CLAUDE.md` to point to the new paths and remove the "sub-packages must not import `internal/storage`" rule (now redundant).

## References

- ADR 0001 — Adopt hexagonal architecture (defines the dependency rule).
- ADR 0002 — Remove `pkg/` (parallel migration).
- `docs/architecture/project-layout.md` §3 (sibling-import exception for the dispatcher).
