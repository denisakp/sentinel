# ADR 0001 — Adopt hexagonal (ports & adapters) architecture

- **Status**: Accepted
- **Date**: 2026-05-17
- **Deciders**: Denis AKPAGNONITE
- **Tags**: architecture, layering, cli, scheduler, storage, engine, notifier

## Context

Sentinel is a single-binary Go 1.24 CLI for automated database backup, restore, and disaster recovery. The codebase has grown organically from a layered shape:

- `internal/cli/` holds Cobra commands **and** a large amount of orchestration logic (`internal/cli/backup.go` is 1475 lines; `internal/cli/restore.go` is 619 lines).
- `internal/storage/storage.go` is a dispatcher that imports every backend sub-package (`local`, `sentinel_s3`, `gcs`, `gdrive`, `azure`); a one-directional import rule was bolted on to keep cycles out (`internal/storage/types/` exists solely for this).
- DB-engine specifics live under `pkg/backup/<engine>_dump/` and `pkg/restore/<engine>_restore/`, exposed as if they were public API even though no external consumer imports them.
- A second in-process driver (`internal/scheduler/`) calls into the same use cases as the CLI, but does so by reaching into CLI helpers (`internal/scheduler/restore_integration.go` is 18 KB and partly duplicates `internal/cli/restore.go`).
- `internal/utils/` holds a small junk drawer (`default.go`, `file.go`, `time.go`) with no clear owner.

The product roadmap continues to multiply plug-in points: new storage backends, new DB engines (incremental + PITR variants), new notifier channels, and likely a future daemon mode. Each addition currently touches both the dispatcher and the CLI command file, and the orchestration-vs-I/O boundary is invisible at the package level.

## Decision drivers

- Multiple **storage backends** (local, S3, GCS, GDrive, Azure) must be swappable behind one contract.
- Multiple **DB engines** (PostgreSQL, MySQL, MariaDB, MongoDB) must implement the same dump/restore contract.
- Multiple **notifier channels** (Slack, Discord, email, webhook) must be swappable behind one delivery contract.
- Two **drivers** of the same use cases must coexist without duplication: the interactive CLI and the in-process scheduler.
- The product promises **integrity and verifiability** (manifest, encryption envelope, chain validation). The logic that enforces these promises must be unit-testable without spinning up containers or cloud SDKs.
- Sentinel ships as **one static binary**; runtime plug-in discovery is out of scope.
- The current layout already leaks SDK imports into `internal/cli/*` and forces an ad-hoc import-cycle rule on `internal/storage/`.

## Options considered

### Option A — Keep the current layered structure

Continue with `internal/cli/` as the orchestration site, `internal/storage/` as a dispatcher importing siblings, and `pkg/<engine>_*` for DB-specific code. Codify the existing import-cycle rule and stop there.

**Pros**
- Zero migration cost.
- Familiar to any Go contributor; no extra vocabulary.

**Cons**
- The CLI package keeps growing the god-files (`backup.go`, `restore.go`); domain logic cannot be unit-tested without dragging in Cobra and config parsing.
- The scheduler driver keeps duplicating CLI helpers (`restore_integration.go` already mirrors `restore.go`).
- Adding a new engine or backend touches the dispatcher, the CLI command, and the scheduler integration — three edits for one capability.
- The "no sub-package imports `internal/storage`" rule is invisible to the type system and re-discovered every onboarding.

### Option B — Adopt hexagonal (ports & adapters) layering

Split `internal/` into four roles:

- `internal/domain/<use-case>/` — pure business logic, depends only on `internal/ports` and the standard library.
- `internal/ports/` — Go interfaces only; the contracts the domain depends on.
- `internal/adapters/<port-impl>/` — driven adapters that implement ports and hold third-party SDK dependencies.
- `internal/cli/`, `internal/scheduler/`, `internal/config/` — driving adapters that wire ports to adapters and call the domain.

`pkg/backup/*` and `pkg/restore/*` move under `internal/adapters/{dump,restore}/`. `internal/utils/` is removed; its contents are redistributed to the package whose domain they actually serve.

**Pros**
- Domain logic is unit-testable with in-memory fakes of every port.
- Adding a backend, engine, or notifier is a self-contained adapter under `internal/adapters/`; no edit to the domain.
- CLI and scheduler share one set of use-case calls; the duplication between `internal/cli/restore.go` and `internal/scheduler/restore_integration.go` disappears.
- The dependency rule is a single sentence (`domain → ports ← adapters`) instead of an ad-hoc cycle ban per directory.
- Aligns with the existing `internal/storage/types/` pattern, which is already a partial port.

**Cons**
- One-time migration cost: every storage backend, dump/restore adapter, notifier, lock, crypto, and monitor implementation moves; orchestration is extracted out of `internal/cli/backup.go` and `internal/scheduler/restore_integration.go`.
- More packages (one port file, one adapter sub-tree per capability) — small indirection cost for small adapters.
- Contributors who have never seen the pattern need a 10-minute orientation; mitigated by `docs/architecture/vision.md`.

### Option C — Clean-architecture style with explicit use-case structs

A stricter variant of Option B: each use case is an exported struct in `internal/usecase/` with a constructor taking every port; the domain holds only entities.

**Pros**
- Maximally testable; mocking is mechanical.
- Use cases are first-class objects, easy to list and document.

**Cons**
- Heavier ceremony than the product needs: Sentinel has roughly a dozen use cases, not hundreds.
- Adds a layer (entities vs use cases) that does not earn its keep at this size.
- Diverges from idiomatic Go packaging more than Option B does.

## Decision

Sentinel adopts a **hexagonal (ports & adapters)** architecture. The layout is defined in `docs/architecture/project-layout.md` §2 and the dependency rule in §3. Specifically:

- `internal/domain/{backup,restore,retention,schedule,manifest}/` holds the pure business logic.
- `internal/ports/` holds the interfaces (`storage.go`, `dump.go`, `restore.go`, `notifier.go`, `recorder.go`, `crypto.go`, `lock.go`).
- `internal/adapters/{storage,dump,restore,notifier,crypto,monitor,lock,tls}/` holds the implementations.
- `internal/cli/`, `internal/scheduler/`, `internal/config/` are driving adapters that wire ports to adapters and call the domain.
- `pkg/backup/*` and `pkg/restore/*` move under `internal/adapters/{dump,restore}/`. `pkg/` becomes empty and is removed; promotion of any sub-tree back to `pkg/` requires a future ADR declaring a public stability contract.
- `internal/utils/` is removed. Its files are redistributed to the package whose domain they serve.

The dependency rule is: **`adapters → ports ← domain`**, with `cli`, `scheduler`, and `config` as driving adapters that may import `domain`, `ports`, and `adapters` (to wire). The domain may not import any driver, adapter, or config package. Adapter sub-packages may not import sibling adapter sub-packages.

## Consequences

### Positive
- Use cases in `internal/domain/` are testable with in-memory port fakes; no Cobra, no Docker, no cloud SDK in unit tests.
- Adding a storage backend, DB engine, or notifier channel is a single self-contained sub-tree under `internal/adapters/`. The domain is untouched.
- The CLI/scheduler duplication around restore (`internal/cli/restore.go` ↔ `internal/scheduler/restore_integration.go`) collapses into one call site against `internal/domain/restore`.
- The ad-hoc import-cycle rule on `internal/storage/` is replaced by a single global rule encoded in package layout.
- `internal/cli/backup.go` (1475 lines) shrinks to flag parsing + a call into `internal/domain/backup`, addressing the god-file problem tracked in `.prds/14-backup-cli-god-file-split.md`.

### Negative
- Migration touches every backend, every engine, every notifier, the crypto/lock/monitor/tls helpers, and the orchestration inside `internal/cli/` and `internal/scheduler/`. This is staged across multiple PRs; see the migration table in `project-layout.md` §8.
- More packages overall. Small adapters (e.g. `internal/adapters/lock`) carry a port file plus an implementation file where a single file existed before.
- The driving adapters (`cli`, `scheduler`, `config`) become wiring-heavy. Without a small DI helper, constructors grow long.
- New contributors must learn the role of each top-level directory before navigating freely; `docs/architecture/vision.md` is the entry point.

### Neutral / to watch
- Whether `internal/adapters/storage/storage.go` (the dispatcher) stays in adapters or becomes a registry in `internal/cli/wire.go`. Today it stays where it is; a follow-up ADR may revisit if adapter coupling reappears.
- Whether `internal/config/` is better classified as a driving adapter or as its own layer. Treated as a driving adapter for now; it parses YAML and produces domain inputs.
- The exact home of cross-cutting helpers (`internal/sanitize`, `internal/version`). These remain top-level under `internal/` and are importable from any layer.

## Compatibility & migration

This is a **source-layout change only**. No effect on:

- On-disk backup layout, manifest format, or encryption envelope.
- The restorability of any existing backup, including v1.0 through v1.3 artefacts.
- The `sentinel.yaml` schema.
- CLI commands, flags, exit codes, or stdout/stderr format.

Scripted consumers and operator runbooks are unaffected. Import paths internal to the repository change, but no public Go import path is published (nothing currently consumes Sentinel as a library).

The migration is staged. Each move listed in `docs/architecture/project-layout.md` §8 lands as a self-contained PR. Until the migration completes, the old paths remain functional and the new paths shadow them; tests run against both during the transition.

## Implementation checklist

- [ ] Add migration tracking issues for each row in `project-layout.md` §8.
- [ ] Move `internal/storage/backend.go` interface to `internal/ports/storage.go`; keep a type alias at the old path for the duration of the migration.
- [ ] Move `internal/storage/{local,sentinel_s3,gcs,gdrive,azure}/` to `internal/adapters/storage/*`.
- [ ] Move `pkg/backup/{pg,mysql,mariadb,mongo}_dump/` to `internal/adapters/dump/{pg,mysql,mariadb,mongo}/`.
- [ ] Move `pkg/backup/{mysqlbinlog,pg_combine}/` to `internal/adapters/dump/{mysqlbinlog,pg_combine}/`.
- [ ] Move `pkg/restore/{pg,mysql,mariadb,mongo}_restore/` to `internal/adapters/restore/{pg,mysql,mariadb,mongo}/`.
- [ ] Move `internal/notifier/{slack,discord,email,webhook}.go` to `internal/adapters/notifier/{slack,discord,email,webhook}/`; the dispatcher stays at `internal/adapters/notifier/dispatcher.go`.
- [x] Move `internal/crypto/` to `internal/adapters/crypto/` (spec 030, 2026-05-27); ports `Hasher`/`KeyProvider`/`EncryptWriter`/`DecryptReader` already exposed in `internal/ports/{crypto,encryption,hasher}.go` by spec 028.
- [x] Move `internal/lock/` to `internal/adapters/lock/` (spec 031, 2026-05-27); port `LockManager` already exposed in `internal/ports/lock.go` by spec 028.
- [ ] Move `internal/tls/`, `internal/monitor/` to `internal/adapters/{tls,monitor}/`; expose ports at `internal/ports/recorder.go`.
- [ ] Move `internal/manifest/` and `internal/retention/` to `internal/domain/{manifest,retention}/`.
- [ ] Extract orchestration from `internal/cli/backup.go` (1475 lines) into `internal/domain/backup/`; the CLI command becomes a thin flag-parser + domain call.
- [ ] Extract orchestration from `internal/scheduler/restore_integration.go` into `internal/domain/restore/`; the scheduler integration becomes a thin wrapper.
- [ ] Remove `internal/utils/`: `default.go` → `internal/config/`, `file.go` → `internal/adapters/storage/local/` or a new `internal/adapters/fs/`, `time.go` → `internal/domain/schedule/`.
- [ ] Add a `go vet`-checkable or CI lint that enforces the dependency rule in `project-layout.md` §3 (candidate: `go-arch-lint` or a small custom analyzer).
- [ ] Update `CLAUDE.md` to reflect the new package layout and import-cycle rule.
- [ ] Add follow-up ADRs: `pkg/` removal policy, `internal/utils/` removal, manifest format home, encryption envelope home, lock file format, scheduler concurrency model.

## References

- `docs/architecture/vision.md` — architectural style and quality goals.
- `docs/architecture/project-layout.md` — target layout, dependency rules, migration table (§8).
- `.prds/14-backup-cli-god-file-split.md` — the immediate trigger for extracting `internal/domain/backup/`.
- `internal/storage/types/` — existing partial port pattern that this ADR generalises.
- Alistair Cockburn, *Hexagonal Architecture* (2005).
