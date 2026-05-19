package mongo_dump

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/sanitize"
)

func TestErrorRedaction(t *testing.T) {
	const secret = "hunter2"

	t.Run("non-empty stderr redacted", func(t *testing.T) {
		script := "printf 'MONGO_INITDB_ROOT_PASSWORD=hunter2\\nmongodb://u:hunter2@h/db\\n' 1>&2; exit 1"
		cmd := exec.Command("sh", "-c", script)
		var stdErr bytes.Buffer
		cmd.Stderr = &stdErr
		runErr := cmd.Run()
		if runErr == nil {
			t.Fatal("expected non-zero exit")
		}

		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		msg := strings.TrimSpace(redacted)
		if msg == "" {
			t.Fatal("expected non-empty msg")
		}
		wrapped := fmt.Errorf("failed to run mongo_dump: %w: %s", runErr, msg).Error()
		if strings.Contains(wrapped, secret) {
			t.Errorf("leaked: %q", wrapped)
		}
	})

	t.Run("empty stderr skipped", func(t *testing.T) {
		var stdErr bytes.Buffer // empty
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		msg := strings.TrimSpace(redacted)
		if msg != "" {
			t.Errorf("expected empty trimmed msg, got %q", msg)
		}
	})
}
