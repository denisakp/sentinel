# Sentinel — Architecture Vision

> Why Sentinel exists, what it optimises for, and which architectural style enforces those goals.

---

## 1. Mission

Sentinel is a single-binary Go CLI that automates **database backup, restore, and disaster recovery** across PostgreSQL, MySQL, MariaDB, and MongoDB. It targets self-hosted operators and small-to-mid teams who need production-grade guarantees without running a managed backup product.

The product promises:

- **Integrity**: every backup is verifiable (SHA-256 manifest, optional AES-256-GCM envelope).
- **Recoverability**: full + incremental chains, PITR for engines that support it, validated before restore.
- **Operability**: one binary, one YAML, one cron-like scheduler, observable history (SQLite).
- **Safety**: credentials never leak to logs, concurrent runs are locked, retries are bounded.

## 2. Quality goals (in priority order)

| # | Goal             | What it means concretely                                                                |
|---|------------------|-----------------------------------------------------------------------------------------|
| 1 | **Correctness**  | A successful backup can always be restored. Chains validate before any destructive op. |
| 2 | **Integrity**    | Bit-rot, partial writes, and tampering are detected at restore time.                    |
| 3 | **Security**     | Secrets never appear in logs, args, or notifications. Encryption is opt-in but first-class. |
| 4 | **Portability**  | One static binary, no runtime dependencies beyond engine client tools (`pg_dump`, …).   |
| 5 | **Extensibility**| New storage backends, DB engines, and notifiers plug in without touching the core.     |
| 6 | **Observability**| Every run is recorded; the operator can answer "what happened, when, and why".          |

Performance is a non-goal until the above are met. We accept a slower but verifiable backup over a fast and silent one.

## 3. Constraints

- **Language**: Go ≥ 1.24. Standard library first; third-party deps require an ADR.
- **Runtime**: single binary, no daemon required. Scheduler runs in-process when invoked.
- **Persistence**: local SQLite for execution history. No external DB server.
- **External tools**: backups shell out to engine-native CLIs (`pg_dump`, `mysqldump`, `mongodump`, …). Sentinel does not reimplement dump logic.
- **Compatibility**: configuration is YAML, versioned. Breaking changes require a migration step.

## 4. Architectural style — Ports & Adapters (Hexagonal)

Sentinel adopts **hexagonal architecture** because the product's value depends on plugging *different things* into a *stable core*:

- Different **storage backends** (local, S3, GCS, GDrive, Azure) behind one contract.
- Different **DB engines** (Postgres, MySQL, MariaDB, Mongo) behind one dump/restore contract.
- Different **notifier channels** (Slack, Discord, email, webhook) behind one delivery contract.
- Different **drivers** of the same domain logic: an interactive CLI command *and* an in-process scheduler both trigger the same `backup` use case.

The layered alternative (cli → service → repository) would force the domain to know about Cobra, YAML, and S3 SDKs. Hexagonal keeps the domain pure and pushes I/O to the edges.

### Layers

```
            ┌─────────────────────────────────────────────────────┐
            │  Driving adapters  (cli, scheduler)                 │
            └───────────────┬─────────────────────────────────────┘
                            │ calls
                            ▼
            ┌─────────────────────────────────────────────────────┐
            │  Domain  (backup, restore, retention, manifest)     │  pure Go
            │  depends only on ports + standard library           │
            └───────────────┬─────────────────────────────────────┘
                            │ depends on (interfaces)
                            ▼
            ┌─────────────────────────────────────────────────────┐
            │  Ports  (Go interfaces: Storage, Dump, Restore,     │
            │   Notifier, Locker, Recorder, Crypto)               │
            └───────────────┬─────────────────────────────────────┘
                            │ implemented by
                            ▼
            ┌─────────────────────────────────────────────────────┐
            │  Driven adapters  (storage/*, dump/*, restore/*,    │
            │   notifier/*, crypto, lock, monitor, tls)           │
            └─────────────────────────────────────────────────────┘
```

### Dependency rule

```
adapters  →  ports  ←  domain
                       ↑
              cli, scheduler
```

- The **domain** imports `ports` only. Never an adapter, never `cli`, never `config`.
- **Adapters** implement `ports`. They may import third-party SDKs (AWS, GCP, Mongo driver, …) freely.
- **Drivers** (`cli`, `scheduler`) wire ports to adapters and call the domain.
- **`config`** is a driving adapter: it parses YAML and produces domain inputs.

Any import that violates this rule must be justified by an ADR.

## 5. Cross-cutting concerns

These cut across layers but stay free of I/O:

- **`sanitize`** — credential redaction; called by every arg builder before logging.
- **`manifest`** — SHA-256 + `HashingWriter`; lives in the domain because integrity is a domain concept.
- **Errors** — wrap with `fmt.Errorf("%w", ...)`; never swallow.
- **Context** — every long-running op takes `context.Context`; cancellation propagates from CLI/scheduler.

## 6. Trade-offs accepted

- **Some indirection cost**: a port + adapter is two files where layered code is one. Justified by swap-ability.
- **No plugin system at runtime**: adapters are wired at compile time. Lower flexibility, much simpler distribution (one static binary).
- **SQLite for history**: not horizontally scalable. Acceptable: Sentinel is a single-node operator tool.
- **Shell-out to engine CLIs**: Sentinel inherits their bugs and CVEs. Accepted: reimplementing `pg_dump` is out of scope.

## 7. What is explicitly *not* in scope

- Multi-tenant SaaS. Sentinel is a single-operator tool.
- Backup of non-DB assets (object storage, filesystems). Use a dedicated tool.
- Real-time replication. Sentinel is point-in-time backup, not streaming replication.
- A daemon mode with HTTP API. May become a future ADR; today the scheduler runs in-process.

## 8. Where decisions are recorded

- **Architecture Decision Records**: `docs/adr/NNNN-*.md`. One ADR per non-trivial choice (new dep, new layering rule, new on-disk format).
- **Product Requirements**: `prds/v*.md` (feature scope), `.prds/` (tactical tickets and bug fixes).
- **Spec Kit artefacts**: `specs/<feature>/` (spec, plan, tasks).

## 9. References

- [project-layout.md](./project-layout.md) — where each concept lives in the tree.
- [../adr/](../adr/) — ADR index.
- [../../CONTRIBUTING.md](../../CONTRIBUTING.md) — human conventions.
- [../../CLAUDE.md](../../CLAUDE.md) — agent conventions.
