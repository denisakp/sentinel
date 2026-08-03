INFRA_DIR   := infra/docker
COMPOSE_DB  := $(INFRA_DIR)/docker-compose.yml
COMPOSE_E2E := $(INFRA_DIR)/docker-compose.e2e.yml
NETWORK     := sentinel
IMAGE       := sentinel-dev:local
MONGO_NAME  := sentinel-mongo

.PHONY: e2e e2e-quick infra-up infra-down build-image clean help lint-redact-stderr lint bench-small bench-matrix

help:
	@echo "Targets:"
	@echo "  e2e         Full end-to-end run (build image + start infra + run tests + teardown)"
	@echo "  e2e-quick   Same but skip image rebuild"
	@echo "  infra-up    Start all infra (DBs + emulators) without running tests"
	@echo "  infra-down  Stop and remove all infra containers"
	@echo "  build-image Build sentinel-dev:local Docker image"
	@echo "  clean       infra-down + remove .e2e workspace"
	@echo "  bench-small Run the small-tier (~1GB) performance benchmark, all engines"
	@echo "  bench-matrix Alias for bench-small (see docs/benchmarks/README.md for --tier large)"

## Full run — one command, everything handled
e2e: _network infra-up build-image
	@bash scripts/e2e.sh --skip-build
	@$(MAKE) -s infra-down

## Skip image rebuild (faster on re-runs)
e2e-quick: _network infra-up
	@bash scripts/e2e.sh --skip-build
	@$(MAKE) -s infra-down

## Start infra only (useful for manual testing)
infra-up: _network _db-up _mongo-up _e2e-emulators-up

## Stop everything
infra-down:
	@echo "[make] Stopping DB stack..."
	@docker compose -f $(COMPOSE_DB) down 2>/dev/null || true
	@echo "[make] Stopping e2e emulators..."
	@docker compose -f $(COMPOSE_E2E) down -v 2>/dev/null || true
	@echo "[make] Removing mongo container..."
	@docker rm -f $(MONGO_NAME) 2>/dev/null || true

build-image:
	@echo "[make] Building $(IMAGE)..."
	@docker build -f $(INFRA_DIR)/Dockerfile.dev -t $(IMAGE) .

clean: infra-down
	@rm -rf .e2e .bench

## Performance benchmark, small tier (~1GB per engine). Report-only —
## see docs/benchmarks/README.md for methodology and docs/benchmarks/v1.3.0.md
## for the published matrix. Use scripts/benchmark.sh directly for --tier
## large (operator-run only) or a single --engine.
bench-small: _network infra-up build-image
	@bash scripts/benchmark.sh --tier small --skip-build

bench-matrix: bench-small

## Forbid raw dump-tool stderr inside error formatters in dump-adapter packages.
## Banned: fmt.Errorf / errors.New / fmt.Sprintf taking stdErr.String() (or stderr.String()).
## Use sanitize.RedactStderr(stdErr.Bytes()) upstream instead. (FR-006)
lint-redact-stderr:
	@set -e; \
	SCAN_DIRS=""; \
	[ -d pkg/backup ] && SCAN_DIRS="$$SCAN_DIRS pkg/backup"; \
	[ -d internal/adapters/dump ] && SCAN_DIRS="$$SCAN_DIRS internal/adapters/dump"; \
	if [ -z "$$SCAN_DIRS" ]; then echo "[lint-redact-stderr] no scan dirs found, skipping"; exit 0; fi; \
	MATCHES=$$(grep -rEn '(fmt\.Errorf|errors\.New|fmt\.Sprintf)\([^)]*std[Ee]rr\.String\(\)' $$SCAN_DIRS --include='*.go' 2>/dev/null || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "[lint-redact-stderr] FAIL: raw stderr embedded in error formatter — use sanitize.RedactStderr(stdErr.Bytes()) instead"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi; \
	echo "[lint-redact-stderr] OK"

## Run golangci-lint (ADR 0001 hexagonal depguard rules + forbidigo + govet) — same config CI uses.
## In the Claude Code dev shell a bare `golangci-lint` is rewritten by the rtk
## hook (injects --out-format, rejected by v2); make runs it as a subprocess so it is unaffected.
lint:
	golangci-lint run --timeout 5m

# ---------------------------------------------------------------------------
# Internal targets
# ---------------------------------------------------------------------------
_network:
	@docker network inspect $(NETWORK) >/dev/null 2>&1 \
		|| (echo "[make] Creating network '$(NETWORK)'..." && docker network create $(NETWORK))

_db-up:
	@echo "[make] Starting DB stack (postgres, mysql, mariadb)..."
	@docker compose -f $(COMPOSE_DB) up -d pgsql mysql mariadb
	@echo "[make] Waiting for DBs to be ready..."
	@bash scripts/wait-healthy.sh pgsql mysql mariadb

_mongo-up:
	@echo "[make] Starting MongoDB..."
	@docker rm -f $(MONGO_NAME) 2>/dev/null || true
	@docker run -d --name $(MONGO_NAME) --network $(NETWORK) --network-alias mongo -p 27017:27017 mongo:8.2
	@sleep 3

_e2e-emulators-up:
	@echo "[make] Starting Azurite + fake-gcs-server..."
	@docker compose -f $(COMPOSE_E2E) up -d
