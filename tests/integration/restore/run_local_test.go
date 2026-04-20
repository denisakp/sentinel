package integration_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/restore"
)

func TestRunLocal_StagesArtifactFromLocalSource(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "local-stage",
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

	if artifact.Path == "" {
		t.Fatal("artifact.Path should not be empty")
	}
	if artifact.SizeBytes <= 0 {
		t.Error("artifact.SizeBytes should be > 0")
	}
	if artifact.SourcePath != filepath.Base(fixtureFile) {
		t.Errorf("artifact.SourcePath = %q, want %q", artifact.SourcePath, filepath.Base(fixtureFile))
	}
}

func TestRunLocal_NotFound_ReturnsErrSourceObjectNotFound(t *testing.T) {
	localDir := t.TempDir()
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "local-missing",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: "does-not-exist.sql",
		},
	}

	_, err := restore.StageRestoreSource(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for missing local backup file")
	}
	if !errors.Is(err, restore.ErrSourceObjectNotFound) {
		t.Errorf("expected ErrSourceObjectNotFound, got %v", err)
	}
}

func TestRunLocal_UnsupportedType_ReturnsErrUnsupportedRestoreSource(t *testing.T) {
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "unsupported-source",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "ftp",
			BackupPath: "backup.sql",
		},
	}

	_, err := restore.StageRestoreSource(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for unsupported source type")
	}
	if !errors.Is(err, restore.ErrUnsupportedRestoreSource) {
		t.Errorf("expected ErrUnsupportedRestoreSource, got %v", err)
	}
}

func TestRunLocal_MissingStagingDir_Fails(t *testing.T) {
	fixtureFile := copyRestoreFixture(t, "postgres-sample.sql")
	localDir := filepath.Dir(fixtureFile)

	job := config.RestoreJob{
		Name:       "no-staging-dir",
		StagingDir: "", // missing
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  localDir,
			BackupPath: filepath.Base(fixtureFile),
		},
	}

	_, err := restore.StageRestoreSource(context.Background(), job)
	if err == nil {
		t.Fatal("expected error when staging_dir is empty")
	}
}

func TestRunLocal_UseLatestMatch_SelectsMostRecent(t *testing.T) {
	localDir := t.TempDir()
	stagingDir := t.TempDir()

	// Write two fixture files with the same name pattern.
	for _, name := range []string{"backup-v1.sql", "backup-v2.sql"} {
		data := []byte("-- " + name)
		dst := filepath.Join(localDir, name)
		if err := writeFile(t, dst, data); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	job := config.RestoreJob{
		Name:       "local-latest",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:           "local",
			LocalPath:      localDir,
			BackupPath:     "backup-*.sql",
			UseLatestMatch: true,
		},
	}

	artifact, err := restore.StageRestoreSource(context.Background(), job)
	if err != nil {
		t.Fatalf("StageRestoreSource() error = %v", err)
	}
	t.Cleanup(func() { _ = restore.CleanupStagedArtifact(artifact) })

	if artifact.Path == "" {
		t.Error("artifact.Path should not be empty when UseLatestMatch=true")
	}
}
