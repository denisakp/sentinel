# The configuration reachability record

One file per top-level schema area. The check reads them all as a single record, so an entry filed
under the wrong area is still found; the split is an authoring convenience, not part of an entry's
identity.

It buys two things. The five areas can be populated in parallel rather than every contributor
queueing on one file, and a diff stays reviewable.

## Areas and measured path counts

| File | Area | Settable paths |
|---|---|---|
| `root.yaml` | root scalars, `restore`, `scheduler`, `integrity` | 23 |
| `storages.yaml` | `storages` | 20 |
| `defaults.yaml` | `defaults` | 43 |
| `databases.yaml` | `databases` | 78 |
| `restores.yaml` | `restores` | 64 |
| | **Total** | **228** |

Counts are measured by reflecting over the root configuration type, not estimated. They are paths,
not field definitions: the same nested type is reached through several parents, and each is
independently settable and therefore independently able to be ignored. There are 180 distinct field
definitions behind these 228 paths, and counting those instead would hide exactly the "wired on one
axis, forgotten on the other" pattern the batch plan names as a root cause.

`restores.yaml` is the area whose fields live in `internal/config/restore_types.go` rather than
`types.go`. It was missed entirely by the first estimate of this work's size.

## Entry format

See [`contracts/reachability-record.md`](../../../specs/061-e2e-harness-foundation/contracts/reachability-record.md)
for the authoritative contract. In short, every settable path appears exactly once, classified
`connected`, `inert` or `partial`, and a connected path carries an evidence anchor of a file plus an
exact expression appearing in it.

Anchor on something specific. A bare field name such as `Enabled` or `Mode` recurs across many types
and matches anywhere, proving nothing. Never anchor on a line number: it moves on unrelated edits
until the check is disabled as noise.
