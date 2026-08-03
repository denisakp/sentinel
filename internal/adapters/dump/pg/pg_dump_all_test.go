package pg

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/sanitize"
)

func TestErrorRedaction_PgDumpAll(t *testing.T) {
	const secret = "hunter2"
	script := "printf 'PGPASSWORD=hunter2\\npostgres://u:hunter2@h/db\\n' 1>&2; exit 1"
	cmd := exec.Command("sh", "-c", script)
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected non-zero exit")
	}

	redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
	wrapped := fmt.Errorf("failed to execute pg_dumpall command - %w, %s", runErr, redacted).Error()

	if strings.Contains(wrapped, secret) {
		t.Errorf("leaked secret: %q", wrapped)
	}
	if !strings.Contains(wrapped, "pg_dumpall") {
		t.Errorf("engine name missing: %q", wrapped)
	}
}
