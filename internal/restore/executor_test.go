package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

func TestExecuteRestoreReturnsLockConflict(t *testing.T) {
	root := t.TempDir()
	lockDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	lockPath := filepath.Join(lockDir, "restore.lock")
	if err := os.WriteFile(lockPath, []byte("locked"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ExecuteRestore(context.Background(), &ExecutionRequest{
		JobName: "restore",
		LockDir: lockDir,
		Job: config.RestoreJob{
			Type:        "postgres",
			Name:        "restore",
			Host:        "localhost",
			Username:    "postgres",
			PasswordEnv: "PGPASSWORD",
			Database:    "db",
			Schedule:    "0 2 * * *",
			StagingDir:  root,
			BackupSource: config.RestoreBackupSource{
				Type:       "local",
				LocalPath:  root,
				BackupPath: "backup.sql",
			},
		},
	})
	if !errors.Is(err, ErrRestoreLockConflict) {
		t.Fatalf("ExecuteRestore() error = %v, want ErrRestoreLockConflict", err)
	}
}
