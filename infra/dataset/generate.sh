#!/usr/bin/env bash
# infra/dataset/generate.sh — deterministic, size-targeted dataset generator
# for the Sentinel benchmark harness (scripts/benchmark.sh, PRD 41).
#
# Replaces the previous untracked, ad-hoc Python venv under infra/dataset/:
# this is a single committed, dependency-free (bash + awk + coreutils only)
# generator that produces a reproducible seed file per engine.
#
# Output per engine:
#   postgres / mysql / mariadb -> plain SQL: DROP+CREATE TABLE followed by
#                                 batched multi-row INSERT statements, loadable
#                                 via `psql -f` / `mysql <`.
#   mongo                      -> JSON Lines, loadable via
#                                 `mongoimport --file <path>`.
#
# Determinism: every row's payload is a deterministic slice of a fixed,
# repeating ASCII pattern (no randomness, no timestamps, no /dev/urandom).
# Re-running the generator with the same (--engine, --size-mb, --row-bytes,
# --batch) flags always produces a byte-identical file, on any machine —
# a prerequisite for benchmark numbers that are comparable across releases
# and machines.
#
# Size targeting is approximate (row count is derived from --size-mb and
# --row-bytes; real per-engine SQL/JSON syntax overhead adds a small,
# consistent percentage on top). The number that actually matters for the
# published matrix is the resulting Sentinel BACKUP ARTIFACT size, read
# from the monitor after the harness runs a real backup over this seed
# data — not the raw seed file size.
#
# Usage:
#   generate.sh --engine <postgres|mysql|mariadb|mongo> --size-mb <N> \
#               --out <path> [--row-bytes <N>] [--batch <N>] [--table <name>]
set -euo pipefail

ENGINE=""
SIZE_MB=""
OUT=""
ROW_BYTES=1024
BATCH=500
TABLE="bench_fixture"

usage() {
  cat <<'EOF'
Usage: generate.sh --engine <postgres|mysql|mariadb|mongo> --size-mb <N> --out <path> [options]

Required:
  --engine <name>     postgres | mysql | mariadb | mongo
  --size-mb <N>       approximate target size of the generated seed file, in MB
  --out <path>        output file path (parent dir created if missing)

Options:
  --row-bytes <N>     approximate payload bytes per row (default: 1024)
  --batch <N>         rows per multi-row INSERT statement, SQL engines only (default: 500)
  --table <name>      table/collection name (default: bench_fixture)
  -h, --help          show this help

Examples:
  generate.sh --engine postgres --size-mb 1024 --out .bench/dataset/postgres-small.sql
  generate.sh --engine mongo    --size-mb 1024 --out .bench/dataset/mongo-small.jsonl
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --engine)    ENGINE="$2"; shift 2 ;;
    --size-mb)   SIZE_MB="$2"; shift 2 ;;
    --out)       OUT="$2"; shift 2 ;;
    --row-bytes) ROW_BYTES="$2"; shift 2 ;;
    --batch)     BATCH="$2"; shift 2 ;;
    --table)     TABLE="$2"; shift 2 ;;
    -h|--help)   usage; exit 0 ;;
    *) echo "[dataset] unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "$ENGINE" || -z "$SIZE_MB" || -z "$OUT" ]]; then
  echo "[dataset] --engine, --size-mb and --out are required" >&2
  usage
  exit 1
fi

case "$ENGINE" in
  postgres|mysql|mariadb|mongo) ;;
  *) echo "[dataset] invalid --engine '$ENGINE' (want postgres|mysql|mariadb|mongo)" >&2; exit 1 ;;
esac

if ! [[ "$SIZE_MB" =~ ^[0-9]+$ ]] || [[ "$SIZE_MB" -lt 1 ]]; then
  echo "[dataset] --size-mb must be a positive integer" >&2
  exit 1
fi

TARGET_BYTES=$(( SIZE_MB * 1024 * 1024 ))
ROWS=$(( (TARGET_BYTES + ROW_BYTES - 1) / ROW_BYTES ))
[[ "$ROWS" -ge 1 ]] || ROWS=1

mkdir -p "$(dirname "$OUT")"

awk -v rows="$ROWS" -v rowbytes="$ROW_BYTES" -v batch="$BATCH" -v engine="$ENGINE" -v table="$TABLE" '
BEGIN {
  # Fixed, non-random repeating pattern -> deterministic payload of exactly
  # `rowbytes` bytes, independent of locale/time/machine.
  pat = "sentinel-benchmark-fixture-payload-0123456789-abcdefghijklmnopqrstuvwxyz-"
  pad = ""
  while (length(pad) < rowbytes) pad = pad pat
  pad = substr(pad, 1, rowbytes)
  q = sprintf("%c", 39) # single quote, avoids backslash-escaping headaches

  if (engine == "mongo") {
    for (i = 1; i <= rows; i++) {
      printf("{\"_id\": %d, \"payload\": \"%s\"}\n", i, pad)
    }
  } else {
    printf("DROP TABLE IF EXISTS %s;\n", table)
    printf("CREATE TABLE %s (id BIGINT PRIMARY KEY, payload TEXT NOT NULL);\n", table)
    line = ""
    count = 0
    for (i = 1; i <= rows; i++) {
      val = sprintf("(%d,%s%s%s)", i, q, pad, q)
      if (count == 0) {
        line = sprintf("INSERT INTO %s (id, payload) VALUES %s", table, val)
      } else {
        line = line "," val
      }
      count++
      if (count == batch) {
        print line ";"
        line = ""
        count = 0
      }
    }
    if (count > 0) print line ";"
  }
}
' > "$OUT"

ACTUAL_SIZE="$(du -h "$OUT" 2>/dev/null | cut -f1)"
echo "[dataset] engine=$ENGINE target=${SIZE_MB}MB rows=$ROWS row_bytes=$ROW_BYTES -> $OUT (${ACTUAL_SIZE:-unknown})"
