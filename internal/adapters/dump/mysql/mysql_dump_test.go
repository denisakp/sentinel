package mysql

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/ports/dbprobertesting"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/adapters/storage"
)

func TestBackup_ConnectivityFailureReturnsError(t *testing.T) {
	// connectivity is injected; a failing prober aborts before mysqldump.
	prober := &dbprobertesting.MockProber{}
	prober.SetPingErr(errors.New("ping: connection refused"))
	_, err := Backup(prober, &MySqlDumpArgs{
		Host:     "127.0.0.1",
		Port:     "1",
		Username: "root",
		Database: "test",
		Storage:  &storage.Params{StorageType: "local", LocalPath: t.TempDir()},
	})
	if err == nil {
		t.Fatal("expected connectivity error")
	}
	if !strings.Contains(err.Error(), "ping") && !strings.Contains(err.Error(), "connect") {
		t.Logf("connectivity error (non-strict assertion): %v", err)
	}
}

func TestBackup_InvalidArgsReturnsBuildError(t *testing.T) {
	// Missing Database → argsBuilder fails before connectivity attempt.
	_, err := Backup(&dbprobertesting.MockProber{}, &MySqlDumpArgs{Username: "root"})
	if err == nil {
		t.Fatal("expected build error")
	}
	if !strings.Contains(err.Error(), "failed to build mysql_dump args") {
		t.Fatalf("wrong wrap: %v", err)
	}
}

func TestErrorRedaction(t *testing.T) {
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
			script: "printf -- '-phunter2\\n' 1>&2; exit 2",
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
