package mysql_dump

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
	// mysqldump-style: MYSQL_PWD= env line and a -phunter2 token in the error narrative.
	script := "printf 'MYSQL_PWD=hunter2\\n-phunter2\\nmysql://u:hunter2@h/db\\n' 1>&2; exit 2"
	cmd := exec.Command("sh", "-c", script)
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected non-zero exit")
	}

	redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
	wrapped := fmt.Errorf("failed to execute mysqldump command - %w, %s", runErr, redacted).Error()

	if strings.Contains(wrapped, secret) {
		t.Errorf("leaked secret: %q", wrapped)
	}
	if !strings.Contains(wrapped, "mysqldump") {
		t.Errorf("engine name missing: %q", wrapped)
	}
}
