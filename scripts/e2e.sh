#!/usr/bin/env bash
# End-to-end validation script for Sentinel M1.
# Requires: docker, sentinel binary in PATH or SENTINEL_BIN env var.
# Usage: ./scripts/e2e.sh [--skip-build] [--keep]
#   --skip-build  skip docker image build (use existing sentinel-dev:local)
#   --keep        keep containers running after the run

set -euo pipefail

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INFRA_DIR="$PROJECT_ROOT/infra/docker"
DATASET_DIR="$PROJECT_ROOT/infra/dataset"
WORKSPACE="$PROJECT_ROOT/.e2e"
HISTORY_DB="$WORKSPACE/.sentinel/history.db"
BACKUPS_DIR="$WORKSPACE/backups"
CONFIGS_DIR="$WORKSPACE/configs"
IMAGE="sentinel-dev:local"
NETWORK="sentinel"

SKIP_BUILD=false
KEEP_RUNNING=false
for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=true ;;
    --keep)       KEEP_RUNNING=true ;;
  esac
done

# Azurite connection string (well-known dev key)
AZURITE_CONN_STRING="DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tiq/K+Y/3cAoRQ==;BlobEndpoint=http://host.docker.internal:10000/devstoreaccount1;"

# RustFS / S3 creds (matches infra/dataset/.env)
S3_ENDPOINT="http://host.docker.internal:9000"
S3_ACCESS_KEY="minioadmin"
S3_SECRET_KEY="minioadmin"
S3_BUCKET="sentinel-e2e"
S3_REGION="us-east-1"

GCS_ENDPOINT="http://host.docker.internal:4443/storage/v1/"
GCS_BUCKET="sentinel-e2e"
GCS_PROJECT="sentinel-e2e-project"

PASS=0
FAIL=0
SKIPPED=0

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${NC}[e2e] $*"; }
ok()   { echo -e "${GREEN}[PASS]${NC} $*"; PASS=$((PASS+1)); }
fail() { echo -e "${RED}[FAIL]${NC} $*"; FAIL=$((FAIL+1)); }
skip() { echo -e "${YELLOW}[SKIP]${NC} $*"; SKIPPED=$((SKIPPED+1)); }

# Run sentinel inside sentinel-dev container, sharing workspace and env.
sentinel() {
  docker run --rm \
    --network "$NETWORK" \
    --add-host=host.docker.internal:host-gateway \
    -e DEV_POSTGRES_PASSWORD=sentinel \
    -e DEV_MYSQL_PASSWORD=sentinel \
    -e DEV_MARIADB_PASSWORD=sentinel \
    -e SENTINEL_S3_ACCESS_KEY="$S3_ACCESS_KEY" \
    -e SENTINEL_S3_SECRET_KEY="$S3_SECRET_KEY" \
    -e SENTINEL_AZURE_CONN_STRING="$AZURITE_CONN_STRING" \
    -e STORAGE_EMULATOR_HOST="http://host.docker.internal:4443" \
    -v "$WORKSPACE:/workspace" \
    -v "$CONFIGS_DIR:/configs" \
    "$IMAGE" \
    sentinel "$@"
}

assert_exit_ok() {
  local label="$1"; shift
  local out
  if out=$(sentinel "$@" 2>&1); then
    ok "$label"
  else
    fail "$label (exit non-zero)"
    printf '%s\n' "$out" | tail -15 | sed 's/^/  [cmd] /'
  fi
}

assert_file_nonempty() {
  local label="$1"
  local path="$2"
  if [[ -s "$path" ]]; then
    ok "$label"
  else
    fail "$label (file empty or missing: $path)"
  fi
}

assert_output_contains() {
  local label="$1"
  local needle="$2"; shift 2
  local out
  out=$(sentinel "$@" 2>&1)
  if echo "$out" | grep -q "$needle"; then
    ok "$label"
  else
    fail "$label (expected '$needle' in output)"
    echo "    got: $out" >&2
  fi
}

wait_healthy() {
  local service="$1"
  local max=30
  log "Waiting for $service to be healthy..."
  for i in $(seq 1 $max); do
    status=$(docker inspect --format='{{.State.Health.Status}}' "$(docker compose -f "$INFRA_DIR/docker-compose.e2e.yml" ps -q "$service" 2>/dev/null)" 2>/dev/null || echo "missing")
    if [[ "$status" == "healthy" ]]; then
      log "$service is healthy"
      return 0
    fi
    sleep 2
  done
  log "WARNING: $service did not become healthy in time"
}

# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------
setup() {
  log "=== Setup ==="

  log "Creating Docker network '$NETWORK' if needed..."
  docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK"

  log "Starting e2e storage emulators..."
  docker compose -f "$INFRA_DIR/docker-compose.e2e.yml" up -d
  wait_healthy azurite
  wait_healthy fake-gcs

  log "Creating workspace directories..."
  mkdir -p "$BACKUPS_DIR" "$CONFIGS_DIR" "$WORKSPACE/.sentinel" "$WORKSPACE/staging"

  if [[ "$SKIP_BUILD" == false ]]; then
    log "Building $IMAGE..."
    docker build -f "$INFRA_DIR/Dockerfile.dev" -t "$IMAGE" "$PROJECT_ROOT"
  fi

  # Create GCS bucket in fake-gcs-server
  log "Creating GCS bucket '$GCS_BUCKET' in fake-gcs-server..."
  curl -sf -X POST "http://localhost:4443/storage/v1/b?project=$GCS_PROJECT" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"$GCS_BUCKET\"}" >/dev/null 2>&1 || true

  # Create S3 bucket in RustFS (best-effort)
  log "Creating S3 bucket '$S3_BUCKET' in RustFS..."
  docker run --rm --network "$NETWORK" \
    --add-host=host.docker.internal:host-gateway \
    --entrypoint sh \
    amazon/aws-cli:latest -c \
    "AWS_ACCESS_KEY_ID=$S3_ACCESS_KEY AWS_SECRET_ACCESS_KEY=$S3_SECRET_KEY \
     aws --endpoint-url $S3_ENDPOINT --region $S3_REGION s3 mb s3://$S3_BUCKET 2>/dev/null || true" 2>/dev/null || true

  generate_configs
}

teardown() {
  if [[ "$KEEP_RUNNING" == false ]]; then
    log "Stopping e2e containers..."
    docker compose -f "$INFRA_DIR/docker-compose.e2e.yml" down -v
  fi
  log "Cleaning workspace..."
  rm -rf "$WORKSPACE"
}

# ---------------------------------------------------------------------------
# Config generation
# ---------------------------------------------------------------------------
generate_configs() {
  log "Generating e2e configs..."

  # --- local backend ---
  cat > "$CONFIGS_DIR/local.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  postgres-e2e:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-e2e

  mysql-e2e:
    type: mysql
    host: mysql
    port: 3306
    username: sentinel
    password_env: DEV_MYSQL_PASSWORD
    database: sentinel
    output: mysql-e2e

  mariadb-e2e:
    type: mariadb
    host: mariadb
    port: 3306
    username: sentinel
    password_env: DEV_MARIADB_PASSWORD
    database: sentinel
    output: mariadb-e2e

  mongo-e2e:
    type: mongodb
    uri: "mongodb://mongo:27017"
    database: sentinel
    output: mongo-e2e
EOF

  # --- s3 backend ---
  cat > "$CONFIGS_DIR/s3.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: s3
    s3_bucket: $S3_BUCKET
    s3_bucket_endpoint: $S3_ENDPOINT
    s3_region: $S3_REGION
    s3_access_key_id_env: SENTINEL_S3_ACCESS_KEY
    s3_secret_access_key_env: SENTINEL_S3_SECRET_KEY

databases:
  postgres-s3:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-s3
EOF

  # --- azure backend (Azurite well-known dev key) ---
  # StorageConfig flat fields: azure_storage_account + azure_storage_key + azure_container
  AZURITE_ACCOUNT_KEY="Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tiq/K+Y/3cAoRQ=="
  cat > "$CONFIGS_DIR/azure.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: azure
    azure_storage_account: devstoreaccount1
    azure_storage_key: ${AZURITE_ACCOUNT_KEY}
    azure_container: sentinel-e2e

databases:
  postgres-azure:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-azure
EOF

  # --- gcs backend ---
  cat > "$CONFIGS_DIR/gcs.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: gcs
    gcs_bucket: $GCS_BUCKET
    gcs_project_id: $GCS_PROJECT

databases:
  postgres-gcs:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-gcs
EOF
}

# ---------------------------------------------------------------------------
# Test suites
# ---------------------------------------------------------------------------
test_unit() {
  log ""
  log "=== Suite: go test + go vet ==="
  if (cd "$PROJECT_ROOT" && go test ./... 2>&1); then
    ok "go test ./..."
  else
    fail "go test ./..."
  fi
  if (cd "$PROJECT_ROOT" && go vet ./... 2>&1); then
    ok "go vet ./..."
  else
    fail "go vet ./..."
  fi
}

test_config_validate() {
  log ""
  log "=== Suite: config validate ==="
  for cfg in local s3 azure gcs; do
    assert_exit_ok "config validate [$cfg]" config validate --config "/configs/$cfg.yaml"
  done
}

test_local_backup() {
  log ""
  log "=== Suite: backup — local backend ==="
  assert_exit_ok "backup local (all engines)" backup --config /configs/local.yaml

  assert_file_nonempty "postgres-e2e.sql non-empty"   "$BACKUPS_DIR/postgres-e2e.sql"
  assert_file_nonempty "mysql-e2e.sql non-empty"      "$BACKUPS_DIR/mysql-e2e.sql"
  assert_file_nonempty "mariadb-e2e.sql non-empty"    "$BACKUPS_DIR/mariadb-e2e.sql"

  if [[ -f "$BACKUPS_DIR/mongo-e2e" ]]; then
    ok "mongo-e2e archive present"
  else
    skip "mongo-e2e: archive missing (mongodump may not be available)"
  fi

  # content sanity
  if grep -q "PostgreSQL database dump" "$BACKUPS_DIR/postgres-e2e.sql" 2>/dev/null; then
    ok "postgres dump header valid"
  else
    fail "postgres dump header missing"
  fi
  if grep -q "MySQL dump\|MariaDB dump\|Dump completed" "$BACKUPS_DIR/mysql-e2e.sql" 2>/dev/null; then
    ok "mysql dump header valid"
  else
    fail "mysql dump header missing"
  fi
}

test_monitor() {
  log ""
  log "=== Suite: monitor ==="
  assert_exit_ok "monitor list (last 1h)" \
    monitor list --config /configs/local.yaml --last 1h
  assert_exit_ok "monitor stats" \
    monitor stats --config /configs/local.yaml --job postgres-e2e
  assert_exit_ok "monitor export json" \
    monitor export --config /configs/local.yaml --format json \
    --output /workspace/backups/history.json
  assert_file_nonempty "monitor export file" "$BACKUPS_DIR/history.json"
}

test_retention() {
  log ""
  log "=== Suite: retention ==="
  assert_exit_ok "retention preview" \
    retention preview --config /configs/local.yaml
  assert_exit_ok "retention apply --dry-run" \
    retention apply --config /configs/local.yaml --dry-run
}

# test_retention_gfs exercises the Grandfather-Father-Son retention path
# end-to-end: seed a synthetic multi-day backup history + files, run a real
# (non-dry-run) apply with a keep_daily:1 policy, and assert exactly the newest
# daily anchor survives. History seeding needs the sqlite3 CLI on the host; the
# real-apply assertion is skipped gracefully when it is absent.
test_retention_gfs() {
  log ""
  log "=== Suite: retention (GFS) ==="

  local gfs_dir="$BACKUPS_DIR/gfs"
  mkdir -p "$gfs_dir"
  printf 'd0' > "$gfs_dir/d0.sql"
  printf 'd1' > "$gfs_dir/d1.sql"
  printf 'd2' > "$gfs_dir/d2.sql"

  cat > "$CONFIGS_DIR/gfs.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  gfs-e2e:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: gfs-e2e
    retention:
      gfs:
        keep_daily: 1
EOF

  # Config must load/validate with a GFS-only retention block.
  assert_exit_ok "gfs config validate" config validate --config /configs/gfs.yaml
  # Dry-run apply must be processed (GFS-only job not skipped) — exit 0.
  assert_exit_ok "gfs retention apply --dry-run" \
    retention apply --config /configs/gfs.yaml --job gfs-e2e --dry-run

  if ! command -v sqlite3 >/dev/null 2>&1; then
    skip "gfs real-apply: sqlite3 not available to seed history"
    return
  fi

  # Seed three success rows across three distinct calendar days (UTC). File
  # paths are the absolute container paths the local backend deletes.
  sqlite3 "$HISTORY_DB" <<SQL
INSERT INTO backup_executions (id, backup_name, database_type, timestamp, status, storage_backend, file_path, file_size_bytes, created_at) VALUES
 ('gfs-d0','gfs-e2e','postgres','2026-08-02 06:00:00+00:00','success','local','/workspace/backups/gfs/d0.sql',2,'2026-08-02 06:00:00+00:00'),
 ('gfs-d1','gfs-e2e','postgres','2026-08-01 06:00:00+00:00','success','local','/workspace/backups/gfs/d1.sql',2,'2026-08-01 06:00:00+00:00'),
 ('gfs-d2','gfs-e2e','postgres','2026-07-31 06:00:00+00:00','success','local','/workspace/backups/gfs/d2.sql',2,'2026-07-31 06:00:00+00:00');
SQL

  # Preview should attribute the two non-anchor deletions to GFS.
  assert_output_contains "gfs preview shows gfs reason" "not retained by gfs" \
    retention preview --config /configs/gfs.yaml --job gfs-e2e

  # Real apply: keep_daily:1 keeps only the newest day's backup (d0).
  assert_exit_ok "gfs retention apply (real)" \
    retention apply --config /configs/gfs.yaml --job gfs-e2e

  assert_file_nonempty "gfs kept newest daily anchor (d0)" "$gfs_dir/d0.sql"
  if [[ ! -f "$gfs_dir/d1.sql" && ! -f "$gfs_dir/d2.sql" ]]; then
    ok "gfs pruned non-anchor days (d1, d2)"
  else
    fail "gfs expected d1.sql and d2.sql deleted (d1 exists: $([[ -f "$gfs_dir/d1.sql" ]] && echo yes || echo no), d2 exists: $([[ -f "$gfs_dir/d2.sql" ]] && echo yes || echo no))"
  fi
}

test_restore_dryrun() {
  log ""
  log "=== Suite: restore dry-run ==="

  # restore config requires at least one backup job (databases:) to load
  cat > "$CONFIGS_DIR/restore.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  postgres-e2e:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-e2e

restores:
  pg-e2e-restore:
    type: postgres
    enabled: false
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    staging_dir: /workspace/staging
    backup_source:
      type: local
      local_path: /workspace/backups
      backup_path: postgres-e2e.sql
EOF

  assert_exit_ok "restore list" \
    restore list --config /configs/restore.yaml
  assert_exit_ok "restore dry-run pg-e2e-restore" \
    restore dry-run pg-e2e-restore --config /configs/restore.yaml
}

test_encryption() {
  log ""
  log "=== Suite: encryption ==="

  local key
  key=$(docker run --rm "$IMAGE" sentinel security init-key 2>/dev/null | grep 'export SENTINEL_MASTER_KEY=' | cut -d'"' -f2)

  if [[ -z "$key" ]]; then
    fail "security init-key returned empty key"
    return
  fi
  ok "security init-key"

  cat > "$CONFIGS_DIR/encrypted.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db
encryption_key_env: SENTINEL_MASTER_KEY

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  postgres-enc:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-encrypted
EOF

  if docker run --rm \
    --network "$NETWORK" \
    --add-host=host.docker.internal:host-gateway \
    -e DEV_POSTGRES_PASSWORD=sentinel \
    -e SENTINEL_MASTER_KEY="$key" \
    -v "$WORKSPACE:/workspace" \
    -v "$CONFIGS_DIR:/configs" \
    "$IMAGE" \
    sentinel backup --config /configs/encrypted.yaml >/dev/null 2>&1; then
    ok "backup with encryption"
  else
    fail "backup with encryption"
  fi

  assert_file_nonempty "encrypted artifact present" "$BACKUPS_DIR/postgres-encrypted.sql"
}

test_s3_backup() {
  log ""
  log "=== Suite: backup — S3 backend (RustFS) ==="
  assert_exit_ok "backup s3" backup --config /configs/s3.yaml
}

test_azure_backup() {
  log ""
  log "=== Suite: storage status — Azure backend (Azurite) ==="
  # Azure backup via --config not yet supported in storage.Params dispatch.
  # Validate connectivity via storage status only.
  if ! docker ps --format '{{.Names}}' | grep -q azurite; then
    skip "Azurite not running"
    return
  fi
  assert_exit_ok "storage status azure" \
    storage status --config /configs/azure.yaml --output json
}

test_gcs_backup() {
  log ""
  log "=== Suite: backup — GCS backend (fake-gcs-server) ==="
  if ! docker ps --format '{{.Names}}' | grep -q fake-gcs; then
    skip "fake-gcs-server not running"
    return
  fi
  assert_exit_ok "backup gcs" backup --config /configs/gcs.yaml
}

test_storage_status() {
  log ""
  log "=== Suite: storage status ==="
  assert_exit_ok "storage status local" \
    storage status --config /configs/local.yaml --output json
  assert_exit_ok "storage status s3" \
    storage status --config /configs/s3.yaml --output json
}

test_db_migrate() {
  log ""
  log "=== Suite: db migrate status ==="
  assert_exit_ok "db migrate status" \
    db migrate status --config /configs/local.yaml
}

test_scheduler() {
  log ""
  log "=== Suite: scheduler — 3 consecutive cron runs ==="

  # One postgres job scheduled every minute
  cat > "$CONFIGS_DIR/scheduler.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  postgres-sched:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-sched
    schedule: "* * * * *"
EOF

  # Start scheduler in a detached container
  local ctr
  ctr=$(docker run -d \
    --network "$NETWORK" \
    --add-host=host.docker.internal:host-gateway \
    -e DEV_POSTGRES_PASSWORD=sentinel \
    -v "$WORKSPACE:/workspace" \
    -v "$CONFIGS_DIR:/configs" \
    "$IMAGE" \
    sentinel schedule start --config /configs/scheduler.yaml 2>/dev/null)

  log "Scheduler started (container: ${ctr:0:12}), waiting up to 4 min for 3 runs..."

  local max_wait=240
  local interval=15
  local elapsed=0
  local count=0
  while [[ $elapsed -lt $max_wait ]]; do
    sleep $interval
    elapsed=$((elapsed + interval))
    # NB: cobra cmd.Println writes the table to stderr — merge it so the
    # success rows are actually countable (pre-existing CLI quirk).
    count=$(sentinel monitor list --config /configs/scheduler.yaml \
      --job postgres-sched --last 6m 2>&1 | grep -c "success" || true)
    log "  scheduler: ${count}/3 success runs (${elapsed}s elapsed)"
    [[ $count -ge 3 ]] && break
  done

  docker stop "$ctr" >/dev/null 2>&1 || true
  docker rm   "$ctr" >/dev/null 2>&1 || true

  if [[ $count -ge 3 ]]; then
    ok "scheduler: 3 consecutive cron runs (postgres-sched)"
  else
    fail "scheduler: only ${count}/3 success runs after ${max_wait}s"
  fi
}

test_restore_real() {
  log ""
  log "=== Suite: restore — real execution (postgres) ==="

  if [[ ! -s "$BACKUPS_DIR/postgres-e2e.sql" ]]; then
    skip "restore real: postgres-e2e.sql missing (backup suite must run first)"
    return
  fi

  # Create target database inside the running pgsql container
  docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
    psql -U sentinel -c "DROP DATABASE IF EXISTS sentinel_restore;" >/dev/null 2>&1 || true
  if ! docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
      psql -U sentinel -c "CREATE DATABASE sentinel_restore;" >/dev/null 2>&1; then
    skip "restore real: could not create sentinel_restore database in pgsql"
    return
  fi

  cat > "$CONFIGS_DIR/restore-real.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  postgres-e2e:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-e2e

restores:
  pg-e2e-restore-real:
    type: postgres
    enabled: true
    schedule: "0 3 * * *"
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel_restore
    staging_dir: /workspace/staging
    backup_source:
      type: local
      local_path: /workspace/backups
      backup_path: postgres-e2e.sql
EOF

  assert_exit_ok "restore run pg-e2e-restore-real" \
    restore run pg-e2e-restore-real --config /configs/restore-real.yaml
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
  trap teardown EXIT

  setup

  test_unit
  test_config_validate
  test_local_backup
  test_monitor
  test_retention
  test_retention_gfs
  test_restore_dryrun
  test_restore_real
  test_encryption
  test_s3_backup
  test_azure_backup
  test_gcs_backup
  test_storage_status
  test_db_migrate
  test_scheduler

  log ""
  log "========================================"
  echo -e "${GREEN}PASS: $PASS${NC}  ${RED}FAIL: $FAIL${NC}  ${YELLOW}SKIP: $SKIPPED${NC}"
  log "========================================"

  [[ $FAIL -eq 0 ]]
}

main
