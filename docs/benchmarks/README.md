# Performance benchmarks

Published, reproducible performance numbers for Sentinel's backup/restore path — wall-clock,
peak RAM, CPU, artifact size, and compression ratio — across PostgreSQL, MySQL, MariaDB, and
MongoDB. This closes the "how long does a full backup take, how much RAM does it burn" gap for
a DBA evaluating Sentinel against a real database size, before committing (backlog item F-05).

- **Per-release matrix**: [`v1.3.0.md`](./v1.3.0.md) — one file per significant release,
  refreshed on each significant (minor) release. Older versions are kept for comparison.
- **Harness**: [`scripts/benchmark.sh`](../../scripts/benchmark.sh) (`make bench-small`) —
  generates a deterministic dataset, runs real backup/restore scenarios through the `sentinel`
  CLI, and emits `results.csv` / `results.json` / `results.md`.
- **Dataset generators**: [`infra/dataset/generate.sh`](../../infra/dataset/generate.sh) —
  committed, deterministic, size-targeted per engine (replaces a previous untracked, ad-hoc
  Python venv under `infra/dataset/`).

## This is NOT `tests/benchmarks/`

`tests/benchmarks/*.go` (`config_parsing_bench_test.go`, `scheduler_bench_test.go`,
`advanced_restore_benchmark_test.go`) are Go `testing.B` **micro-benchmarks**: they measure
ns/op and allocs/op of pure in-process Go logic — config parsing, `scheduler.AddJob`, restore
planner overhead. They never touch a real database, never produce an artifact, and give zero
signal about "how long to back up 50GB." This directory is the opposite: **real databases,
real dump/restore tools, real artifacts, measured wall-clock and RAM.** Keep the two separate;
they answer different questions for different audiences.

## Methodology

- **Storage backend**: local disk only. Non-local backends (S3/GCS/Azure/gdrive) introduce
  network variance that would swamp the engine/tool signal this matrix isolates; they're a
  separate study (out of scope for this matrix).
- **Metrics source — read, not re-instrumented**: wall-clock (`duration_ms`) and artifact size
  (`file_size_bytes`) are read back from `sentinel monitor export --format csv` (see
  `internal/adapters/monitor/export.go` for the column list) — the harness adds zero
  instrumentation to product code. `sentinel restore run` executions are **not** covered by
  `monitor export` (they live in a separate `restore_executions` table with no CSV/JSON export
  today — `sentinel restore history` is text-only); restore wall-clock is instead read from the
  same `time -v` wrapper used for peak RAM (see below). This is the one place the harness's own
  measurement, rather than the monitor, is the source of truth for restore scenarios.
- **Peak RAM + CPU**: Sentinel spawns the engine's native tool (`pg_dump`, `mysqldump`/
  `mariadb-dump`, `mongodump`) as a subprocess and streams; measuring only the Go process would
  undercount. The harness wraps the whole `sentinel` invocation in **GNU `/usr/bin/time -v`**
  (Maximum resident set size + Percent of CPU) run *inside* the Linux container — this is a
  measurement wrapper, not a product-code change.
- **Compression ratio**: computed generically for *every* engine by running the same backup
  scenario twice — once with Sentinel's own pipeline compression (spec 049 / PRD 33, zstd)
  disabled, once enabled — and dividing artifact sizes. This works uniformly across all four
  engines because pipeline compression is engine-agnostic (unlike each engine's own native
  compression flag, e.g. `pg_dump --compress` or `mongodump --gzip`, which the harness does not
  additionally exercise).
- **Environment**: Sentinel version, hardware, OS, and DB engine versions are recorded per
  matrix file (see the top of each `vX.Y.Z.md`). `/usr/bin/time -v` is **GNU time**, not
  available on macOS directly — the harness always runs `sentinel` *inside* the
  `sentinel-dev:local` Linux container (same image `scripts/e2e.sh` uses), so the host OS
  invoking the harness does not matter; only a bare, non-containerized `time -v sentinel ...`
  invocation would require a Linux host.
- **Docker Desktop / virtualized-host caveat**: when producing a matrix, run the harness on a
  Linux Docker host (bare metal or a cloud VM) if at all possible. On Docker Desktop for macOS,
  peak-RAM figures for large (≥1GB) datasets were observed to scale consistently with artifact
  size rather than reading as a fixed virtualization artifact — see the note under `v1.3.0.md`
  for the concrete numbers and interpretation — but treat peak-RAM numbers captured on a
  virtualized Docker host as provisional until cross-checked on native Linux.

## Tiers

| Tier | Size | Where it runs | Gate |
|---|---|---|---|
| `small` | ~1GB per engine | CI (smoke) + local | **Report-only.** No hard perf thresholds — a slow number does not fail the build (avoids flaky gates from noisy CI runners); it's a regression *signal*, not a gate. |
| `large` | 10GB / 50GB | Operator-run only, by hand | Not automated — generating and backing up 10-50GB inside GitHub Actions is infeasible (time + ephemeral disk). Results are committed to the matrix by hand after a manual run, with the environment documented. |

## Reproduce it yourself

```bash
make infra-up                                  # start pg/mysql/mariadb/mongo
make build-image                               # build sentinel-dev:local (needs a local infra/docker/Dockerfile.dev — see below)

scripts/benchmark.sh --tier small                              # all 4 engines, ~1GB each
scripts/benchmark.sh --tier small --engine postgres            # one engine
scripts/benchmark.sh --tier small --engine postgres --size-mb 50   # override size (fast local smoke)
scripts/benchmark.sh --tier small --with-incremental            # also attempt incremental/PITR (best-effort, see below)

make bench-small                                # convenience wrapper for the line above
```

Results land in `.bench/results.{csv,json,md}` (gitignored — a run's own output is not
committed; the *matrix* under `docs/benchmarks/` is a curated, hand-reviewed copy of specific
runs).

`infra/docker/Dockerfile.dev` is a locally-maintained dev image definition (gitignored, same as
the existing `scripts/e2e.sh` precedent) — build sentinel + pg/mysql/mariadb/mongo client
tools into an Alpine base. See `scripts/e2e.sh`'s own header comment for the expected shape if
you don't already have one.

## Incremental / PITR scenarios: best-effort, mostly `skipped` on generic infra

`--with-incremental` attempts `backup-incremental` and `restore-pitr` (or, for MySQL/MariaDB,
the binlog-replay equivalent) per engine, but **the stock `infra/docker/docker-compose.yml`
services do not enable the server-side prerequisites** Sentinel itself checks
(`internal/domain/backup/incremental/prerequisites.go`):

| Engine | Requires | Enabled in `docker-compose.yml`? |
|---|---|---|
| PostgreSQL | `wal_summary`/`summarize_wal = on` (PG17+) | No |
| MySQL/MariaDB | `log_bin = ON` + a locally-mounted `mysql.binlog_path` | No |
| MongoDB | A replica set (oplog) | No (standalone node) |

The harness probes each prerequisite before attempting the scenario and records a `skipped` row
with the exact reason when unmet, rather than fabricating a number or letting a confusing raw
tool error through. Postgres PITR additionally needs continuous WAL archiving
(`archive_mode=on` + `archive_command`), which is a separate, heavier precondition again beyond
generic docker-compose. Enabling these is a real, valuable follow-up for a future benchmark
revision but is operator/infra work, not a harness bug.

## Known gaps (v1.3.0 matrix)

- **MongoDB `restore-full`**: Sentinel's Mongo restore path runs a `mongosh`-based connectivity
  preflight (`internal/adapters/db_probe`); Alpine (the `sentinel-dev:local` base image) has no
  `mongosh` package, so this scenario errors out on a stock Alpine dev image. Add `mongosh` to
  your `Dockerfile.dev` (e.g. a Debian-based variant, or the official `mongosh` tarball) to
  exercise it. Mongo `backup-full` and `backup-full-compressed` are unaffected and measured.
- **Local Mongo backups default to `mongodump --out=` (directory mode)**, but Sentinel's Mongo
  *restore* path always invokes `mongorestore --archive=` — so a local Mongo backup is only
  restorable if it was captured with `database_options.archive: true`. The harness sets this by
  default; a hand-written config that omits it will produce an unrestorable local backup. Worth
  a product-level fix (defaulting local Mongo backups to archive mode, or making restore accept
  both) but out of scope for this benchmark PRD.
