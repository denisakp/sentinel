# PRD I05 — Retention correctness

**Severity:** High (a safety switch that does not switch; deletions reported that never happened)
**Status:** Open
**Issues:** #157, #158, #159, #169, #182, #170.6, #170.7
**Area:** `internal/domain/retention/policy.go`, `internal/cli/{retention_cleaner,retention_helpers,backup,backup_factory}.go`,
`internal/adapters/storage/gcs/backend.go`, `internal/config/loader.go`
**Blocked by:** I01 (#168 — while `retention apply` exits 0 on error, none of these are observable)

## Problem

Retention is the subsystem where a defect costs data. Two independent families here: **reporting
honesty** (the sweep does not do what the config says, and says nothing) and **data shape** (the
rules and the deletions are wrong).

Read this PRD whole before touching any of it. Five of the seven share `retention_cleaner.go` or
`retention_helpers.go`.

## Confirmed defects

| # | Verdict | Defect | Cause |
|---|---|---|---|
| #157 | **PARTIAL** | `retention.dry_run: true` in YAML is ignored; backups are deleted for real | `internal/cli/backup.go:617` calls `applyJobRetention(..., job.Name, false)` — `dryRun` hardcoded `false` |
| #158 | CONFIRMED | `keep_last` and `keep_days` intersect rather than union | `domain/retention/policy.go:38-49` — `flatDeleteReason` merges both exceeded-conditions into one delete-set, so a record must satisfy **both** rules to survive |
| #159 | CONFIRMED | Retention deletes the artifact and orphans its sidecars | `retention_cleaner.go:29-145` calls only `backend.Delete(ctx, cand.FilePath)`. `grep "manifest\|.binlogs\|.oplog"` in that file → 0 matches |
| #169 | CONFIRMED | Retention deletion is unimplemented for `google-drive`, and `preview` never warns | `retention_cleaner.go:142-144` default case errors on "google-drive"; `backup_factory.go:118`'s dry-run branch returns **before** reaching that check. No validation-time rejection either |
| #182 | CONFIRMED | GCS retention reports deletions that never happened | double object-path processing — see below |
| #170.6 | CONFIRMED | `preview` and `apply` print identical wording | `retention_helpers.go:98` shares `"total deleted: %d backups"`. **Owned by I01** — same file and same PR as #168; listed here for context only |
| #170.7 | CONFIRMED | A `dry_run`-only policy blocks `defaults.retention` inheritance | `loader.go:164` `hasRetention` returns true when `DryRun` is set even with `KeepLast`/`KeepDays`/GFS all zero; `retentionEnabled()` (`retention_helpers.go:49-57`) then ignores `DryRun`, so the job is skipped entirely by `applyAllRetention` |

### Correction to #157

The issue cites `backup_factory.go:313` as a second site. That line builds `djob.Retention` — and
**that field is read nowhere in `internal/domain/backup/`**. `executor.go:166` says so in a comment:
"Retention sweep … stays in the driving adapter". It is a dead field. Fixing the cited line changes
nothing; the real site is `backup.go:617`.

### #182 in detail — more precise than the issue

The root cause is **the object path being stripped twice**.

1. `retention_cleaner.go:92` `parseBucketObjectRef` already strips `gs://bucket/` and passes the
   bucket-relative key, e.g. `backups/pg-job/dump.sql.gz` — still containing `/`.
2. `gcs/backend.go:128-137` `Delete` then runs `extractObjectPath` **again** (`backend.go:319-336`),
   and for any non-`gs://` string containing `/` it applies `filepath.Base`, flattening the key to
   `dump.sql.gz`.
3. That object does not exist. `sdkBucketClient.Delete` (`backend.go:279-285`) swallows
   `gcsapi.ErrObjectNotExist` and returns `nil`.
4. The run reports a successful deletion. Nothing was deleted.

Second, compounding: `applyJobRetention` (`backup_factory.go:122-125`) returns on the **first**
error from `deleteRetentionCandidates`, before calling `rec.RetentionDeleteRecords`. One failing
candidate skips the history delete for **every** candidate in the run. Backend-agnostic.

## Fix — order matters

1. **#168 (in I01)** must be in first. Until `retention apply` propagates its errors, every other
   fix here is unverifiable in unattended use.
2. **#157** — pass `job.Retention.DryRun` (OR'd with any CLI override) into the `backup.go:617` call.
3. **#170.7** — exclude `DryRun` from `hasRetention`'s "has a policy" test.
4. **#170.6** — landed by I01 (same file as #168). Verify it before starting item 5.
5. **#182** — (a) stop re-running `extractObjectPath` on an already bucket-relative key, either by
   passing the full `gs://` URI or by making `Delete`/`Exists` accept bare keys without
   basename-flattening; (b) treat `ErrObjectNotExist` as a reportable failure, not success;
   (c) delete history rows **per successfully-deleted candidate**, not all-or-nothing.
6. **#159** — derive and delete `<path>.manifest.json`, `.binlogs.tar`, `.oplog.archive` alongside
   each artifact. Best-effort: a missing sidecar must not fail the run.
7. **#158** — build separate keep-sets per flat rule and union them. Note `policy.go:58` already
   *claims* "union of keeps" in a comment; the comment describes the intent, the code does the
   opposite.
8. **#169** — implement the gdrive `Delete`, **or** reject retention on gdrive at validation time.
   Either way, add an explicit preview-time warning.

## DECISION REQUIRED

- **#158 direction.** `apply-retention.md`, `retention-gfs.md` and the README all describe a
  **union**. Only the code disagrees. Changing the code to union means **existing installs retain
  more backups than they do today** — storage cost goes up silently on upgrade. The alternative is
  changing three documents and the README to describe an intersection. Recommendation: change the
  code (documentation, README and evident intent all agree against it), but flag it in release
  notes as a behaviour change.
- **#169 direction.** Implement gdrive deletion, or reject the combination at config load. Rejecting
  is honest and cheap; implementing is what the config already promises.

## Definition of done

- **Code:** the eight fixes above.
- **e2e:**
  - `retention.dry_run: true` with history exceeding `keep_last` — **no file deleted** from storage,
    history row count unchanged.
  - 10 daily backups, `keep_last=2` + `keep_days=30` — all 10 survive (union), versus 2 today.
  - A backup with manifest and binlog sidecars deleted by retention — the sidecars are gone too.
  - `retention preview` on a gdrive job — prints a warning.
  - A GCS-backed job with a **nested** `outName` path — after `retention apply`, the object is
    actually absent from the bucket, and with one candidate engineered to fail, only the confirmed
    deletes lose their history rows. **NEEDS_LIVE** (fake-gcs is in the compose file).
- **Docs:** `guides/apply-retention.md`, `guides/retention-gfs.md`, `reference/cli/retention.md`,
  `guides/migrate-storage-backend.md`. #182 has **no** doc page today — add one, or add an
  admonition to the GCS storage page.
- **Runbooks:** `docs/runbooks/apply-retention.md` and `retention-gfs.md` are **correct** about the
  union rule and the code is wrong. Do not "fix" them to match current behaviour.
