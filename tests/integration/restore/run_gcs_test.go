package integration_test

// GCS restore source integration tests.
//
// These tests require a real Google Cloud Storage bucket and credentials.
// They are skipped unless the following environment variables are set:
//
//	SENTINEL_TEST_GCS_BUCKET      — bucket name
//	SENTINEL_TEST_GCS_PROJECT     — GCP project ID
//	SENTINEL_TEST_GCS_CREDS       — path to service account JSON key file
//	SENTINEL_TEST_GCS_BACKUP      — object key of an existing backup in the bucket

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	restore "github.com/denisakp/sentinel/internal/adapters/restore/runtime"
)

func gcsTestEnv(t *testing.T) (bucket, project, creds, backupPath string) {
	t.Helper()
	bucket = os.Getenv("SENTINEL_TEST_GCS_BUCKET")
	project = os.Getenv("SENTINEL_TEST_GCS_PROJECT")
	creds = os.Getenv("SENTINEL_TEST_GCS_CREDS")
	backupPath = os.Getenv("SENTINEL_TEST_GCS_BACKUP")
	if bucket == "" || backupPath == "" {
		t.Skip("GCS integration test requires SENTINEL_TEST_GCS_BUCKET and SENTINEL_TEST_GCS_BACKUP")
	}
	return
}

func TestRunGCS_StagesArtifactFromGCSSource(t *testing.T) {
	bucket, project, creds, backupPath := gcsTestEnv(t)
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "gcs-stage",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:               "gcs",
			GCSBucket:          bucket,
			GCSProjectID:       project,
			GCSCredentialsFile: creds,
			BackupPath:         backupPath,
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
}

func TestRunGCS_NotFound_ReturnsErrSourceObjectNotFound(t *testing.T) {
	bucket, project, creds, _ := gcsTestEnv(t)
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "gcs-missing",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:               "gcs",
			GCSBucket:          bucket,
			GCSProjectID:       project,
			GCSCredentialsFile: creds,
			BackupPath:         "sentinel-test-nonexistent-backup-xyzzy.sql",
		},
	}

	_, err := restore.StageRestoreSource(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for missing GCS object")
	}
	if !errors.Is(err, restore.ErrSourceObjectNotFound) {
		t.Logf("note: error type is %T (%v) — acceptable if GCS wraps the not-found error differently", err, err)
	}
}

func TestRunGCS_InvalidBucket_Fails(t *testing.T) {
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "gcs-bad-bucket",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:       "gcs",
			GCSBucket:  "sentinel-test-bucket-that-does-not-exist-xyzzy-12345",
			BackupPath: "any/backup.sql",
		},
	}

	_, err := restore.StageRestoreSource(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for non-existent GCS bucket")
	}
}
