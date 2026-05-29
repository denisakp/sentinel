package pg

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/sanitize"
)

// TestErrorRedaction proves a failing pg_dump invocation's stderr is scrubbed
// before being embedded in the wrapped error string. Mirrors the production
// formatter in pg_dump.go.
func TestErrorRedaction(t *testing.T) {
	const secret = "hunter2"
	script := "printf 'password=hunter2\\nPGPASSWORD=hunter2\\npostgres://u:hunter2@h/db\\n' 1>&2; exit 1"
	cmd := exec.Command("sh", "-c", script)
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected non-zero exit")
	}

	redacted, stats := sanitize.RedactStderr(stdErr.Bytes())
	wrapped := fmt.Errorf("failed to execute pg_dump command - %w, %s", runErr, redacted).Error()

	if strings.Contains(wrapped, secret) {
		t.Errorf("wrapped error leaked secret: %q", wrapped)
	}
	if strings.Contains(wrapped, "u:hunter2@") {
		t.Errorf("wrapped error leaked URI userinfo: %q", wrapped)
	}
	if !strings.Contains(wrapped, "pg_dump") {
		t.Errorf("engine name missing: %q", wrapped)
	}
	if stats.PatternsMatched < 3 {
		t.Errorf("PatternsMatched = %d, want ≥ 3", stats.PatternsMatched)
	}
}
