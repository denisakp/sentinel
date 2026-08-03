#!/usr/bin/env bash
# Self-test for the `make lint-redact-stderr` target.
#
# (a) Clean tree must pass (exit 0).
# (b) Injected offender file must fail (non-zero exit).
#
# Used by T023 / T026 of feature 012-pg-dump-stderr-redaction.

set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OFFENDER="${ROOT}/pkg/backup/pg_dump/_lint_test_offender.go"

cleanup() {
  rm -f "${OFFENDER}"
}
trap cleanup EXIT

cd "${ROOT}"

echo "[self-test] (a) clean tree should pass"
if ! make lint-redact-stderr >/dev/null; then
  echo "[self-test] FAIL: clean tree returned non-zero"
  exit 1
fi

echo "[self-test] (b) injecting offender file"
cat > "${OFFENDER}" <<'EOF'
package pg_dump

import (
	"bytes"
	"fmt"
)

func _lintOffender() error {
	var stdErr bytes.Buffer
	err := fmt.Errorf("nope")
	return fmt.Errorf("x: %w, %s", err, stdErr.String())
}
EOF

if make lint-redact-stderr >/dev/null 2>&1; then
  echo "[self-test] FAIL: lint did not detect offender"
  exit 1
fi

echo "[self-test] OK: both pass-clean and fail-on-offender behaviors verified"
