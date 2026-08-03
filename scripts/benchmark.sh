#!/usr/bin/env bash
# scripts/benchmark.sh — reproducible performance-benchmark harness (PRD 41).
#
# Reuses the same infra/docker DB services and sentinel-dev:local image as
# scripts/e2e.sh, generates a deterministic dataset via
# infra/dataset/generate.sh, runs real backup/restore scenarios through the
# `sentinel` CLI, and reads wall-clock + artifact-size metrics back from
# `sentinel monitor export` (no re-instrumentation of product code). Peak
# RAM + average CPU are the one thing the monitor does not already capture
# (Sentinel streams to an engine subprocess — pg_dump/mysqldump/mongodump —
# so measuring only the Go process would undercount): those are measured
# with GNU `time -v` run *inside* the Linux container over the whole
# invocation.
#
# Requires: docker, bash, awk. Must run on Linux (the container), NOT macOS
# directly — GNU `time -v` is not available on macOS and this script always
# executes `sentinel` inside the sentinel-dev:local container, so the host
# OS running this script does not matter as long as Docker is available;
# only a *native* (non-containerized) invocation of `/usr/bin/time -v
# sentinel ...` would require Linux.
#
# Usage:
#   scripts/benchmark.sh --tier small [--engine postgres|mysql|mariadb|mongo|all]
#                         [--size-mb N] [--with-incremental] [--skip-build]
#                         [--skip-infra] [--out-dir DIR] [--keep]
#
#   --tier small|large     small ~1GB (CI-safe), large 10/50GB (operator-run only)
#   --engine NAME|all      which engine(s) to benchmark (default: all)
#   --size-mb N            override the tier's default dataset size
#   --with-incremental     also attempt backup-incremental / restore-PITR
#                          scenarios (best-effort; skipped per-engine when the
#                          product's own prerequisite check fails, e.g.
#                          log_bin off, wal_summary off, standalone mongo)
#   --skip-build            reuse an existing sentinel-dev:local image
#   --skip-infra            assume `make infra-up` was already run
#   --out-dir DIR           where results are written (default: .bench)
#   --keep                  leave containers + workspace running after the run
#
# Output: DIR/results.csv, DIR/results.json, DIR/results.md — one row per
# (engine, dataset size, scenario). Report-only: this script never fails the
# process on a slow number, only on a hard operational error (see per-tier
# CI wiring in .github/workflows/benchmark.yml, which is workflow_dispatch
# only for the same reason).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INFRA_DIR="$PROJECT_ROOT/infra/docker"
DATASET_GEN="$PROJECT_ROOT/infra/dataset/generate.sh"
NETWORK="sentinel"
IMAGE="sentinel-dev:local"
MONGO_NAME="sentinel-mongo"

TIER="small"
ENGINE="all"
SIZE_MB=""
WITH_INCREMENTAL=false
SKIP_BUILD=false
SKIP_INFRA=false
OUT_DIR="$PROJECT_ROOT/.bench"
KEEP=false

usage() {
  sed -n '2,40p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tier)             TIER="$2"; shift 2 ;;
    --engine)            ENGINE="$2"; shift 2 ;;
    --size-mb)           SIZE_MB="$2"; shift 2 ;;
    --with-incremental)  WITH_INCREMENTAL=true; shift ;;
    --skip-build)        SKIP_BUILD=true; shift ;;
    --skip-infra)        SKIP_INFRA=true; shift ;;
    --out-dir)           OUT_DIR="$2"; shift 2 ;;
    --keep)              KEEP=true; shift ;;
    -h|--help)           usage; exit 0 ;;
    *) echo "[bench] unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

case "$TIER" in
  small) DEFAULT_SIZE_MB=1024 ;;
  large) DEFAULT_SIZE_MB=10240 ;;
  *) echo "[bench] --tier must be 'small' or 'large'" >&2; exit 1 ;;
esac
[[ -n "$SIZE_MB" ]] || SIZE_MB="$DEFAULT_SIZE_MB"

if [[ "$TIER" == "large" ]]; then
  echo "[bench] NOTE: --tier large is operator-run only (10/50GB is not CI-feasible)." >&2
  echo "[bench]       Make sure the Docker host has enough disk + RAM before proceeding." >&2
fi

case "$ENGINE" in
  postgres|mysql|mariadb|mongo|all) ;;
  *) echo "[bench] --engine must be one of postgres|mysql|mariadb|mongo|all" >&2; exit 1 ;;
esac
if [[ "$ENGINE" == "all" ]]; then
  ENGINES=(postgres mysql mariadb mongo)
else
  ENGINES=("$ENGINE")
fi

WORKSPACE="$OUT_DIR/workspace"
CONFIGS_DIR="$WORKSPACE/configs"
BACKUPS_DIR="$WORKSPACE/backups"
DATASET_DIR="$OUT_DIR/dataset"
HISTORY_DB="$WORKSPACE/.sentinel/history.db"
RESULTS_CSV="$OUT_DIR/results.csv"
RESULTS_JSON="$OUT_DIR/results.json"
RESULTS_MD="$OUT_DIR/results.md"

mkdir -p "$CONFIGS_DIR" "$BACKUPS_DIR" "$DATASET_DIR" "$(dirname "$HISTORY_DB")"

GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'
log()  { echo -e "${NC}[bench] $*"; }
warn() { echo -e "${YELLOW}[bench][WARN]${NC} $*"; }
err()  { echo -e "${RED}[bench][ERROR]${NC} $*"; }

upper() { printf '%s' "$1" | tr '[:lower:]' '[:upper:]'; }

SENTINEL_VERSION="$(cd "$PROJECT_ROOT" && grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' internal/version/*.go 2>/dev/null | head -1 || echo unknown)"

# ---------------------------------------------------------------------------
# Results accumulation (CSV + JSON Lines-ish array + Markdown table)
# ---------------------------------------------------------------------------
init_results() {
  echo "engine,dataset_size_mb,scenario,status,duration_ms,peak_rss_mb,cpu_pct,artifact_size_bytes,compression_ratio,note" > "$RESULTS_CSV"
  echo "[]" > "$RESULTS_JSON"
  {
    echo "| Engine | Dataset size | Scenario | Wall-clock | Peak RAM | CPU avg | Artifact size | Compression ratio | Note |"
    echo "|---|---|---|---|---|---|---|---|---|"
  } > "$RESULTS_MD"
}

record_result() {
  local engine="$1" size_mb="$2" scenario="$3" run_status="$4" duration_ms="$5" \
        peak_rss_mb="$6" cpu_pct="$7" artifact_bytes="$8" ratio="$9" note="${10}"

  echo "${engine},${size_mb},${scenario},${run_status},${duration_ms},${peak_rss_mb},${cpu_pct},${artifact_bytes},${ratio},\"${note}\"" >> "$RESULTS_CSV"
  append_json_result "$engine" "$size_mb" "$scenario" "$run_status" "$duration_ms" \
    "$peak_rss_mb" "$cpu_pct" "$artifact_bytes" "$ratio" "$note"

  local ms_disp="${duration_ms}"
  [[ "$duration_ms" != "n/a" ]] && ms_disp="$(awk -v ms="$duration_ms" 'BEGIN{printf "%.1fs", ms/1000}' 2>/dev/null || echo "$duration_ms")"
  echo "| $engine | ${size_mb}MB | $scenario | $run_status / $ms_disp | ${peak_rss_mb} | ${cpu_pct} | ${artifact_bytes} | ${ratio} | ${note} |" >> "$RESULTS_MD"
}

# Appends one row to the RESULTS_JSON array. Pure awk (no python/jq
# dependency) — keeps the harness to bash + awk + coreutils, matching
# infra/dataset/generate.sh.
append_json_result() {
  local engine="$1" size_mb="$2" scenario="$3" run_status="$4" duration_ms="$5" \
        peak_rss_mb="$6" cpu_pct="$7" artifact_bytes="$8" ratio="$9" note="${10}"
  local tmp; tmp="$(mktemp)"
  awk -v e="$engine" -v s="$size_mb" -v sc="$scenario" -v st="$run_status" -v d="$duration_ms" \
      -v p="$peak_rss_mb" -v c="$cpu_pct" -v a="$artifact_bytes" -v r="$ratio" -v n="$note" '
  BEGIN{
    row = sprintf("  {\"engine\":\"%s\",\"dataset_size_mb\":\"%s\",\"scenario\":\"%s\",\"status\":\"%s\",\"duration_ms\":\"%s\",\"peak_rss_mb\":\"%s\",\"cpu_pct\":\"%s\",\"artifact_size_bytes\":\"%s\",\"compression_ratio\":\"%s\",\"note\":\"%s\"}", e, s, sc, st, d, p, c, a, r, n)
    found = 0
  }
  NR==1 && $0=="[]" { print "["; print row; print "]"; found=1; next }
  /^\]$/ && !found { print "," row; print; found=1; next }
  { print }
  END { if (!found) { print "["; print row; print "]" } }
  ' "$RESULTS_JSON" > "$tmp" && mv "$tmp" "$RESULTS_JSON"
}

# ---------------------------------------------------------------------------
# Infra bring-up (reuses the same compose files / mongo run as the Makefile)
# ---------------------------------------------------------------------------
ensure_infra() {
  if [[ "$SKIP_INFRA" == true ]]; then
    log "skipping infra bring-up (--skip-infra)"
    return
  fi
  docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null

  local services=()
  for e in "${ENGINES[@]}"; do
    case "$e" in
      postgres) services+=(pgsql) ;;
      mysql)    services+=(mysql) ;;
      mariadb)  services+=(mariadb) ;;
    esac
  done
  if [[ ${#services[@]} -gt 0 ]]; then
    log "starting DB services: ${services[*]}"
    docker compose -f "$INFRA_DIR/docker-compose.yml" up -d "${services[@]}"
    bash "$SCRIPT_DIR/wait-healthy.sh" "${services[@]}" || true
  fi
  if [[ " ${ENGINES[*]} " == *" mongo "* ]]; then
    if ! docker inspect "$MONGO_NAME" >/dev/null 2>&1; then
      log "starting mongo..."
      docker run -d --name "$MONGO_NAME" --network "$NETWORK" --network-alias mongo -p 27017:27017 docker.io/mongo:noble >/dev/null
      sleep 3
    fi
  fi
}

ensure_image() {
  if [[ "$SKIP_BUILD" == true ]] && docker image inspect "$IMAGE" >/dev/null 2>&1; then
    log "reusing existing $IMAGE (--skip-build)"
    return
  fi
  if [[ ! -f "$INFRA_DIR/Dockerfile.dev" ]]; then
    err "$INFRA_DIR/Dockerfile.dev not found."
    err "Dockerfile.dev is an untracked, locally-maintained dev file (see scripts/e2e.sh precedent)."
    err "Create it locally (dev image = Go build + pg/mysql/mariadb/mongo client tools + bash), or"
    err "pass --skip-build if $IMAGE already exists from a prior 'make build-image' / e2e run."
    exit 1
  fi
  log "building $IMAGE..."
  docker build -f "$INFRA_DIR/Dockerfile.dev" -t "$IMAGE" "$PROJECT_ROOT"
}

# Run `sentinel <args>` inside the dev image, wrapped in GNU time -v when
# available (installed on the fly if missing — no Dockerfile.dev change
# required, since that file is untracked/operator-owned). Prints combined
# stdout+stderr; caller greps out the `time -v` fields it needs.
sentinel_timed() {
  docker run --rm \
    --network "$NETWORK" \
    --add-host=host.docker.internal:host-gateway \
    -e DEV_POSTGRES_PASSWORD=sentinel \
    -e DEV_MYSQL_PASSWORD=sentinel \
    -e DEV_MARIADB_PASSWORD=sentinel \
    -v "$WORKSPACE:/workspace" \
    -v "$CONFIGS_DIR:/configs" \
    -v "$DATASET_DIR:/dataset:ro" \
    --entrypoint sh \
    "$IMAGE" -c "
      if command -v /usr/bin/time >/dev/null 2>&1; then :
      else apk add --no-cache time >/dev/null 2>&1 || true
      fi
      if command -v /usr/bin/time >/dev/null 2>&1; then
        /usr/bin/time -v sentinel $* 2>&1
      else
        echo '[bench][WARN] GNU time unavailable in container (offline apk?) — peak RSS/CPU not measured' >&2
        sentinel $*
      fi
    " 2>&1
}

# Plain (untimed) sentinel invocation, e.g. for cheap `restore dry-run` checks.
sentinel_run() {
  docker run --rm \
    --network "$NETWORK" \
    --add-host=host.docker.internal:host-gateway \
    -e DEV_POSTGRES_PASSWORD=sentinel \
    -e DEV_MYSQL_PASSWORD=sentinel \
    -e DEV_MARIADB_PASSWORD=sentinel \
    -v "$WORKSPACE:/workspace" \
    -v "$CONFIGS_DIR:/configs" \
    -v "$DATASET_DIR:/dataset:ro" \
    "$IMAGE" \
    sentinel "$@"
}

# Load a generated seed file into the target engine's database.
load_dataset() {
  local engine="$1" seed_path="$2"
  local rel; rel="$(basename "$seed_path")"
  log "loading dataset into $engine ($rel)..."
  case "$engine" in
    postgres)
      docker run --rm --network "$NETWORK" -v "$DATASET_DIR:/dataset:ro" \
        -e PGPASSWORD=sentinel "$IMAGE" \
        psql -h pgsql -U sentinel -d sentinel -q -f "/dataset/$rel"
      ;;
    mysql)
      docker run --rm --network "$NETWORK" -v "$DATASET_DIR:/dataset:ro" \
        --entrypoint sh "$IMAGE" -c "mysql --skip-ssl -h mysql -u sentinel -psentinel sentinel < /dataset/$rel"
      ;;
    mariadb)
      docker run --rm --network "$NETWORK" -v "$DATASET_DIR:/dataset:ro" \
        --entrypoint sh "$IMAGE" -c "mysql --skip-ssl -h mariadb -u sentinel -psentinel sentinel < /dataset/$rel"
      ;;
    mongo)
      docker run --rm --network "$NETWORK" -v "$DATASET_DIR:/dataset:ro" "$IMAGE" \
        mongoimport --host mongo --db sentinel --collection bench_fixture --file "/dataset/$rel" --drop
      ;;
  esac
}

# Creates (fresh, dropped-then-created) the restore target database. Postgres
# uses the sentinel user directly (it is the POSTGRES_USER, a superuser on
# this image). MySQL/MariaDB's `sentinel` user only has privileges on the
# `sentinel` database by default (docker-compose MYSQL_DATABASE/MYSQL_USER
# grant), so the target db + grant is created via the root user instead.
# Mongo needs no pre-creation — mongorestore creates the db/collection.
prepare_restore_target() {
  local engine="$1" db_name="$2"
  case "$engine" in
    postgres)
      docker run --rm --network "$NETWORK" -e PGPASSWORD=sentinel "$IMAGE" \
        psql -h pgsql -U sentinel -c "DROP DATABASE IF EXISTS $db_name;" >/dev/null 2>&1
      docker run --rm --network "$NETWORK" -e PGPASSWORD=sentinel "$IMAGE" \
        psql -h pgsql -U sentinel -c "CREATE DATABASE $db_name;" >/dev/null 2>&1
      ;;
    mysql)
      docker run --rm --network "$NETWORK" --entrypoint sh "$IMAGE" -c \
        "mysql --skip-ssl -h mysql -u root -proot -e \"DROP DATABASE IF EXISTS $db_name; CREATE DATABASE $db_name; GRANT ALL PRIVILEGES ON $db_name.* TO 'sentinel'@'%'; FLUSH PRIVILEGES;\"" >/dev/null 2>&1
      ;;
    mariadb)
      docker run --rm --network "$NETWORK" --entrypoint sh "$IMAGE" -c \
        "mysql --skip-ssl -h mariadb -u root -proot -e \"DROP DATABASE IF EXISTS $db_name; CREATE DATABASE $db_name; GRANT ALL PRIVILEGES ON $db_name.* TO 'sentinel'@'%'; FLUSH PRIVILEGES;\"" >/dev/null 2>&1
      ;;
    mongo) : ;;
  esac
}

# ---------------------------------------------------------------------------
# Per-engine prerequisite probes for incremental/PITR scenarios (mirrors the
# same checks internal/domain/backup/incremental/prerequisites.go performs,
# so the harness fails soft with an actionable message instead of a raw
# pg_dump/mysqldump error).
# ---------------------------------------------------------------------------
postgres_wal_summary_enabled() {
  docker run --rm --network "$NETWORK" -e PGPASSWORD=sentinel "$IMAGE" \
    psql -h pgsql -U sentinel -d sentinel -tAc "SHOW wal_summary;" 2>/dev/null | tr -d '[:space:]' | grep -qi '^on$'
}

mysql_log_bin_enabled() {
  local host="$1"
  docker run --rm --network "$NETWORK" --entrypoint sh "$IMAGE" -c \
    "mysql -h $host -u sentinel -psentinel -N -B -e \"SHOW VARIABLES LIKE 'log_bin';\"" 2>/dev/null \
    | awk '{print $2}' | grep -qi '^on$'
}

# ---------------------------------------------------------------------------
# Config templates (one database per file, so `monitor export --job` isolates
# the run cleanly — mirrors the per-suite pattern in scripts/e2e.sh).
# ---------------------------------------------------------------------------
write_backup_config() {
  local path="$1" engine="$2" job="$3" outfile="$4" incremental="$5"
  local inc_block=""
  if [[ "$incremental" == true ]]; then
    case "$engine" in
      postgres) inc_block=$'    incremental_backup:\n      enabled: true\n      wal_summary_check: true\n' ;;
      mysql|mariadb) inc_block=$'    incremental_backup:\n      enabled: true\n      binlog_check: true\n    mysql:\n      binlog_path: /workspace/binlogs\n' ;;
    esac
  fi

  case "$engine" in
    postgres)
      cat > "$path" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  $job:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: $outfile
$inc_block
EOF
      ;;
    mysql|mariadb)
      local host="mysql"
      [[ "$engine" == "mariadb" ]] && host="mariadb"
      cat > "$path" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  $job:
    type: $engine
    host: $host
    port: 3306
    username: sentinel
    password_env: DEV_$(upper "$engine")_PASSWORD
    database: sentinel
    output: $outfile
$inc_block
EOF
      ;;
    mongo)
      cat > "$path" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  $job:
    type: mongodb
    uri: "mongodb://mongo:27017"
    database: sentinel
    output: $outfile
    # Local mongo backups default to mongodump's directory mode (--out=);
    # Sentinel's mongo RESTORE path always invokes mongorestore --archive=
    # (internal/adapters/restore/mongo/mongo_restore.go), so a local backup
    # is only restorable if it was captured in single-file archive mode.
    database_options:
      archive: true
EOF
      ;;
  esac
}

write_restore_config() {
  local path="$1" engine="$2" backup_job="$3" restore_job="$4" outfile="$5" restore_db="$6"
  case "$engine" in
    postgres)
      cat > "$path" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  $backup_job:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: $outfile

restores:
  $restore_job:
    enabled: true
    schedule: "0 3 * * *"
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: $restore_db
    staging_dir: /workspace/staging
    backup_source:
      type: local
      local_path: /workspace/backups
      backup_path: $outfile
EOF
      ;;
    mysql|mariadb)
      local host="mysql"
      [[ "$engine" == "mariadb" ]] && host="mariadb"
      cat > "$path" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  $backup_job:
    type: $engine
    host: $host
    port: 3306
    username: sentinel
    password_env: DEV_$(upper "$engine")_PASSWORD
    database: sentinel
    output: $outfile

restores:
  $restore_job:
    enabled: true
    schedule: "0 3 * * *"
    type: $engine
    host: $host
    port: 3306
    username: sentinel
    password_env: DEV_$(upper "$engine")_PASSWORD
    database: $restore_db
    staging_dir: /workspace/staging
    restore_options:
      additional_args: "--skip-ssl"
    backup_source:
      type: local
      local_path: /workspace/backups
      backup_path: $outfile
EOF
      ;;
    mongo)
      cat > "$path" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  $backup_job:
    type: mongodb
    uri: "mongodb://mongo:27017"
    database: sentinel
    output: $outfile

restores:
  $restore_job:
    enabled: true
    schedule: "0 3 * * *"
    type: mongodb
    uri: "mongodb://mongo:27017"
    database: $restore_db
    staging_dir: /workspace/staging
    backup_source:
      type: local
      local_path: /workspace/backups
      backup_path: $outfile
EOF
      ;;
  esac
}

# ---------------------------------------------------------------------------
# Metrics extraction
# ---------------------------------------------------------------------------
extract_peak_rss_mb() {
  # GNU time -v: "Maximum resident set size (kbytes): NNN"
  local kb
  kb="$(grep -oE 'Maximum resident set size \(kbytes\): [0-9]+' <<<"$1" | grep -oE '[0-9]+$' || true)"
  if [[ -n "$kb" ]]; then awk -v k="$kb" 'BEGIN{printf "%.1f", k/1024}'; else echo "n/a"; fi
}

extract_cpu_pct() {
  # GNU time -v: "Percent of CPU this job got: NN%"
  grep -oE 'Percent of CPU this job got: [0-9]+' <<<"$1" | grep -oE '[0-9]+$' || echo "n/a"
}

# Restore executions are recorded in the monitor's separate
# `restore_executions` table (`sentinel restore history`, text-only, no
# --format json/csv), not the `executions` table `monitor export` reads from
# — so restore wall-clock is read from the same `time -v` wrapper already
# used for peak RSS/CPU, rather than the monitor. Handles both time(1)
# variants seen in practice: GNU coreutils "h:mm:ss.cc" / "m:ss.cc", and the
# Alpine `time` package's "Xm Y.YYs".
extract_elapsed_ms() {
  local line
  line="$(grep -oE 'Elapsed \(wall clock\) time \(h:mm:ss or m:ss\): [0-9:. ms]+' <<<"$1" | sed -E 's/.*\): //')"
  [[ -n "$line" ]] || { echo "n/a"; return; }
  if [[ "$line" =~ ^([0-9]+)m[[:space:]]+([0-9.]+)s$ ]]; then
    awk -v m="${BASH_REMATCH[1]}" -v s="${BASH_REMATCH[2]}" 'BEGIN{printf "%.0f", (m*60+s)*1000}'
  elif [[ "$line" =~ ^([0-9]+):([0-9]+):([0-9.]+)$ ]]; then
    awk -v h="${BASH_REMATCH[1]}" -v m="${BASH_REMATCH[2]}" -v s="${BASH_REMATCH[3]}" 'BEGIN{printf "%.0f", (h*3600+m*60+s)*1000}'
  elif [[ "$line" =~ ^([0-9]+):([0-9.]+)$ ]]; then
    awk -v m="${BASH_REMATCH[1]}" -v s="${BASH_REMATCH[2]}" 'BEGIN{printf "%.0f", (m*60+s)*1000}'
  else
    echo "n/a"
  fi
}

# Reads duration_ms + file_size_bytes for the most recent execution of `job`
# from `sentinel monitor export --format csv --job <job>` (columns per
# internal/adapters/monitor/export.go: id,backup_name,database_type,
# timestamp,duration_ms,status,error_message,storage_backend,file_path,
# file_size_bytes,checksum,backup_type,chain_id,chain_index,
# delta_size_bytes,full_backup_size_bytes,created_at).
#
# NOTE: `sentinel monitor export` (no --output) prints its data via Cobra's
# cmd.Println, which Cobra sends to OutOrStderr() — i.e. this CLI writes
# export data to STDERR, not stdout (verified empirically; a pre-existing
# CLI quirk, out of scope to change here). So we capture combined
# stdout+stderr and pick the last well-formed CSV row rather than relying on
# stdout alone.
read_monitor_metrics() {
  local job="$1" config_rel="$2"
  local csv
  csv="$(sentinel_run monitor export --config "$config_rel" --format csv --job "$job" 2>&1)"
  # last data row (most recent execution), field 5 = duration_ms, field 10 = file_size_bytes
  local last_row
  last_row="$(echo "$csv" | grep -E '^[0-9a-f-]{36},' | tail -1)"
  if [[ -z "$last_row" ]]; then
    echo "n/a n/a"
    return
  fi
  awk -F',' '{print $5, $10}' <<<"$last_row"
}

# ---------------------------------------------------------------------------
# Scenario runner
# ---------------------------------------------------------------------------
run_engine() {
  local engine="$1"
  local seed_ext="sql"
  [[ "$engine" == "mongo" ]] && seed_ext="jsonl"
  local seed_path="$DATASET_DIR/${engine}-${TIER}.${seed_ext}"
  local tag="${engine}-${TIER}"

  log "=== engine=$engine tier=$TIER size=${SIZE_MB}MB ==="

  "$DATASET_GEN" --engine "$engine" --size-mb "$SIZE_MB" --out "$seed_path" || {
    err "$engine: dataset generation failed"
    record_result "$engine" "$SIZE_MB" "backup-full" "error" n/a n/a n/a n/a n/a "dataset generation failed"
    return
  }

  if ! load_dataset "$engine" "$seed_path"; then
    err "$engine: dataset load failed (is the DB service up? see --skip-infra)"
    record_result "$engine" "$SIZE_MB" "backup-full" "error" n/a n/a n/a n/a n/a "dataset load failed"
    return
  fi

  # --- backup full ---
  local out_full="${tag}.sql"
  [[ "$engine" == "mongo" ]] && out_full="${tag}.archive"
  local cfg_full="$CONFIGS_DIR/${tag}-backup-full.yaml"
  write_backup_config "$cfg_full" "$engine" "${tag}-full" "$out_full" false

  local t_out
  t_out="$(sentinel_timed backup --config "/configs/$(basename "$cfg_full")")"
  local rc=$?
  local peak cpu dur size
  peak="$(extract_peak_rss_mb "$t_out")"
  cpu="$(extract_cpu_pct "$t_out")"
  read -r dur size < <(read_monitor_metrics "${tag}-full" "/configs/$(basename "$cfg_full")")

  if [[ $rc -eq 0 && "$dur" != "n/a" ]]; then
    record_result "$engine" "$SIZE_MB" "backup-full" "ok" "$dur" "$peak" "$cpu" "$size" "n/a" ""
    ok_backup_full=true
  else
    warn "$engine: backup-full did not complete cleanly (rc=$rc)"
    echo "$t_out" | tail -20 | sed 's/^/    /'
    record_result "$engine" "$SIZE_MB" "backup-full" "error" n/a "$peak" "$cpu" n/a n/a "backup command failed, see harness log"
    ok_backup_full=false
  fi

  # --- backup full, with pipeline compression enabled (PRD 33) — used to
  # derive a compression ratio generically across all four engines, since
  # spec-049 pipeline compression is engine-agnostic. ---
  if [[ "$ok_backup_full" == true ]]; then
    local out_comp="${tag}-zstd.sql"
    [[ "$engine" == "mongo" ]] && out_comp="${tag}-zstd.archive"
    local cfg_comp="$CONFIGS_DIR/${tag}-backup-compressed.yaml"
    write_backup_config "$cfg_comp" "$engine" "${tag}-comp" "$out_comp" false
    # append compression block
    printf '    compression:\n      enabled: true\n      algorithm: zstd\n' >> "$cfg_comp"

    local c_out
    c_out="$(sentinel_timed backup --config "/configs/$(basename "$cfg_comp")")"
    local crc=$?
    local cdur csize
    read -r cdur csize < <(read_monitor_metrics "${tag}-comp" "/configs/$(basename "$cfg_comp")")
    if [[ $crc -eq 0 && "$csize" != "n/a" && "$size" != "n/a" && "$csize" -gt 0 ]]; then
      local ratio
      ratio="$(awk -v a="$size" -v b="$csize" 'BEGIN{printf "%.2fx", a/b}')"
      record_result "$engine" "$SIZE_MB" "backup-full-compressed" "ok" "$cdur" "n/a" "n/a" "$csize" "$ratio" "ratio = uncompressed_artifact/compressed_artifact"
    else
      record_result "$engine" "$SIZE_MB" "backup-full-compressed" "error" n/a n/a n/a n/a n/a "compressed backup failed or produced empty artifact"
    fi
  fi

  # --- backup incremental (best-effort, --with-incremental only) ---
  if [[ "$WITH_INCREMENTAL" == true ]]; then
    local can_incremental=false
    local skip_reason=""
    case "$engine" in
      postgres)
        if postgres_wal_summary_enabled; then can_incremental=true
        else skip_reason="wal_summary is off on the pgsql service (needs summarize_wal=on in postgresql.conf, PG17+); not enabled by infra/docker/docker-compose.yml by default"
        fi
        ;;
      mysql)
        if mysql_log_bin_enabled mysql; then can_incremental=true
        else skip_reason="log_bin is off on the mysql service; not enabled by infra/docker/docker-compose.yml by default (needs --log-bin server arg + a locally-mounted binlog_path)"
        fi
        ;;
      mariadb)
        if mysql_log_bin_enabled mariadb; then can_incremental=true
        else skip_reason="log_bin is off on the mariadb service; not enabled by infra/docker/docker-compose.yml by default (needs --log-bin server arg + a locally-mounted binlog_path)"
        fi
        ;;
      mongo)
        skip_reason="mongo service is a standalone node (no replica set); oplog-based incremental backup requires a replica set (ValidateMongoIncrementalPrerequisites)"
        ;;
    esac

    if [[ "$can_incremental" == true ]]; then
      local out_inc="${tag}-inc.sql"
      local cfg_inc="$CONFIGS_DIR/${tag}-backup-incremental.yaml"
      write_backup_config "$cfg_inc" "$engine" "${tag}-inc" "$out_inc" true
      local i_out
      i_out="$(sentinel_timed backup --config "/configs/$(basename "$cfg_inc")")"
      local irc=$?
      local idur isize
      read -r idur isize < <(read_monitor_metrics "${tag}-inc" "/configs/$(basename "$cfg_inc")")
      if [[ $irc -eq 0 && "$idur" != "n/a" ]]; then
        record_result "$engine" "$SIZE_MB" "backup-incremental" "ok" "$idur" "n/a" "n/a" "$isize" "n/a" ""
      else
        record_result "$engine" "$SIZE_MB" "backup-incremental" "error" n/a n/a n/a n/a n/a "prerequisites reported enabled but the run failed; see harness log"
      fi
    else
      warn "$engine: skipping backup-incremental — $skip_reason"
      record_result "$engine" "$SIZE_MB" "backup-incremental" "skipped" n/a n/a n/a n/a n/a "$skip_reason"
    fi
  fi

  # --- restore full ---
  if [[ "$ok_backup_full" == true ]]; then
    local restore_db="sentinel_bench_restore"
    prepare_restore_target "$engine" "$restore_db"
    local cfg_restore="$CONFIGS_DIR/${tag}-restore-full.yaml"
    write_restore_config "$cfg_restore" "$engine" "${tag}-full" "${tag}-restore" "$out_full" "$restore_db"

    local r_out
    r_out="$(sentinel_timed restore run "${tag}-restore" --config "/configs/$(basename "$cfg_restore")")"
    local rrc=$?
    local rpeak rcpu
    rpeak="$(extract_peak_rss_mb "$r_out")"
    rcpu="$(extract_cpu_pct "$r_out")"
    # restore_executions has no CSV/JSON export (sentinel restore history is
    # text-only) — wall-clock comes from the same time -v wrapper instead.
    local rdur="$(extract_elapsed_ms "$r_out")" rsize="n/a"

    if [[ $rrc -eq 0 ]]; then
      record_result "$engine" "$SIZE_MB" "restore-full" "ok" "${rdur:-n/a}" "$rpeak" "$rcpu" "${rsize:-n/a}" "n/a" "duration measured via time -v (restore_executions has no CSV export)"
    else
      warn "$engine: restore-full did not complete cleanly (rc=$rrc)"
      echo "$r_out" | tail -20 | sed 's/^/    /'
      local rnote="restore command failed, see harness log"
      if grep -q 'mongosh.*not found' <<<"$r_out"; then
        rnote="mongo restore preflight requires 'mongosh' in the runtime image; Alpine (sentinel-dev:local's base) has no mongosh package — add it to Dockerfile.dev (e.g. a Debian-based variant, or the mongosh tarball) to exercise this scenario"
      fi
      record_result "$engine" "$SIZE_MB" "restore-full" "error" n/a "$rpeak" "$rcpu" n/a n/a "$rnote"
    fi
  else
    record_result "$engine" "$SIZE_MB" "restore-full" "skipped" n/a n/a n/a n/a n/a "backup-full did not succeed"
  fi

  # --- restore PITR (postgres) / restore incremental (mysql/mariadb) ---
  if [[ "$WITH_INCREMENTAL" == true ]]; then
    case "$engine" in
      postgres)
        record_result "$engine" "$SIZE_MB" "restore-pitr" "skipped" n/a n/a n/a n/a n/a \
          "requires continuous WAL archiving (archive_mode=on + archive_command) on the pgsql service, beyond generic docker-compose infra; operator-run only"
        ;;
      mysql|mariadb)
        record_result "$engine" "$SIZE_MB" "restore-pitr" "skipped" n/a n/a n/a n/a n/a \
          "requires archived binlog artifacts (mysql.binlog_path) + log_bin enabled on the server, beyond generic docker-compose infra; operator-run only"
        ;;
      mongo)
        record_result "$engine" "$SIZE_MB" "restore-pitr" "skipped" n/a n/a n/a n/a n/a \
          "requires a replica set for oplog replay; standalone mongo service does not support it; operator-run only"
        ;;
    esac
  fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
init_results
ensure_infra
ensure_image

for e in "${ENGINES[@]}"; do
  run_engine "$e"
done

log "results written to:"
log "  $RESULTS_CSV"
log "  $RESULTS_JSON"
log "  $RESULTS_MD"
cat "$RESULTS_MD"

if [[ "$KEEP" == false && "$SKIP_INFRA" == false ]]; then
  log "tearing down infra (pass --keep to leave it running)..."
  docker compose -f "$INFRA_DIR/docker-compose.yml" down >/dev/null 2>&1 || true
  docker rm -f "$MONGO_NAME" >/dev/null 2>&1 || true
fi

exit 0
