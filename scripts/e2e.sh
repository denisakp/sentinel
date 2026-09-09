#!/usr/bin/env bash
# End-to-end validation script for Sentinel.
# Requires: docker, sentinel binary in PATH or SENTINEL_BIN env var.
# Usage: ./scripts/e2e.sh [--skip-build] [--keep]
#   --skip-build  skip docker image build (use existing sentinel-dev:local)
#   --keep        keep containers running after the run
#   --demo-assert-rejected
#                 run only the deliberately-failing demonstration of the refusal
#                 assertion, to show the gate can fail. Never part of a normal run.
#
# WHERE THE SUITE RUNS, AND WHY IT IS SPLIT
#
# The unit stage runs `go test ./...`, which silently excludes every file behind
# the `integration` build tag. Those tests therefore need their own stage, which
# is `test_integration` below, and it behaves differently from the others on
# purpose:
#
#   * It ALWAYS compiles the integration-tagged tests, module-wide. That is
#     nearly free and it is the guarantee that matters most: a test that does not
#     compile is not coverage, and until this stage existed nothing locally
#     compiled them at all.
#   * It RUNS them only when the four database client binaries they invoke are
#     present. On a host without pg_dump, mysqldump and mariadb-dump, six engine
#     tests fail for reasons that have nothing to do with the change under test.
#     Continuous integration installs all four explicitly (see
#     .github/workflows/integration.yml), so the stage runs there.
#   * When it declines to run, it SAYS SO, naming the missing binaries. A stage
#     that could not run is neither a pass nor a failure, and reporting it as
#     either is what allowed 73 defects to ship against a green suite.
#
# Every skip is checked against scripts/e2e-skip-allowlist.txt. An unrecorded
# skip fails the suite, and so does an allowlist entry that no longer skips.
#
# Until branch protection exists on develop and 1.x, every gate here is advisory:
# nothing stops a merge that ignores it. See docs/runbooks/required-checks.md.

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
DEMO_ASSERT_REJECTED=false
for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=true ;;
    --keep)       KEEP_RUNNING=true ;;
    --demo-assert-rejected) DEMO_ASSERT_REJECTED=true ;;
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

# The integration stage keeps its own tallies so the summary can distinguish
# "the integration tests passed" from "the rest of the suite passed".
INTEG_PASS=0
INTEG_FAIL=0
INTEG_SKIP=0

# Stages that could not run, as "name|reason" entries. This is the third state
# the summary was missing: neither a pass nor a failure, and rounding it to
# either is the defect this harness exists to stop repeating.
STAGES_NOT_RUN=()

# Names of everything that did not execute, listed by name in the summary so the
# count is never the only thing a reader gets.
SKIPPED_NAMES=()

SKIP_ALLOWLIST="$PROJECT_ROOT/scripts/e2e-skip-allowlist.txt"

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
skip() { echo -e "${YELLOW}[SKIP]${NC} $*"; SKIPPED=$((SKIPPED+1)); SKIPPED_NAMES+=("$*"); }

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
    -e SENTINEL_MASTER_KEY="${SENTINEL_MASTER_KEY:-}" \
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

# Record a stage that could not run, with the reason. Reported distinctly from
# passes and failures in the summary.
stage_not_run() {
  STAGES_NOT_RUN+=("$1|$2")
  echo -e "${YELLOW}[NOT RUN]${NC} $1: $2"
}

# Run the Go test suite with the integration build tag and emit one line per test
# outcome, as "<action> <package> <test>".
#
# This consumes the toolchain's own machine-readable stream rather than scraping
# the human-readable output. Scraping is fragile across toolchain versions and
# cannot reliably attribute a skip to its package.
#
# IMPORTANT: invoke the toolchain directly. Some developer environments proxy
# `go test` through a wrapper that rewrites its output into a human summary. Such
# a wrapper swallows this stream entirely and yields an empty result that reads as
# "no tests ran" while the suite reports success. That is the exact failure this
# stage exists to prevent, so do not validate changes here through a wrapper.
#
# Returns the toolchain's own exit status.
run_go_tests_json() {
  local rc=0
  ( cd "$PROJECT_ROOT" && go test -tags integration -json ./... 2>/dev/null ) \
    | grep -E '"Action":"(pass|fail|skip)"' \
    | grep '"Test":' \
    | sed -E 's/.*"Action":"([a-z]+)".*"Package":"([^"]+)".*"Test":"([^"]+)".*/\1 \2 \3/' \
    || rc=$?
  return $rc
}

# Assert that a command REFUSED an input, and refused it for a stated reason.
#
#   assert_rejected <label> <expected-stderr-pattern> <command> [args...]
#
# Passes only when BOTH hold: the command exits non-zero, AND the expected
# pattern matches the ERROR stream.
#
# WHY THIS HELPER DOES NOT MERGE THE STREAMS, unlike assert_exit_ok and
# assert_output_contains above: a refusal is an error, and errors belong on the
# error stream. Merging is exactly what kept issue #165 invisible, where several
# commands write ordinary output to the error stream and no assertion could see
# it. A merging helper would happily pass for a command that prints its refusal
# to stdout, so this one captures the two separately and matches only stderr.
# The stdout capture is used solely to make failures legible.
#
# The four failure cases are distinguished, because "it failed" is not enough to
# act on: a wording change, a stream fault and a genuine regression need
# different fixes.
assert_rejected() {
  local label="$1"
  local needle="$2"; shift 2
  local out err rc=0 errfile
  errfile="$(mktemp)"
  out="$(sentinel "$@" 2>"$errfile")" || rc=$?
  err="$(cat "$errfile")"
  rm -f "$errfile"

  local err_match=false out_match=false
  echo "$err" | grep -q "$needle" && err_match=true
  echo "$out" | grep -q "$needle" && out_match=true

  if [[ $rc -ne 0 && "$err_match" == true ]]; then
    ok "$label"
    return
  fi

  if [[ $rc -eq 0 && "$out_match" == true ]]; then
    fail "$label (two faults: command exited 0 when it should have refused, AND wrote the message to stdout instead of stderr)"
  elif [[ $rc -eq 0 ]]; then
    fail "$label (expected refusal, but the command exited 0)"
  elif [[ "$out_match" == true ]]; then
    fail "$label (WRONG STREAM: refused with exit $rc, but '$needle' arrived on stdout, not stderr)"
  else
    fail "$label (refused with exit $rc, but for a different reason: expected '$needle')"
  fi
  printf '%s\n' "  [stderr] ${err:-<empty>}" | head -8
  printf '%s\n' "  [stdout] ${out:-<empty>}" | head -8
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

# ---------------------------------------------------------------------------
# Integration stage
#
# Always compiles the integration-tagged tests. Runs them only when the database
# client binaries they invoke are present, and says so plainly when they are not.
# See the header comment for why the stage is split this way.
# ---------------------------------------------------------------------------

# The binaries the integration tests shell out to. Absent any of these, the
# engine backup and restore tests fail for reasons unrelated to the change under
# test, so the stage declines to run rather than reporting a misleading red.
INTEG_REQUIRED_TOOLS=(pg_dump mysqldump mariadb-dump mongodump)

# Echo the names of any required binaries that are missing. Returns the names
# rather than a bare boolean so the report can say which ones.
integration_missing_tools() {
  local t
  for t in "${INTEG_REQUIRED_TOOLS[@]}"; do
    command -v "$t" >/dev/null 2>&1 || printf '%s\n' "$t"
  done
}

# Strip the module prefix so allowlist entries read as repository paths.
_short_pkg() { printf '%s\n' "${1#github.com/denisakp/sentinel/}"; }

# Emit "key|category|issue|reason" for each allowlist entry, comments stripped.
_allowlist_entries() {
  [[ -f "$SKIP_ALLOWLIST" ]] || return 0
  # Strip whole-line comments only. A mid-line strip would eat the "#123" issue
  # reference that every environment and scaffold entry is required to carry.
  grep -v '^[[:space:]]*#' "$SKIP_ALLOWLIST" \
    | grep -v '^[[:space:]]*$' \
    | sed -E 's/[[:space:]]*\|[[:space:]]*/|/g'
}

test_integration() {
  log ""
  log "=== Suite: integration tests ==="

  # (1) Compile gate. Module-wide on purpose: the continuous-integration job
  # targets ./tests/integration/... only, which misses the integration-tagged
  # files under internal/adapters/storage/. Those are compiled here.
  if (cd "$PROJECT_ROOT" && go vet -tags=integration ./... >/dev/null 2>&1); then
    ok "integration tests compile (go vet -tags=integration ./...)"
  else
    fail "integration tests do not compile (go vet -tags=integration ./...)"
    (cd "$PROJECT_ROOT" && go vet -tags=integration ./... 2>&1 | tail -15 | sed 's/^/  [vet] /') || true
    return
  fi

  # (2) Prerequisite preflight.
  local missing
  missing=$(integration_missing_tools | tr '\n' ' ' | sed 's/[[:space:]]*$//')
  if [[ -n "$missing" ]]; then
    stage_not_run "integration tests" "missing client binaries: $missing (CI installs these; see .github/workflows/integration.yml)"
    return
  fi

  # (3) Run, and account for every outcome.
  local results rc=0
  results=$(run_go_tests_json) || rc=$?

  local action pkg test key
  local observed_skips=()
  while read -r action pkg test; do
    [[ -z "${action:-}" ]] && continue
    key="$(_short_pkg "$pkg")::$test"
    case "$action" in
      pass) INTEG_PASS=$((INTEG_PASS+1)) ;;
      fail) INTEG_FAIL=$((INTEG_FAIL+1)); fail "integration: $key" ;;
      skip) INTEG_SKIP=$((INTEG_SKIP+1)); observed_skips+=("$key") ;;
    esac
  done <<< "$results"

  if [[ $INTEG_FAIL -eq 0 && $rc -ne 0 ]]; then
    fail "integration stage exited non-zero ($rc) with no failing test recorded"
  fi
  [[ $INTEG_FAIL -eq 0 ]] && ok "integration tests ($INTEG_PASS passed)"

  # (3a) Every skip must be declared. An unrecorded skip is coverage that
  # silently vanished, which is how this defect batch reached production.
  local entry ekey allowed_keys=()
  while IFS='|' read -r ekey ecat eissue ereason; do
    [[ -z "${ekey:-}" ]] && continue
    allowed_keys+=("$ekey")
  done <<< "$(_allowlist_entries)"

  local s a found
  for s in ${observed_skips[@]+"${observed_skips[@]}"}; do
    found=false
    for a in ${allowed_keys[@]+"${allowed_keys[@]}"}; do
      [[ "$s" == "$a" ]] && { found=true; break; }
    done
    if [[ "$found" == true ]]; then
      skip "integration: $s (declared)"
    else
      fail "integration: $s skipped but is not in scripts/e2e-skip-allowlist.txt (record it with a reason and issue, or fix it)"
    fi
  done

  # (3b) Every declared skip must still be skipping. Without this the allowlist
  # decays into a list nobody has revisited, which is how a control of this kind
  # usually dies.
  for a in ${allowed_keys[@]+"${allowed_keys[@]}"}; do
    found=false
    for s in ${observed_skips[@]+"${observed_skips[@]}"}; do
      [[ "$s" == "$a" ]] && { found=true; break; }
    done
    [[ "$found" == false ]] && \
      fail "stale allowlist entry: $a no longer skips (or no longer exists); remove it from scripts/e2e-skip-allowlist.txt"
  done
}

test_config_validate() {
  log ""
  log "=== Suite: config validate ==="
  for cfg in local s3 azure gcs; do
    assert_exit_ok "config validate [$cfg]" config validate --config "/configs/$cfg.yaml"
  done

  # The refusal assertion, exercised against refusals the product already
  # performs. No product behaviour is introduced or changed to make these pass;
  # they demonstrate the helper works in the direction that matters.
  assert_rejected "config validate rejects a missing config file" \
    "failed to load config" config validate --config /configs/does-not-exist.yaml
  assert_rejected "unknown subcommand is rejected" \
    "unknown command" definitely-not-a-command
}

# Demonstrates that the refusal assertion FAILS when a command does not refuse.
# Run on demand, never from main: its whole purpose is to fail, and a suite that
# ships a deliberately failing test teaches contributors to ignore red.
#
#   bash scripts/e2e.sh --demo-assert-rejected
#
# This is the negative half of the demonstration required for the helper. A gate
# nobody has watched fail is not a gate anyone should trust.
demo_assert_rejected_negative() {
  log ""
  log "=== Demo: assert_rejected must FAIL below (this is the point) ==="
  assert_rejected "version should NOT be refused (expect this to FAIL)" \
    "this string appears nowhere" version
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

# test_verify_all exercises the repository-wide integrity sweep (spec 051 / PRD
# 34) over the local backups seeded by test_local_backup: an all-intact sweep
# must exit 0 with every backup "ok"; corrupting a stored artifact must flag it
# "corrupted" with a non-zero exit; and --output json must be parseable. The
# corrupted file is restored afterwards so later suites are unaffected.
test_verify_all() {
  log ""
  log "=== Suite: backup verify --all (integrity sweep) [spec 051] ==="

  # A dedicated job whose `output` INCLUDES the .sql extension, so the local
  # artifact path the manifest step resolves matches the file the dump writes
  # and a `<name>.sql.manifest.json` sidecar is produced (a bare `output:
  # pg-verify` would not get a manifest — a separate pre-existing quirk). The
  # sweep is scoped with `--job` so it only sees this manifest-bearing backup.
  cat > "$CONFIGS_DIR/verify-all.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  pg-verify-e2e:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: pg-verify-e2e.sql
EOF

  assert_exit_ok "verify-all: backup (writes manifest sidecar)" \
    backup --config /configs/verify-all.yaml
  assert_file_nonempty "verify-all: manifest sidecar present" \
    "$BACKUPS_DIR/pg-verify-e2e.sql.manifest.json"

  # 1. Intact ⇒ exit 0 + summary + ok.
  assert_exit_ok "verify --all exits 0 (all intact)" \
    backup verify --all --job pg-verify-e2e --config /configs/verify-all.yaml
  assert_output_contains "verify --all reports ok" "ok" \
    backup verify --all --job pg-verify-e2e --config /configs/verify-all.yaml

  # 2. JSON output is parseable (results array + summary object).
  local json
  json=$(sentinel backup verify --all --job pg-verify-e2e --output json --config /configs/verify-all.yaml 2>/dev/null)
  if command -v jq >/dev/null 2>&1; then
    if echo "$json" | jq -e '.summary.checked >= 1 and (.results | type == "array")' >/dev/null 2>&1; then
      ok "verify --all --output json is parseable"
    else
      fail "verify --all --output json not parseable"
      echo "$json" | tail -10 | sed 's/^/  [json] /'
    fi
  elif echo "$json" | grep -q '"summary"' && echo "$json" | grep -q '"results"'; then
    ok "verify --all --output json is parseable (grep fallback)"
  else
    fail "verify --all --output json not parseable"
  fi

  # 3. Corrupt the stored artifact FROM INSIDE a container (the sentinel CLI
  #    reads the mounted file from the container; a host-side append is not
  #    guaranteed to propagate under some bind-mount drivers). Expect corrupted
  #    + non-zero exit.
  docker run --rm -v "$WORKSPACE:/workspace" "$IMAGE" \
    sh -c "printf 'CORRUPTION\n' >> /workspace/backups/pg-verify-e2e.sql" >/dev/null 2>&1
  local out code
  # `verify --all` returns exit 5 on integrity failure by design; capture it
  # without tripping `set -e` on the command-substitution assignment.
  out=$(sentinel backup verify --all --job pg-verify-e2e --config /configs/verify-all.yaml 2>&1) && code=0 || code=$?
  if [[ $code -ne 0 ]] && echo "$out" | grep -qE '[1-9][0-9]* corrupted'; then
    ok "verify --all detects corruption (corrupted + non-zero exit $code)"
  else
    fail "verify --all should report corrupted + non-zero exit (code=$code)"
    echo "$out" | tail -8 | sed 's/^/  [cmd] /'
  fi
}

# test_scheduled_integrity exercises the scheduled-integrity wiring (spec 052 /
# PRD 35): a config with integrity.scheduled_check passes validation, an enabled
# check without a cron is rejected, `schedule list` registers the reserved
# __integrity_check job (type integrity), and the integrity_checks audit table
# round-trips a scheduled run with the result vocabulary CHECK-enforced. A real
# weekly cron tick is out of scope for the harness (the automatic firing is
# proven by a controllable-clock unit test); this asserts config-validation +
# scheduler registration + the results-store round-trip end-to-end (A8).
test_scheduled_integrity() {
  log ""
  log "=== Suite: scheduled integrity check [spec 052] ==="

  cat > "$CONFIGS_DIR/integrity-scheduled.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

integrity:
  algorithm: sha256
  scheduled_check:
    enabled: true
    cron: "0 3 * * 0"
    since: 30d
    notify_on: failure

databases:
  pg-integrity-e2e:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    schedule: "0 2 * * *"
EOF

  # 1. A valid scheduled_check block passes config validation.
  assert_exit_ok "scheduled integrity: config validate" \
    config validate --config /configs/integrity-scheduled.yaml

  # 2. An enabled check WITHOUT a cron is rejected at validation.
  cat > "$CONFIGS_DIR/integrity-bad.yaml" <<EOF
version: "1.0"
history_db_path: /workspace/.sentinel/history.db
defaults:
  storage:
    type: local
    local_path: /workspace/backups
integrity:
  scheduled_check:
    enabled: true
databases:
  pg-integrity-e2e:
    type: postgres
    host: pgsql
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
EOF
  local bad_out bad_code
  bad_out=$(sentinel config validate --config /configs/integrity-bad.yaml 2>&1) && bad_code=0 || bad_code=$?
  if [[ $bad_code -ne 0 ]] && echo "$bad_out" | grep -qi "cron is required"; then
    ok "scheduled integrity: enabled-without-cron rejected"
  else
    fail "scheduled integrity: enabled-without-cron should be rejected (code=$bad_code)"
    echo "$bad_out" | tail -5 | sed 's/^/  [cmd] /'
  fi

  # 3. `schedule list` registers the reserved __integrity_check job (type integrity).
  assert_output_contains "scheduled integrity: schedule list registers __integrity_check" "__integrity_check" \
    schedule list --config /configs/integrity-scheduled.yaml --format json
  assert_output_contains "scheduled integrity: job labelled integrity" "\"type\": \"integrity\"" \
    schedule list --config /configs/integrity-scheduled.yaml --format json

  # 4. Opening the monitor applies migration 005 (the integrity_checks table).
  assert_exit_ok "scheduled integrity: monitor open migrates to v5" \
    monitor list --config /configs/integrity-scheduled.yaml

  if ! command -v sqlite3 >/dev/null 2>&1; then
    skip "scheduled integrity: sqlite3 not available for integrity_checks round-trip"
    return
  fi

  if ! sqlite3 "$HISTORY_DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='integrity_checks';" 2>/dev/null | grep -q integrity_checks; then
    fail "scheduled integrity: integrity_checks table missing after migration 005"
    return
  fi
  ok "scheduled integrity: integrity_checks table present (migration 005)"

  # Seed one grouped scheduled run (2 rows) and read it back — the durable
  # audit round-trip A8 calls for.
  sqlite3 "$HISTORY_DB" <<SQL
INSERT INTO integrity_checks (id, run_id, backup_id, job_name, result, storage_backend, artifact_path, checked_at, trigger) VALUES
 ('ic-e2e-1','run-e2e','b1','pg-integrity-e2e','ok','local','/workspace/backups/b1.sql','2026-08-03 03:00:00+00:00','scheduled'),
 ('ic-e2e-2','run-e2e','b2','pg-integrity-e2e','corrupted','local','/workspace/backups/b2.sql','2026-08-03 03:00:00+00:00','scheduled');
SQL

  local rowcount
  rowcount=$(sqlite3 "$HISTORY_DB" "SELECT COUNT(*) FROM integrity_checks WHERE run_id='run-e2e' AND trigger='scheduled';" 2>/dev/null || echo "ERR")
  if [[ "$rowcount" == "2" ]]; then
    ok "scheduled integrity: integrity_checks persisted 2 grouped rows"
  else
    fail "scheduled integrity: expected 2 rows for run-e2e, got '$rowcount'"
  fi

  # The result vocabulary is CHECK-enforced: an out-of-vocabulary value fails.
  if sqlite3 "$HISTORY_DB" "INSERT INTO integrity_checks (id, run_id, result, checked_at, trigger) VALUES ('ic-bad','run-e2e','rotten','2026-08-03 03:00:00+00:00','scheduled');" >/dev/null 2>&1; then
    fail "scheduled integrity: integrity_checks accepted an out-of-vocabulary result"
  else
    ok "scheduled integrity: integrity_checks CHECK rejects a bad result value"
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

# test_compression proves the spec 049 / PRD 33 pipeline compression end-to-end:
# a config-driven LOCAL backup with compression enabled must produce a
# COMPRESSED artifact (zstd magic, not plaintext SQL) + a manifest recording the
# algorithm, and restore must auto-decompress (no operator flag) and round-trip.
test_compression() {
  log ""
  log "=== Suite: compression — local, zstd [spec 049] ==="

  local art="$BACKUPS_DIR/postgres-comp.sql"
  local manifest="${art}.manifest.json"
  rm -f "$art" "$manifest"

  cat > "$CONFIGS_DIR/compression.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  postgres-comp:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-comp.sql
    compression:
      enabled: true
      algorithm: zstd
EOF

  # Seed a probe row so the restore below proves a real round-trip.
  local seeded=false
  if docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
      psql -U sentinel -d sentinel -c \
      "DROP TABLE IF EXISTS e2e_comp_probe; CREATE TABLE e2e_comp_probe(id int primary key, note text); INSERT INTO e2e_comp_probe VALUES (1,'spec-049-compression');" >/dev/null 2>&1; then
    seeded=true
  fi

  assert_exit_ok "compression: backup local (zstd)" backup --config /configs/compression.yaml

  # Core assertion: the stored artifact is zstd-compressed (magic 28 b5 2f fd),
  # NOT plaintext SQL.
  if [[ -s "$art" ]]; then
    local magic
    magic=$(head -c4 "$art" | od -An -tx1 | tr -d ' \n')
    if [[ "$magic" == "28b52ffd" ]]; then
      ok "compression: artifact carries zstd magic (compressed, not plaintext)"
    else
      fail "compression: artifact magic=$magic, expected zstd 28b52ffd (not compressed?)"
    fi
    if grep -qa "PostgreSQL database dump" "$art"; then
      fail "compression: artifact contains plaintext SQL header — not compressed"
    else
      ok "compression: no plaintext SQL header in artifact"
    fi
  else
    fail "compression: artifact missing ($art)"
  fi

  # Manifest records the compression algorithm.
  if [[ -s "$manifest" ]] && grep -qa '"compression"' "$manifest" && grep -qa 'zstd' "$manifest"; then
    ok "compression: manifest records algorithm (zstd)"
  else
    fail "compression: manifest missing compression block"
  fi

  # Restore auto-decompresses (no operator flag) and round-trips the data.
  if [[ "$seeded" == true ]]; then
    docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
      psql -U sentinel -c "DROP DATABASE IF EXISTS sentinel_restore_comp;" >/dev/null 2>&1 || true
    if docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
        psql -U sentinel -c "CREATE DATABASE sentinel_restore_comp;" >/dev/null 2>&1; then
      cat > "$CONFIGS_DIR/compression-restore.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db

defaults:
  storage:
    type: local
    local_path: /workspace/backups

databases:
  postgres-comp:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: postgres-comp.sql

restores:
  pg-comp-restore:
    type: postgres
    enabled: true
    schedule: "0 3 * * *"
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel_restore_comp
    staging_dir: /workspace/staging
    backup_source:
      type: local
      local_path: /workspace/backups
      backup_path: postgres-comp.sql
EOF
      assert_exit_ok "compression: restore run (auto-decompress)" \
        restore run pg-comp-restore --config /configs/compression-restore.yaml
      local got
      got=$(docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
        psql -U sentinel -d sentinel_restore_comp -t -c "SELECT note FROM e2e_comp_probe WHERE id=1;" 2>/dev/null | tr -d ' \n')
      if [[ "$got" == "spec-049-compression" ]]; then
        ok "compression: restore round-trip data matches (auto-decompressed)"
      else
        fail "compression: restored data mismatch (got '$got')"
      fi
    else
      skip "compression: could not create sentinel_restore_comp (restore skipped)"
    fi
    docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
      psql -U sentinel -d sentinel -c "DROP TABLE IF EXISTS e2e_comp_probe;" >/dev/null 2>&1 || true
  else
    skip "compression: could not seed probe row (restore round-trip skipped)"
  fi
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

# test_remote_encrypted_backup proves the spec 047 security fix end-to-end:
# a config-driven REMOTE backup (S3 emulator) with an encryption key configured
# must upload CIPHERTEXT + a <name>.manifest.json sidecar (not the pre-fix
# silent plaintext bypass), be verifiable, and restore + decrypt round-trip.
# Object probing uses the amazon/aws-cli image already used by setup() to create
# the bucket — no host aws/mc binary is required.
test_remote_encrypted_backup() {
  log ""
  log "=== Suite: remote encrypted backup — S3 (RustFS) [spec 047] ==="

  # 1. Generate a master key (same channel test_encryption uses).
  local key
  key=$(docker run --rm "$IMAGE" sentinel security init-key 2>/dev/null | grep 'export SENTINEL_MASTER_KEY=' | cut -d'"' -f2)
  if [[ -z "$key" ]]; then
    fail "remote-enc: security init-key returned empty key"
    return
  fi
  ok "remote-enc: security init-key"

  # Explicit .sql output so the staged filename and the uploaded S3 object key
  # match deterministically (upload key = job output; manifest = key + suffix).
  local art_key="postgres-s3-enc.sql"
  local manifest_key="${art_key}.manifest.json"

  # 1b. S3 config with encryption_key_env set + a postgres job -> S3 emulator.
  cat > "$CONFIGS_DIR/s3-encrypted.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db
encryption_key_env: SENTINEL_MASTER_KEY

defaults:
  storage:
    type: s3
    s3_bucket: $S3_BUCKET
    s3_bucket_endpoint: $S3_ENDPOINT
    s3_region: $S3_REGION
    s3_access_key_id_env: SENTINEL_S3_ACCESS_KEY
    s3_secret_access_key_env: SENTINEL_S3_SECRET_KEY

databases:
  postgres-s3-enc:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: $art_key
EOF

  # Seed a probe row into the source DB so the restore below can prove a real
  # round-trip (dump -> encrypt -> upload -> download -> decrypt -> restore).
  local seeded=false
  if docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
      psql -U sentinel -d sentinel -c \
      "DROP TABLE IF EXISTS e2e_enc_probe; CREATE TABLE e2e_enc_probe(id int primary key, note text); INSERT INTO e2e_enc_probe VALUES (1,'spec-047-remote-encrypted');" >/dev/null 2>&1; then
    seeded=true
  fi

  # 2. Run the encrypted backup to S3. The key reaches the CLI via the sentinel()
  #    env passthrough (-e SENTINEL_MASTER_KEY).
  export SENTINEL_MASTER_KEY="$key"
  assert_exit_ok "remote-enc: backup to s3 (encrypted)" backup --config /configs/s3-encrypted.yaml

  # aws_s3 runs the S3-compatible CLI against RustFS with the workspace mounted,
  # mirroring the bucket-create call in setup().
  aws_s3() {
    docker run --rm \
      --network "$NETWORK" \
      --add-host=host.docker.internal:host-gateway \
      -v "$WORKSPACE:/workspace" \
      --entrypoint sh \
      amazon/aws-cli:latest -c \
      "AWS_ACCESS_KEY_ID=$S3_ACCESS_KEY AWS_SECRET_ACCESS_KEY=$S3_SECRET_KEY aws --endpoint-url $S3_ENDPOINT --region $S3_REGION $*"
  }

  # 3 + 4. Download the stored object and assert it is CIPHERTEXT (core SC-002
  #        security assertion), plus assert the manifest sidecar exists.
  local host_dl="$WORKSPACE/s3-enc-download.bin"
  rm -f "$host_dl"
  if aws_s3 "s3 cp s3://$S3_BUCKET/$art_key /workspace/s3-enc-download.bin" >/dev/null 2>&1 && [[ -s "$host_dl" ]]; then
    # SC-002: the stored object must NOT contain the plaintext pg_dump header.
    if grep -qa "PostgreSQL database dump" "$host_dl"; then
      fail "remote-enc: stored S3 object is PLAINTEXT SQL — encryption bypass (SC-002)"
    else
      ok "remote-enc: stored S3 object is ciphertext, no plaintext SQL header (SC-002)"
    fi
    # Sanity: v2 ciphertext starts with the "SENC" envelope magic.
    if [[ "$(head -c4 "$host_dl")" == "SENC" ]]; then
      ok "remote-enc: ciphertext carries SENC envelope-v2 magic"
    else
      log "  note: SENC magic not at offset 0 (envelope layout may differ) — plaintext probe above is authoritative"
    fi
    # SC-003: the <name>.manifest.json sidecar object exists in the bucket.
    if aws_s3 "s3 ls s3://$S3_BUCKET/$manifest_key" 2>/dev/null | grep -q "manifest.json"; then
      ok "remote-enc: manifest sidecar present in bucket ($manifest_key)"
    else
      fail "remote-enc: manifest sidecar missing in bucket ($manifest_key)"
    fi
    rm -f "$host_dl" 2>/dev/null || true
  else
    skip "remote-enc: aws-cli image unavailable — cannot probe stored S3 object (ciphertext + manifest checks skipped)"
  fi

  # 5. backup verify validates the remote backup (SC-003). Resolve the execution
  #    id from monitor history; assert exit 0 (verify performs the remote fetch).
  local backup_id
  backup_id=$(sentinel monitor list --config /configs/s3-encrypted.yaml --job postgres-s3-enc --last 1h 2>&1 \
    | grep "postgres-s3-enc" | grep "success" | head -1 | awk '{print $1}' || true)
  if [[ -n "$backup_id" ]]; then
    assert_exit_ok "remote-enc: backup verify (remote, SC-003)" \
      backup verify "$backup_id" --config /configs/s3-encrypted.yaml
  else
    skip "remote-enc: could not resolve backup execution id for verify"
  fi

  # 6. Restore the encrypted remote backup end-to-end into a fresh DB (SC-004).
  docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
    psql -U sentinel -c "DROP DATABASE IF EXISTS sentinel_restore_enc;" >/dev/null 2>&1 || true
  if ! docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
      psql -U sentinel -c "CREATE DATABASE sentinel_restore_enc;" >/dev/null 2>&1; then
    skip "remote-enc: could not create sentinel_restore_enc database (restore skipped)"
    docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
      psql -U sentinel -d sentinel -c "DROP TABLE IF EXISTS e2e_enc_probe;" >/dev/null 2>&1 || true
    unset SENTINEL_MASTER_KEY
    return
  fi

  cat > "$CONFIGS_DIR/s3-encrypted-restore.yaml" <<EOF
version: "1.0"
log_format: text
history_db_path: /workspace/.sentinel/history.db
encryption_key_env: SENTINEL_MASTER_KEY

defaults:
  storage:
    type: s3
    s3_bucket: $S3_BUCKET
    s3_bucket_endpoint: $S3_ENDPOINT
    s3_region: $S3_REGION
    s3_access_key_id_env: SENTINEL_S3_ACCESS_KEY
    s3_secret_access_key_env: SENTINEL_S3_SECRET_KEY

databases:
  postgres-s3-enc:
    type: postgres
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel
    output: $art_key

restores:
  pg-s3-enc-restore:
    type: postgres
    enabled: true
    schedule: "0 3 * * *"
    host: pgsql
    port: 5432
    username: sentinel
    password_env: DEV_POSTGRES_PASSWORD
    database: sentinel_restore_enc
    staging_dir: /workspace/staging
    backup_source:
      type: s3
      s3_bucket: $S3_BUCKET
      s3_bucket_endpoint: $S3_ENDPOINT
      s3_region: $S3_REGION
      s3_access_key_id_env: SENTINEL_S3_ACCESS_KEY
      s3_secret_access_key_env: SENTINEL_S3_SECRET_KEY
      backup_path: $art_key
EOF

  assert_exit_ok "remote-enc: restore run pg-s3-enc-restore (SC-004)" \
    restore run pg-s3-enc-restore --config /configs/s3-encrypted-restore.yaml

  # Data-match: the probe row must have round-tripped through encrypt+restore.
  if [[ "$seeded" == true ]]; then
    local probe
    probe=$(docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
      psql -U sentinel -d sentinel_restore_enc -tAc \
      "SELECT note FROM e2e_enc_probe WHERE id=1;" 2>/dev/null | tr -d '[:space:]' || true)
    if [[ "$probe" == "spec-047-remote-encrypted" ]]; then
      ok "remote-enc: restored data matches source (probe row round-tripped, SC-004)"
    else
      fail "remote-enc: restored probe row mismatch (got '$probe')"
    fi
  else
    skip "remote-enc: probe seeding unavailable — data-match assertion skipped (restore exit-0 above stands)"
  fi

  # Cleanup: drop the probe table from the shared source DB, clear the key.
  docker compose -f "$INFRA_DIR/docker-compose.yml" exec -T pgsql \
    psql -U sentinel -d sentinel -c "DROP TABLE IF EXISTS e2e_enc_probe;" >/dev/null 2>&1 || true
  unset SENTINEL_MASTER_KEY
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
  # The demonstration needs only the product image, not the database infra, so it
  # runs before setup and skips the teardown trap entirely.
  if [[ "$DEMO_ASSERT_REJECTED" == true ]]; then
    demo_assert_rejected_negative
    log ""
    echo -e "${GREEN}PASS: $PASS${NC}  ${RED}FAIL: $FAIL${NC}"
    log "The single failure above is the expected result: it shows the refusal"
    log "assertion rejects a command that did not refuse. A gate nobody has"
    log "watched fail is not a gate anyone should trust."
    return 0
  fi

  trap teardown EXIT

  setup

  test_unit
  test_integration
  test_config_validate
  test_local_backup
  test_verify_all
  test_scheduled_integrity
  test_compression
  test_monitor
  test_retention
  test_retention_gfs
  test_restore_dryrun
  test_restore_real
  test_encryption
  test_s3_backup
  test_remote_encrypted_backup
  test_azure_backup
  test_gcs_backup
  test_storage_status
  test_db_migrate
  test_scheduler

  log ""
  log "========================================"
  echo -e "${GREEN}PASS: $PASS${NC}  ${RED}FAIL: $FAIL${NC}  ${YELLOW}SKIP: $SKIPPED${NC}"

  # The integration stage reports separately, so "the suite passed" cannot be
  # read as "the integration tests passed" when they never ran.
  if [[ $((INTEG_PASS + INTEG_FAIL + INTEG_SKIP)) -gt 0 ]]; then
    echo -e "  integration: ${GREEN}${INTEG_PASS} passed${NC}, ${RED}${INTEG_FAIL} failed${NC}, ${YELLOW}${INTEG_SKIP} skipped${NC}"
  fi

  # Name what did not run. A count alone tells a reader nothing about which
  # coverage they are missing.
  if [[ ${#SKIPPED_NAMES[@]} -gt 0 ]]; then
    log ""
    log "Did not execute (${#SKIPPED_NAMES[@]}):"
    local n
    for n in "${SKIPPED_NAMES[@]}"; do
      echo -e "  ${YELLOW}-${NC} $n"
    done
  fi

  # A stage that could not run is neither a pass nor a failure. Reporting it as
  # either is what let 73 defects ship against a green suite.
  if [[ ${#STAGES_NOT_RUN[@]} -gt 0 ]]; then
    log ""
    log "Stages that DID NOT RUN (${#STAGES_NOT_RUN[@]}):"
    local entry
    for entry in "${STAGES_NOT_RUN[@]}"; do
      echo -e "  ${YELLOW}-${NC} ${entry%%|*}: ${entry#*|}"
    done
    log ""
    log "This result does NOT cover the stages above. It is not a full pass."
  fi

  log "========================================"

  [[ $FAIL -eq 0 ]]
}

main
