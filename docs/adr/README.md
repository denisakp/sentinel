# Architecture Decision Records

Sentinel records every non-trivial architectural choice as an ADR. The format and review rules live in `.claude/skills/sentinel-adr-writer/SKILL.md`.

## Index

| #     | Title                                                                 | Status   | Date       |
|-------|------------------------------------------------------------------------|----------|------------|
| 0000  | [Template](./0000-template.md)                                         | —        | —          |
| 0001  | [Adopt hexagonal (ports & adapters) architecture](./0001-adopt-hexagonal-architecture.md) | Accepted | 2026-05-17 |
| 0002  | [Remove the `pkg/` directory](./0002-remove-pkg-directory.md)         | Accepted | 2026-05-17 |
| 0003  | [Remove `internal/utils/`](./0003-remove-internal-utils.md)           | Accepted | 2026-05-17 |
| 0004  | [Storage backend port location](./0004-storage-backend-port-location.md) | Accepted | 2026-05-17 |
| 0005  | [Manifest format v1](./0005-manifest-format-v1.md)                    | Accepted | 2026-05-17 |
| 0006  | [Encryption envelope v1 (AES-256-GCM, 64 KB chunks)](./0006-encryption-envelope-v1.md) | Proposed (blocked by `.prds/01`, `.prds/07`) | 2026-05-17 |
| 0007  | [Lock file format v1](./0007-lock-file-format.md)                     | Proposed (blocked by `.prds/10`) | 2026-05-17 |
| 0008  | [Scheduler concurrency model](./0008-scheduler-concurrency-model.md)  | Proposed (blocked by `.prds/09`) | 2026-05-17 |

## Conventions

- Files: `NNNN-kebab-case-title.md`, zero-padded four-digit number, sequential.
- `0000-template.md` is the canonical template — never a decision.
- Status lifecycle: `Proposed → Accepted → Superseded by ADR-XXXX | Deprecated`.
- Never delete an ADR. Supersede instead, with a banner at the top of the old file pointing to the new one.
