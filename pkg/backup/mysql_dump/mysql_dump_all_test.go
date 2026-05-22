package mysql_dump

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/storage"
)

// fakeMysqldumpOnPath installs a fake `mysqldump` script in a temp dir and
// prepends it to PATH. Returns the script path. The script writes `body`
// to stdout and exits with `exitCode`.
func fakeMysqldumpOnPath(t *testing.T, body string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "mysqldump")
	content := fmt.Sprintf("#!/bin/sh\nprintf '%s'\nexit %d\n", body, exitCode)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake mysqldump: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return script
}

func TestErrorRedaction_MysqlDumpAll(t *testing.T) {
	const secret = "hunter2"
	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "MD-01 MYSQL_PWD env-style leak",
			script: "printf 'MYSQL_PWD=hunter2\\n' 1>&2; exit 2",
		},
		{
			name:   "MD-02 -p<password> token",
			script: "printf -- '--password=hunter2\\n' 1>&2; exit 2",
		},
		{
			name:   "MD-03 URL-style credential",
			script: "printf 'mysql://u:hunter2@h/db\\n' 1>&2; exit 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", tt.script)
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
		})
	}
}

func TestArgsBuilderAll(t *testing.T) {
	t.Run("MDA-01 defaults + skip-password + all-databases", func(t *testing.T) {
		got, err := argsBuilderAll(&MySqlDumpAllArgs{Username: "root"})
		if err != nil {
			t.Fatalf("argsBuilderAll: %v", err)
		}
		assertContainsAll(t, got,
			"--host=127.0.0.1",
			"--port=3306",
			"--user=root",
			"--all-databases",
			"--skip-password",
		)
		count := 0
		for _, a := range got {
			if a == "--all-databases" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("--all-databases appears %d times, want 1; got=%v", count, got)
		}
	})

	t.Run("MDA-02 additional args merge + dedup", func(t *testing.T) {
		got, err := argsBuilderAll(&MySqlDumpAllArgs{
			Username:       "root",
			AdditionalArgs: "--port=3306 --single-transaction",
		})
		if err != nil {
			t.Fatalf("argsBuilderAll: %v", err)
		}
		portCount := 0
		hasSingleTx := false
		for _, a := range got {
			if a == "--port=3306" {
				portCount++
			}
			if a == "--single-transaction" {
				hasSingleTx = true
			}
		}
		if portCount != 1 {
			t.Fatalf("--port=3306 appears %d times, want 1; got=%v", portCount, got)
		}
		if !hasSingleTx {
			t.Fatalf("--single-transaction missing; got=%v", got)
		}
	})

	t.Run("MDA-04 BackupAll success path with fake mysqldump", func(t *testing.T) {
		fakeMysqldumpOnPath(t, "-- mysqldump fake output\\n", 0)
		out := t.TempDir()
		err := BackupAll(&MySqlDumpAllArgs{
			Username: "root",
			Storage: &storage.Params{
				StorageType: "local",
				LocalPath:   out,
				OutName:     "alldbs",
			},
		})
		if err != nil {
			t.Fatalf("BackupAll: %v", err)
		}
		// Verify the output file exists with .sql extension.
		if _, err := os.Stat(filepath.Join(out, "alldbs.sql")); err != nil {
			t.Fatalf("expected output file: %v", err)
		}
	})

	t.Run("MDA-05 BackupAll error path wraps redaction", func(t *testing.T) {
		// Fake mysqldump that emits a password-bearing stderr and exits non-zero.
		dir := t.TempDir()
		script := filepath.Join(dir, "mysqldump")
		content := "#!/bin/sh\nprintf 'MYSQL_PWD=hunter2\\n' 1>&2\nexit 2\n"
		if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
			t.Fatalf("write fake: %v", err)
		}
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		err := BackupAll(&MySqlDumpAllArgs{
			Username: "root",
			Password: "hunter2",
			Storage:  &storage.Params{StorageType: "local", LocalPath: t.TempDir(), OutName: "x"},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if strings.Contains(msg, "hunter2") {
			t.Errorf("password leaked in error: %q", msg)
		}
		if !strings.Contains(msg, "mysqldump") {
			t.Errorf("engine name missing: %q", msg)
		}
	})

	t.Run("MDA-03 password no-leak; no --skip-password", func(t *testing.T) {
		got, err := argsBuilderAll(&MySqlDumpAllArgs{
			Username: "root",
			Password: "s3cret",
		})
		if err != nil {
			t.Fatalf("argsBuilderAll: %v", err)
		}
		for _, a := range got {
			if a == "--skip-password" {
				t.Fatalf("--skip-password unexpectedly present when password is set; got=%v", got)
			}
		}
		assertNoPasswordInArgs(t, got, "s3cret")
	})
}
