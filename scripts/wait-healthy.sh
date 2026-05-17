#!/usr/bin/env bash
# Usage: wait-healthy.sh <service1> [service2 ...]
# Waits for each docker compose service to report "healthy".
set -euo pipefail

COMPOSE_DB="infra/docker/docker-compose.yml"
MAX_WAIT=60

for service in "$@"; do
  echo "[wait] Waiting for $service..."
  elapsed=0
  while true; do
    id=$(docker compose -f "$COMPOSE_DB" ps -q "$service" 2>/dev/null || true)
    if [[ -n "$id" ]]; then
      status=$(docker inspect --format='{{.State.Health.Status}}' "$id" 2>/dev/null || echo "none")
      [[ "$status" == "healthy" || "$status" == "none" ]] && break
    fi
    elapsed=$((elapsed+2))
    if [[ $elapsed -ge $MAX_WAIT ]]; then
      echo "[wait] WARNING: $service not healthy after ${MAX_WAIT}s, continuing anyway"
      break
    fi
    sleep 2
  done
  echo "[wait] $service ready"
done
