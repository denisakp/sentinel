#!/usr/bin/env bash
# check-adr-0001-precondition.sh
#
# Gate script for PRD 24 / ADR 0001 hexagonal migration.
# Verifies that the ports/adapters/domain layout prerequisites exist
# before downstream specs (notably spec 027 — backup-cli-split, T001)
# are allowed to run.
#
# Exit codes:
#   0 — all preconditions satisfied
#   1 — one or more preconditions missing
#
# The script is intentionally pessimistic (always-fail) until spec 028
# (ports-skeleton) lands. As subsequent migration specs merge into
# adr-0001-full-migration, additional checks below will flip from
# always-fail to actual filesystem assertions.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

ok() {
  echo "OK:   $*"
}

# ---------------------------------------------------------------------------
# Required ports (spec 028 — ports-skeleton)
# ---------------------------------------------------------------------------
REQUIRED_PORTS=(
  internal/ports/storage.go
  internal/ports/dump.go
  internal/ports/hasher.go
  internal/ports/encryption.go
  internal/ports/manifest.go
  internal/ports/lock.go
  internal/ports/recorder.go
  internal/ports/notifier.go
  internal/ports/crypto.go
  internal/ports/tls.go
)

missing_ports=0
for port in "${REQUIRED_PORTS[@]}"; do
  if [[ -f "$port" ]]; then
    ok "port present: $port"
  else
    echo "FAIL: missing port file: $port" >&2
    missing_ports=$((missing_ports + 1))
  fi
done

if (( missing_ports > 0 )); then
  fail "$missing_ports required port file(s) missing — spec 028 (ports-skeleton) has not landed yet"
fi

# ---------------------------------------------------------------------------
# Future checks (uncomment as migration specs land)
# ---------------------------------------------------------------------------
# Spec 029 — storage adapters moved:
#   [[ -d internal/adapters/storage ]] || fail "internal/adapters/storage/ missing (spec 029)"
#
# Spec 030 — crypto adapter moved:
#   [[ -d internal/adapters/crypto ]] || fail "internal/adapters/crypto/ missing (spec 030)"
#
# Spec 031 — lock adapter moved:
#   [[ -d internal/adapters/lock ]] || fail "internal/adapters/lock/ missing (spec 031)"
#
# Spec 032 — monitor + tls adapters moved:
#   [[ -d internal/adapters/monitor ]] || fail "internal/adapters/monitor/ missing (spec 032)"
#   [[ -d internal/adapters/tls ]]     || fail "internal/adapters/tls/ missing (spec 032)"
#
# Spec 033 — notifier adapter moved:
#   [[ -d internal/adapters/notifier ]] || fail "internal/adapters/notifier/ missing (spec 033)"
#
# Spec 034 — dump adapters moved:
#   [[ -d internal/adapters/dump ]] || fail "internal/adapters/dump/ missing (spec 034)"
#
# Spec 035 — restore adapters moved:
#   [[ -d internal/adapters/restore ]] || fail "internal/adapters/restore/ missing (spec 035)"
#
# Spec 036 — domain manifest + retention:
#   [[ -d internal/domain/manifest ]]  || fail "internal/domain/manifest/ missing (spec 036)"
#   [[ -d internal/domain/retention ]] || fail "internal/domain/retention/ missing (spec 036)"
#
# Spec 037 — utils removal:
#   [[ ! -d internal/utils ]] || fail "internal/utils/ still present (spec 037)"
#
# Spec 038 — pkg removal:
#   [[ ! -d pkg ]] || fail "pkg/ still present (spec 038)"

echo
echo "ADR 0001 preconditions: PASS"
