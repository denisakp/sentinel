package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/denisakp/sentinel/internal/monitor"
	internalrestore "github.com/denisakp/sentinel/internal/restore"
)

func TestRestoreRunCmd_GCSFlagsRegistered(t *testing.T) {
	if restoreRunCmd.Flags().Lookup("gcs-bucket") == nil {
		t.Fatal("expected --gcs-bucket flag on restore run command")
	}
	if restoreRunCmd.Flags().Lookup("gcs-credentials-file") == nil {
		t.Fatal("expected --gcs-credentials-file flag on restore run command")
	}
	if restoreRunCmd.Flags().Lookup("gcs-project-id") == nil {
		t.Fatal("expected --gcs-project-id flag on restore run command")
	}
}

func TestHandleRestoreRun_DispatchesSharedExecutor(t *testing.T) {
	cfgPath := writeRestoreRunConfig(t, false)

	prevCfg := restoreConfigFile
	prevKeep := restoreKeepFile
	prevExecutor := runRestoreExecution
	t.Cleanup(func() {
		restoreConfigFile = prevCfg
		restoreKeepFile = prevKeep
		runRestoreExecution = prevExecutor
	})

	restoreConfigFile = cfgPath
	restoreKeepFile = false

	called := false
	runRestoreExecution = func(_ context.Context, req *internalrestore.ExecutionRequest) (*internalrestore.ExecutionResult, error) {
		called = true
		if req.Job.BackupSource.Type != "gcs" {
			return nil, fmt.Errorf("unexpected source type: %s", req.Job.BackupSource.Type)
		}
		if req.Job.BackupSource.GCSBucket != "backup-bucket" {
			return nil, fmt.Errorf("unexpected bucket: %s", req.Job.BackupSource.GCSBucket)
		}
		return &internalrestore.ExecutionResult{Status: monitor.StatusSuccess}, nil
	}

	err := handleRestoreRun(nil, []string{"pg-restore"})
	if err != nil {
		t.Fatalf("handleRestoreRun() error = %v", err)
	}
	if !called {
		t.Fatal("expected shared restore executor to be called")
	}
}

func TestHandleRestoreRun_KeepFilePreservesStagedFile(t *testing.T) {
	cfgPath := writeRestoreRunConfig(t, true)

	prevCfg := restoreConfigFile
	prevKeep := restoreKeepFile
	prevExecutor := runRestoreExecution
	t.Cleanup(func() {
		restoreConfigFile = prevCfg
		restoreKeepFile = prevKeep
		runRestoreExecution = prevExecutor
	})

	restoreConfigFile = cfgPath
	restoreKeepFile = false

	retainedPath := t.TempDir() + "/staged.sql"
	runRestoreExecution = func(_ context.Context, _ *internalrestore.ExecutionRequest) (*internalrestore.ExecutionResult, error) {
		return &internalrestore.ExecutionResult{
			Status:             monitor.StatusSuccess,
			StagedFileRetained: true,
			StagedFilePath:     retainedPath,
		}, nil
	}

	err := handleRestoreRun(nil, []string{"pg-restore"})
	if err != nil {
		t.Fatalf("handleRestoreRun() error = %v", err)
	}
}

func TestHandleRestoreRun_LockConflictIsActionable(t *testing.T) {
	cfgPath := writeRestoreRunConfig(t, false)

	prevCfg := restoreConfigFile
	prevExecutor := runRestoreExecution
	t.Cleanup(func() {
		restoreConfigFile = prevCfg
		runRestoreExecution = prevExecutor
	})

	restoreConfigFile = cfgPath
	runRestoreExecution = func(_ context.Context, _ *internalrestore.ExecutionRequest) (*internalrestore.ExecutionResult, error) {
		return &internalrestore.ExecutionResult{Status: monitor.StatusSkipped, Reason: "lock_conflict"}, internalrestore.ErrRestoreLockConflict
	}

	err := handleRestoreRun(nil, []string{"pg-restore"})
	if err == nil {
		t.Fatal("expected lock conflict error")
	}
	if !strings.Contains(err.Error(), "restore execution skipped: lock_conflict") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestHandleRestoreRun_NotifyRestoreStatuses(t *testing.T) {
	t.Setenv("TEST_PG_PASSWORD", "secret")

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfgPath := writeRestoreRunConfigWithNotifications(t, server.URL)

	tests := []struct {
		name   string
		result *internalrestore.ExecutionResult
		err    error
	}{
		{name: "success", result: &internalrestore.ExecutionResult{Status: monitor.StatusSuccess}},
		{name: "failed", result: &internalrestore.ExecutionResult{Status: monitor.StatusFailed}, err: fmt.Errorf("restore failed")},
		{name: "timeout", result: &internalrestore.ExecutionResult{Status: monitor.StatusTimeout}, err: context.DeadlineExceeded},
		{name: "skipped", result: &internalrestore.ExecutionResult{Status: monitor.StatusSkipped, Reason: "lock_conflict"}, err: internalrestore.ErrRestoreLockConflict},
	}

	prevCfg := restoreConfigFile
	prevExecutor := runRestoreExecution
	t.Cleanup(func() {
		restoreConfigFile = prevCfg
		runRestoreExecution = prevExecutor
	})

	restoreConfigFile = cfgPath
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runRestoreExecution = func(_ context.Context, _ *internalrestore.ExecutionRequest) (*internalrestore.ExecutionResult, error) {
				return tt.result, tt.err
			}
			_ = handleRestoreRun(nil, []string{"pg-restore"})
		})
	}

	if calls.Load() != int32(len(tests)) {
		t.Fatalf("expected %d notification webhook calls, got %d", len(tests), calls.Load())
	}
}

func writeRestoreRunConfig(t *testing.T, keepFile bool) string {
	t.Helper()
	t.Setenv("TEST_PG_PASSWORD", "secret")

	cfg := "version: \"1.0\"\n" +
		"defaults:\n" +
		"  storage:\n" +
		"    type: local\n" +
		"    local_path: ./backups\n" +
		"databases:\n" +
		"  pg:\n" +
		"    type: postgres\n" +
		"    host: localhost\n" +
		"    username: sentinel\n" +
		"    password_env: TEST_PG_PASSWORD\n" +
		"    database: app\n" +
		"restores:\n" +
		"  pg-restore:\n" +
		"    enabled: true\n" +
		"    type: postgres\n" +
		"    host: localhost\n" +
		"    username: sentinel\n" +
		"    password_env: TEST_PG_PASSWORD\n" +
		"    database: app_restore\n" +
		"    schedule: \"0 2 * * *\"\n" +
		"    keep_file: "
	if keepFile {
		cfg += "true\n"
	} else {
		cfg += "false\n"
	}
	cfg += "    backup_source:\n" +
		"      type: gcs\n" +
		"      gcs_bucket: backup-bucket\n" +
		"      backup_path: dumps/latest.sql\n"

	path := t.TempDir() + "/restore.yaml"
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func writeRestoreRunConfigWithNotifications(t *testing.T, webhookURL string) string {
	t.Helper()
	t.Setenv("TEST_WEBHOOK_URL", webhookURL)
	cfg := "version: \"1.0\"\n" +
		"defaults:\n" +
		"  storage:\n" +
		"    type: local\n" +
		"    local_path: ./backups\n" +
		"databases:\n" +
		"  pg:\n" +
		"    type: postgres\n" +
		"    host: localhost\n" +
		"    username: sentinel\n" +
		"    password_env: TEST_PG_PASSWORD\n" +
		"    database: app\n" +
		"restores:\n" +
		"  pg-restore:\n" +
		"    enabled: true\n" +
		"    type: postgres\n" +
		"    host: localhost\n" +
		"    username: sentinel\n" +
		"    password_env: TEST_PG_PASSWORD\n" +
		"    database: app_restore\n" +
		"    schedule: \"0 2 * * *\"\n" +
		"    staging_dir: /tmp/sentinel\n" +
		"    notifications:\n" +
		"      - type: webhook\n" +
		"        webhook_url_env: TEST_WEBHOOK_URL\n" +
		"        events: [success, failure, warning]\n" +
		"    backup_source:\n" +
		"      type: gcs\n" +
		"      gcs_bucket: backup-bucket\n" +
		"      backup_path: dumps/latest.sql\n"

	path := t.TempDir() + "/restore-notify.yaml"
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
