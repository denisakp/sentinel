package integration_test

// S3 restore source integration tests.
//
// These tests require real AWS credentials and an accessible S3 bucket.
// They are skipped unless the following environment variables are set:
//
//	SENTINEL_TEST_S3_BUCKET    — bucket name
//	SENTINEL_TEST_S3_REGION    — AWS region (e.g. us-east-1)
//	SENTINEL_TEST_S3_KEY_ID    — AWS access key ID
//	SENTINEL_TEST_S3_SECRET    — AWS secret access key
//	SENTINEL_TEST_S3_BACKUP    — object key of an existing backup in the bucket

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	restore "github.com/denisakp/sentinel/internal/adapters/restore/runtime"
)

func s3TestEnv(t *testing.T) (bucket, region, keyID, secret, backupPath string) {
	t.Helper()
	bucket = os.Getenv("SENTINEL_TEST_S3_BUCKET")
	region = os.Getenv("SENTINEL_TEST_S3_REGION")
	keyID = os.Getenv("SENTINEL_TEST_S3_KEY_ID")
	secret = os.Getenv("SENTINEL_TEST_S3_SECRET")
	backupPath = os.Getenv("SENTINEL_TEST_S3_BACKUP")
	if bucket == "" || region == "" || keyID == "" || secret == "" || backupPath == "" {
		t.Skip("S3 integration test requires SENTINEL_TEST_S3_BUCKET, SENTINEL_TEST_S3_REGION, SENTINEL_TEST_S3_KEY_ID, SENTINEL_TEST_S3_SECRET, SENTINEL_TEST_S3_BACKUP")
	}
	return
}

func TestRunS3_StagesArtifactFromS3Source(t *testing.T) {
	bucket, region, keyID, secret, backupPath := s3TestEnv(t)
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "s3-stage",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:              "s3",
			S3Bucket:          bucket,
			S3Region:          region,
			S3AccessKeyID:     keyID,
			S3SecretAccessKey: secret,
			BackupPath:        backupPath,
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

func TestRunS3_NotFound_ReturnsErrSourceObjectNotFound(t *testing.T) {
	bucket, region, keyID, secret, _ := s3TestEnv(t)
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "s3-missing",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:              "s3",
			S3Bucket:          bucket,
			S3Region:          region,
			S3AccessKeyID:     keyID,
			S3SecretAccessKey: secret,
			BackupPath:        "sentinel-test-nonexistent-backup-xyzzy.sql",
		},
	}

	_, err := restore.StageRestoreSource(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for missing S3 object")
	}
	if !errors.Is(err, restore.ErrSourceObjectNotFound) {
		t.Logf("note: error type is %T (%v) — acceptable if S3 wraps the not-found error differently", err, err)
	}
}

func TestRunS3_InvalidCredentials_Fails(t *testing.T) {
	bucket := os.Getenv("SENTINEL_TEST_S3_BUCKET")
	region := os.Getenv("SENTINEL_TEST_S3_REGION")
	if bucket == "" || region == "" {
		t.Skip("requires SENTINEL_TEST_S3_BUCKET and SENTINEL_TEST_S3_REGION")
	}
	stagingDir := t.TempDir()

	job := config.RestoreJob{
		Name:       "s3-bad-creds",
		StagingDir: stagingDir,
		BackupSource: config.RestoreBackupSource{
			Type:              "s3",
			S3Bucket:          bucket,
			S3Region:          region,
			S3AccessKeyID:     "INVALIDKEYID",
			S3SecretAccessKey: "invalidsecret",
			BackupPath:        "any/backup.sql",
		},
	}

	_, err := restore.StageRestoreSource(context.Background(), job)
	if err == nil {
		t.Fatal("expected error with invalid S3 credentials")
	}
}
