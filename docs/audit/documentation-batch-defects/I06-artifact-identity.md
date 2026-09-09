# PRD I06 — Artifact identity: output naming, manifest, history

**Severity:** High (backups are unverifiable, and history records `"unknown"` as the artifact path)
**Status:** Open
**Issues:** #151, #193, #184.2
**Area:** `internal/cli/backup_factory.go`, `internal/utils/default.go`,
`internal/domain/backup/{executor,pipeline}.go`, `internal/adapters/storage/local/backend.go`
**Blocks:** I07 (the Mongo artifact fix reuses whatever hashing/identity primitive lands here)

## Problem

There is no single answer to "what file did this backup produce?". The dump builder resolves one
name, the domain Job carries another (often empty), the manifest writer sees the empty one, and the
storage `Status` counts the artifact twice.

## Confirmed defects

### #151 — no manifest is written unless the job sets `output:`

**The issue's title describes the symptom, not the cause.** The cause is staleness:

`internal/cli/backup_factory.go:320` sets `djob.OutName = storageParams.OutName` **before**
`e.dumps.Build()` (`executor.go:115`) resolves the real filename via `FinalOutName`/`setOutName`.
That resolution lands on the dump builder's own `*storage.Params` and **never propagates back to the
domain Job**. With `output:` unset, `job.OutName` stays `""` for the whole `Run()`.

Downstream: `ApplyArtifactSecurity` (`pipeline.go:116-119`) short-circuits to `(nil, nil, nil)`
because `LocalArtifactInfo(..., job.OutName="")` returns `""`.

**Extra defect, not in the issue:** `res.ArtifactPath` at `executor.go:159` resolves to `"unknown"`
for the same reason. **The execution history is broken, not just the manifest.**

Scheduled runs are unaffected — the scheduler fills a name in first. This is a CLI-path defect.

### #193 — an explicit `output:` makes every backup overwrite the previous one

`internal/utils/default.go:21-29` `FinalOutName` returns `outName` verbatim whenever it has an
extension. `output: shop.sql` resolves to the same literal name on every run. No interpolation logic
exists anywhere in the call chain — `dump/{pg,mysql,mariadb}/*_dump*.go` all call it verbatim.

So `output:` is required for integrity (per #151) and destroys history when set. There is no
configuration that gets both.

### #184.2 — `backup_count` doubles

`internal/adapters/storage/local/backend.go:52-70` `List` walks every file with no suffix filter;
`Status:82-102` sets `BackupCount: len(objects)`. Each backup contributes its artifact **and** its
`.manifest.json` sidecar. The same `List → Status` shape exists in s3/gcs/gdrive/azure — verify each.

## The pairing — read before implementing

**Fixing #193 alone does not fix #151.** They are different code paths.

- #151's cause is `Job.OutName` staleness. Manifests are missing **even when `output:` is unset**,
  regardless of any naming policy.
- #193's cause is `FinalOutName`'s literal passthrough. An explicit fixed name collides every run.

Correct contract, in this order:

1. **The domain Job must always carry the real resolved artifact name at manifest-write time.**
   Resolve the final output name once before constructing the Job, or refresh `job.OutName` from the
   build result immediately after `Build()` and before `ApplyArtifactSecurity` runs. Structural —
   do this first.
2. **An explicit `output:` must still vary per run**, via timestamp or backup-ID interpolation, so
   it never collides. Layered on top of (1).

## Fix

1. `backup_factory.go` / `executor.go` — resolve and propagate the real output name (#151).
2. `utils/default.go` — give `FinalOutName` a template mode; document the placeholders in
   `config/types.go` (#193).
3. `storage/local/backend.go` — exclude `*.manifest.json` from `List`, or count only non-manifest
   objects in `Status`. Check and fix the same pattern in s3/gcs/gdrive/azure (#184.2).

## DECISION REQUIRED

**#193's naming scheme.** Interpolating into an explicit `output:` changes the filenames every
existing install produces. Options: (a) always interpolate, (b) interpolate only when the value
contains a placeholder token, (c) keep literal names and require uniqueness at validation time.
Option (b) is backward-compatible and explicit; (a) is what the issue asks for. Product call.

## Definition of done

- **Code:** the three fixes, in the stated order.
- **e2e:**
  - A job with **no** `output:` against local storage, then `backup verify <id>` — exit 0, not
    `missing_manifest`, and `ArtifactPath != "unknown"`.
  - One job run twice with `output: shop.sql` — two distinct stored artifacts, two distinct manifest
    hashes, two history rows. Not one truncated file.
  - `Status()` after one backup — `BackupCount == 1` with the artifact and its `.manifest.json`
    both present.
- **Docs:** `operations/troubleshooting.md`, `operations/failed-backup-triage.md`,
  `operations/chain-corruption-recovery.md`, `guides/integrity-sweep.md`,
  `guides/verify-backup-integrity.md`, `concepts/manifest.md`, `reference/glossary.md` — all cite
  #151 with the correct root cause and note scheduled runs are unaffected. #193 has no doc page;
  add one if the naming behaviour changes.
