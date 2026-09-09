# PRD I07 — Dump streaming + MongoDB local artifact

**Severity:** High (peak memory scales with database size; Mongo local backups are unverifiable)
**Status:** Open
**Issues:** #164 (PARTIAL), #191
**Area:** `internal/adapters/dump/{pg,mysql,mariadb,mongo}/`, `internal/adapters/storage/writer.go`,
`internal/domain/backup/pipeline.go`
**Blocked by:** I06 (the Mongo fix reuses the artifact-identity/hashing primitive that lands there)

## Problem

### #164 — PARTIAL: three engines buffer the whole dump, not four

`internal/adapters/dump/pg/pg_dump.go:55-56`, `mysql_dump.go:46-47` and `mariadb_dump.go:48-49` all
set `cmd.Stdout = &bytes.Buffer` and then do `sum := sha256.Sum256(stdOut.Bytes())` /
`WriteBackup(stdOut.Bytes(), ...)`. Peak RSS scales with dump size.

**The issue says "every dump adapter". That is wrong for MongoDB.** `mongodump` streams straight to
disk via `--archive` / `--out`; its stdout buffer captures log text only. Mongo's real defect is
#191, below, and it is different in kind.

### #191 — a default local MongoDB backup is a directory recorded as an empty artifact

`internal/adapters/dump/mongo/mongo_dump.go:130-134` hashes `stdOut.Bytes()` — **mongodump's log
output**, not the dump — and writes that via `WriteBackup`. The actual dump went to a directory via
`--out=` (`args_builder.go:67`).

Downstream both layers agree to do nothing about it:
- `internal/domain/backup/pipeline.go:39-40` `LocalArtifactInfo` explicitly `return path, 0` when
  `info.IsDir()`.
- `internal/adapters/storage/local/local.go` `WriteData` special-cases directories and no-ops.

Result: the backup succeeds, the recorded artifact is empty, and the hash is computed over log text.
It cannot be verified and it cannot be restored from the recorded path.

The remote path already does the right thing — it wraps the dump in a single archive.

**Not verified:** the issue's secondary claim that the oplog is deleted before upload looks
plausible from the staging/cleanup code but was not traced end to end. Treat as unconfirmed.

## Fix

1. **#164** — stream `cmd.Stdout` through an `io.Pipe` / `HashingWriter` into the `writer.go`
   storage sink instead of buffering. `internal/adapters/crypto.HashingWriter` already exists and is
   what the manifest store uses.
2. **#191** — for local Mongo, wrap the dump in a single archive (`--archive=`) as the remote path
   already does, then hash and size **that file**. Reuse the streaming primitive from (1).
3. Consider making `LocalArtifactInfo`'s directory branch an **error** rather than a silent
   `return path, 0`. It is the layer that turned a broken artifact into a successful backup.

Order: #164 first — #191's fix should reuse whatever hashing/streaming primitive it introduces
rather than inventing a second one.

## Cross-reference

I06 must land first. #191's "hash the real artifact" fix depends on the domain Job carrying the real
resolved artifact name, which is exactly what #151 fixes.

## Definition of done

- **Code:** the three items above.
- **e2e:**
  - A local MongoDB backup job — either `sentinel backup verify` fails **loudly** (not silently
    passes), or the artifact is a single restorable file with a real hash. The latter is the goal.
  - A restore from that local Mongo artifact round-trips the data.
  - A multi-hundred-MB dump — peak `sentinel` RSS stays roughly flat against dump size.
    **NEEDS_LIVE** for real proof; the buffering itself is confirmed statically.
- **Docs:** #191 has no doc page today. The MongoDB tutorial and `concepts/storage-backends.md`
  describe local Mongo backups as working — add an admonition now and remove it with the fix.
  #164's `grep` hits (recover-legacy-envelope.md, operations/index.md, key-loss-incident.md,
  failed-backup-triage.md, security-encryption.md) are about `--allow-legacy-envelope`, a **different
  matter** that happens to share the number in prose. Do not edit them for this PRD.
