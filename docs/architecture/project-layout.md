# Sentinel — Project Layout

> Where each concept lives in the repository, and the rules that keep it that way.
> Complement: [vision.md](./vision.md) — the *why* and the architectural style.

This document describes the **target layout**. The current tree is being migrated toward it; sections marked *(migration in progress)* call out the gap.

---

## 1. Top-level tree

```
sentinel/
├── main.go                  # entry point — calls internal/cli.Execute()
├── go.mod  go.sum
├── Makefile                 # build, test, e2e, infra targets
├── README.md                # user-facing usage
├── CLAUDE.md                # agent conventions
├── CONTRIBUTING.md          # human conventions
├── SECURITY.md  CODE_OF_CONDUCT.md  LICENSE
├── release-notes.md
├── sonar-project.properties
│
├── internal/                # all production code (not importable by third parties)
├── docs/                    # vision, ADRs, runbooks, roadmap, testing guide
├── infra/                   # docker-compose stacks for local + e2e
├── scripts/                 # e2e.sh, wait-healthy.sh, dev helpers
├── tests/                   # cross-package integration + benchmarks (see §4)
├── prds/                    # feature PRDs (v1.0.md, v1.1.0.md, …)
├── .prds/                   # tactical issue tickets (one per bug / micro-feature)
├── .specify/  specs/        # Spec Kit artefacts (constitution, specs, plans, tasks)
└── .github/                 # CI, issue templates
```

There is no `cmd/` directory today. If a second binary appears (e.g. a daemon), introduce `cmd/sentinel/` and `cmd/sentineld/`.

There is no top-level `pkg/`. Anything that *could* be public lives under `internal/` until a stability contract justifies promotion via ADR. *(migration in progress: `pkg/backup/` and `pkg/restore/` move to `internal/adapters/{dump,restore}/`.)*

## 2. `internal/` — the hexagon

```
internal/
├── cli/                     # DRIVING adapter — Cobra commands (thin)
│   ├── root.go              # registers every top-level command
│   ├── backup.go  restore.go  schedule.go  monitor.go
│   ├── retention.go  security.go  storage_cmd.go  db.go  version.go  config.go
│   └── *_test.go
│
├── scheduler/               # DRIVING adapter — cron loop, in-process executor
│   ├── scheduler.go  executor.go  lock_integration.go
│   └── restore_integration.go    # (migration: thin wrapper over domain/restore)
│
├── config/                  # DRIVING adapter — YAML loader, validator, env overrides
│   ├── loader.go  validator.go  marshal.go  types.go  env.go
│   └── migrations/          # config schema migrations
│
├── domain/                  # PURE business logic — no I/O, no SDK imports
│   ├── backup/              # planner, executor, chain manager, full+incremental orchestration
│   ├── restore/              # restore orchestration, conflict strategy, chain validation
│   ├── retention/           # policy evaluation (count, age, size)
│   ├── schedule/            # cron model, next-run calculation
│   └── manifest/            # SHA-256 manifest + HashingWriter
│
├── ports/                   # Go interfaces — the contracts the domain depends on
│   ├── storage.go           # StorageBackend (Put, Get, List, Delete, Stat)
│   ├── dump.go              # DumpEngine (per-DB)
│   ├── restore.go           # RestoreEngine (per-DB)
│   ├── notifier.go          # Notifier (Slack, Discord, email, webhook)
│   ├── recorder.go          # execution history recorder
│   ├── crypto.go            # Encrypter, Decrypter
│   └── lock.go              # Locker (RunWithLock, RunWithTimeout)
│
├── adapters/                # DRIVEN adapters — implement ports, hold SDK deps
│   ├── storage/
│   │   ├── local/  s3/  gcs/  gdrive/  azure/
│   │   └── types/           # shared adapter-side types (no domain leak)
│   ├── dump/                # was pkg/backup/{pg,mysql,mariadb,mongo}_dump
│   │   ├── pg/  mysql/  mariadb/  mongo/
│   │   ├── mysqlbinlog/     # incremental helper (MySQL/MariaDB binlogs)
│   │   └── pg_combine/      # incremental helper (PG WAL combine)
│   ├── restore/             # was pkg/restore/{pg,mysql,mariadb,mongo}_restore
│   │   └── pg/  mysql/  mariadb/  mongo/
│   ├── notifier/
│   │   ├── slack/  discord/  email/  webhook/
│   │   └── dispatcher.go    # fan-out (still calls into domain notifier model)
│   ├── crypto/              # AES-256-GCM streaming impl
│   ├── monitor/             # SQLite recorder + queries (was internal/monitor)
│   ├── lock/                # file-based locker
│   └── tls/                 # TLS config builder
│
├── sanitize/                # cross-cutting — credential redaction (RedactArgs)
└── version/                 # build-time version info
```

There is **no `internal/utils/`** package. *(migration in progress: `default.go`, `file.go`, `time.go` will be redistributed to `config`, a new `manifest`/`fs` helper, and `domain/schedule`.)*

## 3. Dependency rules

The hexagonal rule, expressed as imports:

```
              ┌──────────────────────────┐
              │  internal/cli            │
              │  internal/scheduler      │   ← driving adapters
              │  internal/config         │
              └─────────────┬────────────┘
                            │ imports
                            ▼
              ┌──────────────────────────┐
              │  internal/domain/*       │   ← pure
              └─────────────┬────────────┘
                            │ imports
                            ▼
              ┌──────────────────────────┐
              │  internal/ports          │   ← interfaces only
              └─────────────▲────────────┘
                            │ implements (no import the other way)
                            │
              ┌─────────────┴────────────┐
              │  internal/adapters/*     │   ← driven adapters
              └──────────────────────────┘
```

Enforced rules:

1. **`internal/domain/*` MUST NOT import** anything from `internal/cli`, `internal/scheduler`, `internal/config`, or `internal/adapters/*`. It imports only `internal/ports`, `internal/sanitize`, `internal/domain/*`, and standard library.
2. **`internal/ports`** has zero internal imports. Pure interface definitions and minimal value types.
3. **`internal/adapters/*` MUST NOT import** other adapter sub-packages. Adapters are siblings; they communicate only through ports. *(Exception: a thin `adapters/storage/storage.go` dispatcher may import sibling backends. This is the current pattern and is grandfathered until an ADR proposes a registry.)*
4. **Driving adapters** (`cli`, `scheduler`, `config`) may import `domain/*`, `ports`, and `adapters/*` (to wire). They MUST NOT be imported *by* the domain.
5. **`internal/sanitize`** is the only utility allowed to be imported from any layer.
6. No package may import `main`.

Violations require an ADR.

## 4. Tests

Sentinel uses two test locations, deliberately:

| Location                  | Purpose                                                            |
|---------------------------|--------------------------------------------------------------------|
| Co-located `*_test.go`    | Unit tests. Same package or `_test` package. Pure Go, no Docker.   |
| `tests/integration/`      | Cross-package integration. May spin up containers via testcontainers. |
| `tests/benchmarks/`       | Long-running perf / size benchmarks.                                |
| `scripts/e2e.sh`          | Full end-to-end against real engines + storage emulators.           |

Rule of thumb: if a test imports more than one `internal/adapters/*` sub-package, it belongs under `tests/integration/`.

## 5. `docs/`

```
docs/
├── architecture/
│   ├── vision.md             # the why
│   └── project-layout.md     # this file
├── adr/
│   ├── 0000-template.md
│   └── NNNN-<slug>.md        # one per architectural decision
├── roadmap/
├── runbooks/
└── testing-guide.md
```

ADRs are required for: new external dependency, new layering rule, new on-disk format (manifest, encryption envelope, lock file), new storage backend, new DB engine, and any deviation from the rules in §3.

## 6. `infra/` and `scripts/`

```
infra/
└── docker/
    ├── docker-compose.yml         # local dev stack
    └── docker-compose.e2e.yml     # e2e stack (engines + Azurite + fake-gcs)

scripts/
├── e2e.sh                         # full e2e runner
└── wait-healthy.sh                # readiness probe
```

*(migration in progress: the repo currently has a root-level `docker-compose.e2e.yml`. Target is to keep both compose files under `infra/docker/`.)*

## 7. Public vs internal

Everything ships under `internal/`. The CLI is the only public contract. There is no `pkg/` directory.

If a future need arises to expose a Go API (library use case), the promotion path is:

1. Write an ADR describing the contract and stability guarantees.
2. Move the relevant sub-tree to `pkg/<name>/`.
3. Add a doc comment on the package declaring the stability tier.

Until then: `internal/` everywhere.

## 8. Migration status (as of 2026-05)

The current tree diverges from this target. Tracked moves:

| From                                                | To                                              | Driver                  |
|-----------------------------------------------------|-------------------------------------------------|-------------------------|
| `pkg/backup/{pg,mysql,mariadb,mongo}_dump`          | `internal/adapters/dump/{pg,mysql,mariadb,mongo}` | hexagonal commit        |
| `pkg/backup/{mysqlbinlog,pg_combine}`               | `internal/adapters/dump/{mysqlbinlog,pg_combine}` | same                    |
| `pkg/restore/*`                                     | `internal/adapters/restore/*`                   | same                    |
| `internal/storage/{local,s3,gcs,gdrive,azure}`      | `internal/adapters/storage/*`                   | same                    |
| `internal/storage/backend.go` (interface)           | `internal/ports/storage.go`                     | same                    |
| `internal/notifier/{slack,discord,email,webhook}`   | `internal/adapters/notifier/*`                  | same                    |
| `internal/crypto`, `internal/lock`, `internal/tls`, `internal/monitor` | `internal/adapters/{crypto,lock,tls,monitor}` | same                    |
| `internal/manifest`, `internal/retention`           | `internal/domain/{manifest,retention}`          | same                    |
| Orchestration inside `internal/cli/backup.go` (1475 LOC) | `internal/domain/backup/`                  | god-file split (PRD 14) |
| Orchestration inside `internal/scheduler/restore_integration.go` (18 KB) | `internal/domain/restore/`         | same                    |
| `internal/utils/`                                   | redistributed; package deleted                  | layout cleanup          |

Each move is governed by its own ADR.

## 9. References

- [vision.md](./vision.md) — the why, quality goals, hexagonal rationale.
- [../adr/](../adr/) — decisions that shaped this layout.
- [../../CONTRIBUTING.md](../../CONTRIBUTING.md) — human conventions.
- [../../CLAUDE.md](../../CLAUDE.md) — agent conventions.
