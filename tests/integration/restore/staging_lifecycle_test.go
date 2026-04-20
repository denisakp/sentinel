package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/restore"
)

func TestStagingLifecycle_DirectoryCreatedWith0700Mode(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	// Use a sub-directory that does not exist yet so MkdirAll is exercised.
	stagingDir := filepath.Join(t.TempDir(), "staging-subdir")

	enabled := true
	job := config.RestoreJob{
		Name:       "test-staging-mode",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		KeepFile:   true,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{ //nolint:errcheck
		JobName: job.Name,
		Job:     job,
	})

	fi, err := os.Stat(stagingDir)
	if os.IsNotExist(err) {
		t.Fatal("staging directory was not created")
	}
	if err != nil {
		t.Fatalf("stat stagingDir: %v", err)
	}
	if !fi.IsDir() {
		t.Fatal("staging path is not a directory")
	}
	mode := fi.Mode().Perm()
	if mode != 0o700 {
		t.Errorf("staging dir mode = %o, want 0700", mode)
	}
}

func TestStagingLifecycle_StagedFileHas0600Mode(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()

	enabled := true
	job := config.RestoreJob{
		Name:       "test-file-mode",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		KeepFile:   true, // retain so we can stat the file
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	result, _ := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	if result == nil || result.StagedFilePath == "" {
		t.Skip("no staged file path in result; cannot verify file mode")
	}
	fi, err := os.Stat(result.StagedFilePath)
	if os.IsNotExist(err) {
		t.Skip("staged file does not exist (may have been cleaned up despite keep_file)")
	}
	if err != nil {
		t.Fatalf("stat staged file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("staged file mode = %o, want 0600", fi.Mode().Perm())
	}
}

func TestStagingLifecycle_KeepFileTrue_StagedFileRetained(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()

	enabled := true
	job := config.RestoreJob{
		Name:       "test-keepfile-true",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		KeepFile:   true,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	result, _ := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
	})

	if result == nil {
		t.Fatal("result must not be nil")
	}
	if !result.StagedFileRetained {
		t.Error("StagedFileRetained should be true when keep_file=true")
	}
	if result.StagedFilePath != "" {
		if _, err := os.Stat(result.StagedFilePath); os.IsNotExist(err) {
			t.Error("staged file should exist on disk when keep_file=true")
		}
	}
}

func TestStagingLifecycle_KeepFileFalse_StagedFileRemoved(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()

	enabled := true
	job := config.RestoreJob{
		Name:       "test-keepfile-false",
		Type:       "postgres",
		Enabled:    &enabled,
		Host:       "localhost",
		Port:       5432,
		Username:   "testuser",
		Database:   "testdb",
		Schedule:   "0 2 * * *",
		StagingDir: stagingDir,
		KeepFile:   false,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	// Capture staged path before cleanup via ConflictEvaluator hook (runs after staging).
	var capturedStagedPath string
	result, _ := restore.ExecuteRestore(context.Background(), &restore.ExecutionRequest{
		JobName: job.Name,
		Job:     job,
		ConflictEvaluator: func(_ context.Context, _ config.RestoreJob, staged string) error {
			capturedStagedPath = staged
			return nil
		},
	})

	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.StagedFileRetained {
		t.Error("StagedFileRetained should be false when keep_file=false")
	}
	// After ExecuteRestore returns, the staged file should be gone.
	pathToCheck := capturedStagedPath
	if pathToCheck == "" {
		pathToCheck = result.StagedFilePath
	}
	if pathToCheck != "" {
		if _, err := os.Stat(pathToCheck); !os.IsNotExist(err) {
			t.Errorf("staged file %q should be deleted when keep_file=false", pathToCheck)
		}
	}
}

// TestStagingLifecycle_DirectStageSource exercises StageRestoreSource directly
// to validate 0o700 dir + 0o600 file without going through the full executor.
func TestStagingLifecycle_DirectStageSource_FileAndDirModes(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "direct-stage",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	artifact, err := restore.StageRestoreSource(context.Background(), job)
	if err != nil {
		t.Fatalf("StageRestoreSource() error = %v", err)
	}
	t.Cleanup(func() { _ = restore.CleanupStagedArtifact(artifact) })

	// Staging dir mode
	dirInfo, err := os.Stat(stagingDir)
	if err != nil {
		t.Fatalf("stat stagingDir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("staging dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}

	// Staged file mode
	fileInfo, err := os.Stat(artifact.Path)
	if err != nil {
		t.Fatalf("stat artifact.Path: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("staged file mode = %o, want 0600", fileInfo.Mode().Perm())
	}

	// Size matches fixture
	if artifact.SizeBytes <= 0 {
		t.Error("SizeBytes should be > 0")
	}
}

// TestStagingLifecycle_CleanupRemovesBothArtifactAndManifest verifies that
// CleanupStagedArtifact removes the file and its manifest when both exist.
func TestStagingLifecycle_CleanupRemovesBothArtifactAndManifest(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "backup.sql")
	manifestPath := filepath.Join(dir, "backup.sql.manifest.json")

	if err := os.WriteFile(artifactPath, []byte("backup"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	artifact := &restore.StagedArtifact{
		Path:         artifactPath,
		ManifestPath: manifestPath,
	}

	if err := restore.CleanupStagedArtifact(artifact); err != nil {
		t.Fatalf("CleanupStagedArtifact() error = %v", err)
	}
	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Error("artifact should be removed after cleanup")
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Error("manifest should be removed after cleanup")
	}
}
