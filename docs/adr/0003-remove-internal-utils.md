# ADR 0003 — Remove `internal/utils/`; redistribute by domain

- **Status**: Accepted
- **Date**: 2026-05-17
- **Deciders**: Denis AKPAGNONITE
- **Tags**: layering, refactor

## Context

`internal/utils/` currently holds:

- `default.go` (~668 B) — default-value helpers for config loading.
- `file.go` (~4.5 KB) — filesystem helpers (path manipulation, existence checks).
- `time.go` (~2.8 KB) — time formatting helpers.
- `scheduled_output.go` (~1.9 KB) — output formatting for scheduled-job results.

These files share no domain. They were grouped under `utils/` because they did not obviously belong anywhere when first written. The package is imported from `internal/config/`, `internal/cli/`, `internal/scheduler/`, and `internal/adapters/monitor/`, creating an implicit dependency on a junk drawer.

Under the hexagonal layout adopted in ADR 0001, every package must answer the question "which port or which domain do I serve?". `internal/utils/` cannot.

## Decision drivers

- Hexagonal layering (ADR 0001) forbids junk-drawer packages: every file must justify its location by its domain or port role.
- Each file in `utils/` has a clear single owner already; the grouping is accidental.
- Cross-package shared utilities discourage strong package boundaries: any function added to `utils/` becomes available everywhere with no review.

## Options considered

### Option A — Keep `internal/utils/` but rename it

Rename to `internal/common/` or `internal/shared/`. Same problem with a different label.

**Cons**
- Cosmetic; the package still has no domain.

### Option B — Split `internal/utils/` into thematic sub-packages

Create `internal/utils/fs/`, `internal/utils/timefmt/`, `internal/utils/defaults/`. Multiple small packages all under `utils/`.

**Pros**
- Each sub-package has a clearer purpose.

**Cons**
- Still groups unrelated code under a top-level `utils/`. Solves nothing structurally.

### Option C — Redistribute each file to the package whose domain it serves

Move each file to a package whose existing domain it already supports. Delete `internal/utils/`.

**Pros**
- Every file is owned by a domain.
- Function visibility tightens: helpers become package-private to the package that actually needs them.
- Eliminates one cross-cutting package.

**Cons**
- One-time churn across importers.
- A few helpers may be used by more than one package and need to be duplicated or hoisted to a more specific shared package (e.g. `internal/sanitize`, `internal/domain/manifest`).

## Decision

Sentinel removes `internal/utils/`. Each file moves to the package whose domain it serves:

- `default.go` → `internal/config/` (default-value helpers for config loading).
- `time.go` → `internal/domain/schedule/` (cron timestamp formatting).
- `scheduled_output.go` → `internal/scheduler/` (job-result rendering for the scheduler driver).
- `file.go` → split: filesystem-existence helpers go to `internal/adapters/storage/local/`; path-manipulation helpers that are pure logic move into a new minimal `internal/domain/fs/` only if more than one domain package needs them. If only one needs them, they move there directly.

Helpers used by exactly one caller become unexported in the consuming package. Helpers shared by two callers are inlined or kept exported in the most specific shared package; no new top-level `internal/util*` package is introduced.

## Consequences

### Positive
- One fewer cross-cutting package; the dependency graph simplifies.
- Helpers can no longer be silently re-used by unrelated code: a function under `internal/config/` is in the config domain, full stop.
- Future contributors cannot "add it to utils" as a default; they must pick a domain.

### Negative
- Migration churn: ~4 files move, ~6 importers update.
- A small amount of helper code that was previously shared may end up duplicated across two packages. Acceptable: explicit duplication beats accidental coupling.

### Neutral / to watch
- Whether a genuine cross-cutting need emerges that is *not* `sanitize`-shaped. If so, a new ADR proposes the specific shared package by name.

## Compatibility & migration

No effect on the binary, the CLI, the YAML config, the on-disk backup layout, the manifest, or the encryption envelope. Source-layout change only.

Migration is one PR per moved file plus a final PR deleting `internal/utils/`.

## Implementation checklist

- [ ] Move `internal/utils/default.go` to `internal/config/`; unexport or rename if name collides.
- [ ] Move `internal/utils/time.go` to `internal/domain/schedule/`.
- [ ] Move `internal/utils/scheduled_output.go` to `internal/scheduler/`.
- [ ] Move `internal/utils/file.go` contents: filesystem-existence helpers to `internal/adapters/storage/local/`; pure path helpers to wherever they have a single consumer, or to a new `internal/domain/fs/` only if shared.
- [ ] Update every import of `internal/utils` across the tree.
- [ ] Delete `internal/utils/`.
- [ ] Update `CLAUDE.md` so the package list no longer mentions `utils`.

## References

- ADR 0001 — Adopt hexagonal architecture.
- `docs/architecture/project-layout.md` §2 (target layout, no `utils/`).
