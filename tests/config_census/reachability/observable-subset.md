# Which configuration keys the existing suite can prove behaviourally

**Requirement**: FR-020a and FR-021, spec 061. **Determined**: 2026-09-09.

Nothing enumerated this subset before. It was recorded as "the keys the existing suite can already
observe", which is not a set anyone had actually written down, so this file establishes it.

## Method

A key is behaviourally provable *here* if setting it to two different values produces two different
observable results **without new fixtures and without live infrastructure**. That is a deliberately
harsh bar. Confirming that a configuration was accepted does not qualify: that is precisely the
assertion whose weakness produced this defect batch, since a command that validates a key, discards
it, and exits zero passes it.

Two candidate observation points exist today:

1. **Config resolution alone.** Load two configurations differing in one key and compare the loaded
   result. No database, no container, sub-second. This is where the provable subset lives.
2. **The container-backed suite.** Run a real backup or restore and compare artifacts, history rows
   or exit behaviour. Far more powerful, but it needs the infra and, for the engine paths, the four
   database client binaries a developer host usually lacks.

Only the first counts as "without new fixtures and without live infrastructure".

## Proven

| Path | How | Test |
|---|---|---|
| `defaults.compression.enabled` | Two configurations differing only in this default resolve to different values on an inheriting job | `TestDefaultsCompressionReachesJobs` |
| `history_db_path` | Two configurations differing only in this key resolve to different paths | `TestHistoryDBPathIsHonoured` |

`defaults.compression.enabled` is worth its own note. The static heuristic could not classify it: the
Go field `Enabled` is declared on seven configuration types, so no read can be attributed to it by
name. The behavioural test settles what static inference could not, which is the argument for keeping
both kinds of evidence rather than choosing one. It also happens to be the key issue **#188** is
about, and a defect of that shape stays invisible until something asserts the value actually arrives.

## Not provable without infrastructure, and why

The remaining 226 paths fall into three groups.

- **Requires a running database.** Everything under `databases.*` and `restores.*` that shapes a dump
  or restore invocation. Their effect appears in the command the product builds and the artifact it
  produces, neither of which exists without an engine to talk to. This is the largest group.
- **Requires a storage backend.** Everything under `storages.*`, plus the remote paths under
  `defaults.*`. Observable against the emulators the container suite already starts, so these are the
  cheapest to convert next.
- **Requires a scheduler run.** `scheduler.*` and `integrity.scheduled_check.*` only take effect on a
  timed execution.

Each `connected` entry carries its own `needs` field saying what its proof would require, so the
outstanding coverage is an enumerable list rather than an implication. That is FR-021.

## What would move the line

Extending the container-backed suite to diff a real backup's manifest against the configuration that
produced it would make most of the `storages.*` and `defaults.*` groups provable in one step. That is
larger than this slice and belongs with the PRDs that touch those paths, each of which is required to
ship its assertion alongside its fix.
