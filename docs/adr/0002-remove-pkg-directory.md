# ADR 0002 — Remove the `pkg/` directory; everything ships under `internal/`

- **Status**: Accepted
- **Date**: 2026-05-17
- **Deciders**: Denis AKPAGNONITE
- **Tags**: layering, packaging

## Context

Sentinel currently exposes DB-engine specifics under `pkg/backup/<engine>_dump/` and `pkg/restore/<engine>_restore/`. In Go, code under `pkg/` is importable by any external module; code under `internal/` is not.

No external consumer imports Sentinel as a library today. The packages under `pkg/` are only called by `internal/cli/` and `internal/scheduler/`. They expose engine-specific argument builders, output parsers, and replay helpers that are tightly coupled to Sentinel's internal types and have no documented stability guarantee.

ADR 0001 adopts hexagonal layering and moves these packages under `internal/adapters/{dump,restore}/`. The question is whether `pkg/` should stay as an empty placeholder for future public APIs, or be removed entirely.

## Decision drivers

- Nothing currently consumes Sentinel as a library; there is no public Go API contract to honour.
- The `pkg/` packages are not API-stable — argument builders change shape whenever an engine adds a flag.
- A public surface without a stability guarantee creates accidental contracts: external code may import what was never meant to be public.
- Promotion to `pkg/` is reversible; demotion from `pkg/` to `internal/` is a breaking change.

## Options considered

### Option A — Keep `pkg/` as an empty directory marker

Move current contents to `internal/adapters/`, leave `pkg/` empty with a README explaining the promotion policy.

**Pros**
- Visible signal that public-API promotion is anticipated.

**Cons**
- Empty directories are not idiomatic Go.
- README rot: the note explains a policy that nothing currently enforces.

### Option B — Remove `pkg/` entirely; promotion requires an ADR

Move everything to `internal/adapters/`. Delete `pkg/`. When a real public-API need arises, write an ADR declaring the contract and stability tier, then create `pkg/<name>/`.

**Pros**
- Honest: the surface matches the (non-existent) public contract.
- Forces a deliberate, reviewed decision before anything becomes public.
- Removes a directory contributors otherwise feel obliged to populate.

**Cons**
- Loses the visual cue that some code is "more public than other code". Mitigated by ADR-gated promotion.

## Decision

Sentinel removes the `pkg/` directory. All production code ships under `internal/`. The contents of `pkg/backup/*` move to `internal/adapters/dump/*`; the contents of `pkg/restore/*` move to `internal/adapters/restore/*` as specified in ADR 0001 and `docs/architecture/project-layout.md` §8.

A future need to expose a Go API follows this path:

1. Write an ADR naming the package, the consumers, and the stability tier (`experimental`, `stable`, `frozen`).
2. Move the relevant sub-tree to `pkg/<name>/`.
3. Add a package doc comment declaring the tier and the compatibility window.

Until then, `pkg/` does not exist.

## Consequences

### Positive
- One rule for the whole tree: production code lives in `internal/`.
- No accidental public contracts on engine-specific argument builders.
- Lower contributor confusion about where new code goes.

### Negative
- Future library consumers face a small upgrade path: the first promotion to `pkg/` will require its own ADR.

### Neutral / to watch
- Whether any tooling (linters, code generators) hard-codes the path `pkg/`. None known.

## Compatibility & migration

This is a source-layout change only. No effect on the binary, the CLI, the YAML config, the on-disk backup layout, the manifest, or the encryption envelope. No public Go module imports Sentinel today, so no external module needs an update.

Internal import paths change; this is handled by the per-package migration PRs listed in ADR 0001's implementation checklist.

## Implementation checklist

- [ ] Move `pkg/backup/{pg,mysql,mariadb,mongo}_dump/` to `internal/adapters/dump/{pg,mysql,mariadb,mongo}/`.
- [ ] Move `pkg/backup/{mysqlbinlog,pg_combine}/` to `internal/adapters/dump/{mysqlbinlog,pg_combine}/`.
- [ ] Move `pkg/restore/{pg,mysql,mariadb,mongo}_restore/` to `internal/adapters/restore/{pg,mysql,mariadb,mongo}/`.
- [ ] Update every import path that referenced `pkg/...`.
- [ ] Delete the now-empty `pkg/` directory.
- [ ] Update `CLAUDE.md` so the layout description matches reality.

## References

- ADR 0001 — Adopt hexagonal architecture.
- `docs/architecture/project-layout.md` §7 (public vs internal) and §8 (migration table).
- Go project layout guidance: <https://go.dev/doc/modules/layout>.
