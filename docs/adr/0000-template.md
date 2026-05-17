# ADR NNNN — <Title>

- **Status**: Proposed | Accepted | Superseded by ADR-XXXX | Deprecated
- **Date**: YYYY-MM-DD
- **Deciders**: <names>
- **Tags**: <storage, engine, crypto, config, scheduler, cli, ...>

## Context

<!--
  Factual description of the situation in Sentinel today.
  What constraints apply (Go 1.24, single-binary CLI, operator-run from cron,
  must work offline)? What triggered this decision? Stay descriptive — no
  opinion in this section.
-->

## Decision drivers

- Driver 1 (e.g. "must not break restore of existing v1.2 backups")
- Driver 2 (e.g. "must work offline / without cloud credentials")
- Driver 3 (e.g. "operator runs from cron, no daemon")

## Options considered

### Option A — short label

Description.

**Pros**
-

**Cons**
-

### Option B — short label

Description.

**Pros**
-

**Cons**
-

### Option C — (if applicable)

...

## Decision

<!--
  The choice, stated affirmatively in the present tense.
  "Sentinel uses pgx for PG17 WAL parsing."
  NOT "We will use" or "It was decided to use".
-->

## Consequences

### Positive
-

### Negative
-

### Neutral / to watch
-

## Compatibility & migration

<!--
  Mandatory for Sentinel. Address:
  - Does this change the on-disk backup layout, manifest format, or encryption envelope?
  - Are existing backups still restorable by the new code? How many versions back?
  - Does `sentinel.yaml` need migration? Is there a deprecation-warning path?
  - CLI flag/output changes — any scripted consumers affected?
  If none apply, write "No compatibility impact."
-->

## Implementation checklist

- [ ] Concrete step 1 (reference real paths, e.g. `internal/storage/storage.go`)
- [ ] Concrete step 2 (tests, e.g. `internal/storage/types/` or `scripts/e2e.sh`)
- [ ] Concrete step 3 (docs, e.g. `README.md`, `docs/testing-guide.md`, `release-notes.md`)

## References

- Issues, prior ADRs superseded, upstream docs, RFCs, CVEs
