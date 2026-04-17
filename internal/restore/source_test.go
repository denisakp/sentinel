package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

func TestStageRestoreSourceLocal(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	stagingDir := filepath.Join(root, "staging")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "backup.sql"), []byte("select 1;"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	artifact, err := StageRestoreSource(context.Background(), config.RestoreJob{
		Name:       "test-job",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  sourceDir,
			BackupPath: "backup.sql",
		},
	})
	if err != nil {
		t.Fatalf("StageRestoreSource() error = %v", err)
	}
	if artifact.Path == "" {
		t.Fatal("expected staged path")
	}
	info, err := os.Stat(artifact.Path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("staged file mode = %o, want 0600", info.Mode().Perm())
	}
	stagingInfo, err := os.Stat(stagingDir)
	if err != nil {
		t.Fatalf("Stat(stagingDir) error = %v", err)
	}
	if stagingInfo.Mode().Perm() != 0o700 {
		t.Fatalf("staging dir mode = %o, want 0700", stagingInfo.Mode().Perm())
	}
}

func TestCleanupStagedArtifactRemovesFiles(t *testing.T) {
	root := t.TempDir()
	artifact := &StagedArtifact{
		Path:         filepath.Join(root, "backup.sql"),
		ManifestPath: filepath.Join(root, "backup.sql.manifest.json"),
	}
	if err := os.WriteFile(artifact.Path, []byte("backup"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(artifact.ManifestPath, []byte("manifest"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := CleanupStagedArtifact(artifact); err != nil {
		t.Fatalf("CleanupStagedArtifact() error = %v", err)
	}
	if _, err := os.Stat(artifact.Path); !os.IsNotExist(err) {
		t.Fatalf("expected staged file removed, stat err = %v", err)
	}
}

func TestStageRestoreSource_CleansStagedFileOnManifestDownloadError(t *testing.T) {
	originalDownload := downloadRestoreSourceObject
	t.Cleanup(func() { downloadRestoreSourceObject = originalDownload })

	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	stagingDir := filepath.Join(root, "staging")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "backup.sql"), []byte("select 1;"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	downloadRestoreSourceObject = func(ctx context.Context, source config.RestoreBackupSource, object, dest string) error {
		if object == "backup.sql.manifest.json" {
			return errors.New("backend timeout")
		}
		return downloadSourceObject(ctx, source, object, dest)
	}

	_, err := StageRestoreSource(context.Background(), config.RestoreJob{
		Name:       "test-job",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  sourceDir,
			BackupPath: "backup.sql",
		},
	})
	if err == nil {
		t.Fatal("expected manifest download error")
	}
	matches, globErr := filepath.Glob(filepath.Join(stagingDir, "*backup.sql*"))
	if globErr != nil {
		t.Fatalf("Glob() error = %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("expected staged files cleaned up, found %v", matches)
	}
}

func TestStageChainArtifacts_CleansPartialDownloadsOnError(t *testing.T) {
	originalDownload := downloadRestoreSourceObject
	t.Cleanup(func() { downloadRestoreSourceObject = originalDownload })

	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	stagingDir := filepath.Join(root, "staging")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, name := range []string{"base-001.dump", "incr-001.dump"} {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	downloadRestoreSourceObject = func(ctx context.Context, source config.RestoreBackupSource, object, dest string) error {
		if object == "incr-001.dump" {
			return errors.New("simulated download failure")
		}
		return downloadSourceObject(ctx, source, object, dest)
	}

	_, err := StageChainArtifacts(context.Background(), config.RestoreJob{
		Name:       "restore-job",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:      "local",
			LocalPath: sourceDir,
		},
	}, []string{"base-001.dump", "incr-001.dump"})
	if err == nil {
		t.Fatal("expected chain staging error")
	}
	matches, globErr := filepath.Glob(filepath.Join(stagingDir, "*"))
	if globErr != nil {
		t.Fatalf("Glob() error = %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("expected partial staged chain cleaned up, found %v", matches)
	}
}
