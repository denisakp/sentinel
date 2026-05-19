package mongo_dump

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/sanitize"
)

func TestErrorRedaction_Oplog(t *testing.T) {
	const secret = "hunter2"
	script := "printf 'mongodb://u:hunter2@rs0/local\\n' 1>&2; exit 1"
	cmd := exec.Command("sh", "-c", script)
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected non-zero exit")
	}

	redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
	msg := strings.TrimSpace(redacted)
	wrapped := fmt.Errorf("mongodump oplog capture failed: %w: %s", runErr, msg).Error()
	if strings.Contains(wrapped, secret) {
		t.Errorf("leaked: %q", wrapped)
	}
	if strings.Contains(wrapped, "u:hunter2@") {
		t.Errorf("URI userinfo leaked: %q", wrapped)
	}
}
