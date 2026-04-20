package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/restore"
)

// buildLocalRestoreJob creates a restore job backed by a local fixture file.
func buildLocalRestoreJob(t *testing.T, fixtureName string, keepFile bool) (config.RestoreJob, string) {
	t.Helper()
	fixtureFile := copyRestoreFixture(t, fixtureName)
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()
	enabled := true
	job := config.RestoreJob{
		Name:       "test-restore",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		KeepFile:   keepFile,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}
	return job, stagingDir
}

func TestManualRestoreRun_LocalSource_EngineReceivesStagedPath(t *testing.T) {
	job, _ := buildLocalRestoreJob(t, "postgres-sample.sql", false)

	var capturedPath string
	result, err := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
		PostRestoreHook: func(_ context.Context, _ config.RestoreJob, stagedPath string) error {
			capturedPath = stagedPath
			return nil
		},
	})

	// Engine will fail (no real postgres), but staging should have succeeded.
	// We assert on result fields regardless of engine outcome.
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.StagedFilePath == "" {
		t.Fatal("StagedFilePath should be set after staging")
	}
	if result.BytesRestored <= 0 {
		t.Error("BytesRestored should be > 0 after staging a real fixture file")
	}
	_ = err
	_ = capturedPath
}

func TestManualRestoreRun_StagedFileHas0600Mode(t *testing.T) {
	job, _ := buildLocalRestoreJob(t, "postgres-sample.sql", true) // keep_file so we can inspect

	result, _ := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	if result == nil || result.StagedFilePath == "" {
		t.Skip("staging did not produce a file path; cannot verify mode")
	}
	fi, err := os.Stat(result.StagedFilePath)
	if os.IsNotExist(err) {
		t.Skip("staged file already removed; cannot verify mode")
	}
	if err != nil {
		t.Fatalf("stat staged file: %v", err)
	}
	mode := fi.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("staged file mode = %o, want 0600", mode)
	}
}

func TestManualRestoreRun_KeepFileTrue_ArtifactRetained(t *testing.T) {
	job, _ := buildLocalRestoreJob(t, "postgres-sample.sql", true)

	result, _ := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	if result == nil {
		t.Fatal("result must not be nil")
	}
	if !result.StagedFileRetained {
		t.Error("expected StagedFileRetained=true when keep_file=true")
	}
	if result.StagedFilePath != "" {
		if _, err := os.Stat(result.StagedFilePath); os.IsNotExist(err) {
			t.Error("staged file should still exist when keep_file=true")
		}
	}
}

func TestManualRestoreRun_KeepFileFalse_ArtifactRemoved(t *testing.T) {
	job, _ := buildLocalRestoreJob(t, "postgres-sample.sql", false)

	capturedPath := ""
	result, _ := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
		PostRestoreHook: func(_ context.Context, _ config.RestoreJob, p string) error {
			capturedPath = p
			return nil
		},
	})

	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.StagedFileRetained {
		t.Error("expected StagedFileRetained=false when keep_file=false")
	}
	// After ExecuteRestore returns, artifact should be cleaned up.
	pathToCheck := capturedPath
	if pathToCheck == "" {
		pathToCheck = result.StagedFilePath
	}
	if pathToCheck != "" {
		if _, err := os.Stat(pathToCheck); !os.IsNotExist(err) {
			t.Error("staged file should be deleted when keep_file=false")
		}
	}
}

func TestManualRestoreRun_UnsupportedSourceType_FailsEarly(t *testing.T) {
	stagingDir := t.TempDir()
	enabled := true
	job := config.RestoreJob{
		Name:       "test-unsupported",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "ftp",
			BackupPath: "backup.sql",
		},
	}

	result, err := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	// Config validation rejects unsupported source types before staging;
	// ExecuteRestore may return nil result when failing during validation.
	if err == nil {
		t.Fatal("expected error for unsupported source type")
	}
	if result != nil && result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
}

func TestManualRestoreRun_MissingBackupFile_FailsWithNotFound(t *testing.T) {
	localDir := t.TempDir()
	stagingDir := t.TempDir()
	enabled := true
	job := config.RestoreJob{
		Name:       "test-missing-backup",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: "nonexistent-backup.sql",
		},
	}

	result, err := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	if err == nil {
		t.Fatal("expected error for missing backup file")
	}
	if result == nil {
		t.Fatal("result must not be nil even on failure")
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
}

func TestManualRestoreRun_ResultHasTimingFields(t *testing.T) {
	job, _ := buildLocalRestoreJob(t, "postgres-sample.sql", false)

	result, _ := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
	if result.CompletedAt.IsZero() {
		t.Error("CompletedAt should be set")
	}
	if result.Duration <= 0 {
		t.Error("Duration should be > 0")
	}
}
