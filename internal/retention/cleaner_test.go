package retention

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/storage/gcs"
)

type fakeS3DeleteBackend struct {
	deleteErr error
	deleted   []string
}

func (f *fakeS3DeleteBackend) Delete(_ context.Context, path string) error {
	f.deleted = append(f.deleted, path)
	return f.deleteErr
}

type fakeGCSDeleteBackend struct {
	deleteErr error
	deleted   []string
}

func (f *fakeGCSDeleteBackend) Delete(_ context.Context, path string) error {
	f.deleted = append(f.deleted, path)
	return f.deleteErr
}

type fakeAzureDeleteBackend struct {
	deleteErr error
	deleted   []string
}

func (f *fakeAzureDeleteBackend) Delete(_ context.Context, path string) error {
	f.deleted = append(f.deleted, path)
	return f.deleteErr
}

func TestDeleteCandidatesLocal(t *testing.T) {
	f := filepath.Join(t.TempDir(), "old.sql")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	deleted, errs := DeleteCandidates(context.Background(), []BackupCandidate{{
		FilePath:      f,
		FileSize:      1,
		ReasonDeleted: "keep_last_exceeded",
	}}, "local", config.StorageConfig{})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(deleted))
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted, stat err = %v", err)
	}
}

func TestDeleteCandidatesGCS(t *testing.T) {
	original := newGCSDeleteBackend
	t.Cleanup(func() { newGCSDeleteBackend = original })

	fake := &fakeGCSDeleteBackend{}
	newGCSDeleteBackend = func(cfg gcs.Config) (gcsDeleteBackend, error) {
		if cfg.Bucket != "bucket-a" {
			t.Fatalf("bucket = %q", cfg.Bucket)
		}
		if cfg.CredentialsFile != "/tmp/creds.json" {
			t.Fatalf("credentials file = %q", cfg.CredentialsFile)
		}
		return fake, nil
	}

	deleted, errs := DeleteCandidates(context.Background(), []BackupCandidate{{
		FilePath:      "gs://bucket-a/folder/old.sql",
		FileSize:      10,
		ReasonDeleted: "keep_days_exceeded",
	}}, "gcs", config.StorageConfig{GCSCredentialsFile: "/tmp/creds.json"})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(deleted))
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "folder/old.sql" {
		t.Fatalf("delete calls = %#v", fake.deleted)
	}
}

func TestDeleteCandidatesS3(t *testing.T) {
	original := newS3DeleteBackend
	t.Cleanup(func() { newS3DeleteBackend = original })

	fake := &fakeS3DeleteBackend{}
	newS3DeleteBackend = func(cfg config.StorageConfig) (s3DeleteBackend, error) {
		if cfg.S3Bucket != "bucket-a" {
			t.Fatalf("bucket = %q", cfg.S3Bucket)
		}
		return fake, nil
	}

	deleted, errs := DeleteCandidates(context.Background(), []BackupCandidate{{
		FilePath:      "s3://bucket-a/folder/old.sql",
		FileSize:      10,
		ReasonDeleted: "keep_days_exceeded",
	}}, "s3", config.StorageConfig{S3Bucket: "bucket-a"})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(deleted))
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "folder/old.sql" {
		t.Fatalf("delete calls = %#v", fake.deleted)
	}
}

func TestDeleteCandidatesAzurePlainPath(t *testing.T) {
	original := newAzureDeleteBackend
	t.Cleanup(func() { newAzureDeleteBackend = original })

	fake := &fakeAzureDeleteBackend{}
	newAzureDeleteBackend = func(cfg config.StorageConfig) (azureDeleteBackend, error) {
		if cfg.AzureContainer != "container-a" {
			t.Fatalf("container = %q", cfg.AzureContainer)
		}
		return fake, nil
	}

	deleted, errs := DeleteCandidates(context.Background(), []BackupCandidate{{
		FilePath:      "folder/old.sql",
		FileSize:      10,
		ReasonDeleted: "keep_last_exceeded",
	}}, "azure", config.StorageConfig{AzureContainer: "container-a"})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(deleted))
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "folder/old.sql" {
		t.Fatalf("delete calls = %#v", fake.deleted)
	}
}

func TestDeleteCandidatesGCSDeleteFailure(t *testing.T) {
	original := newGCSDeleteBackend
	t.Cleanup(func() { newGCSDeleteBackend = original })

	newGCSDeleteBackend = func(_ gcs.Config) (gcsDeleteBackend, error) {
		return &fakeGCSDeleteBackend{deleteErr: errors.New("boom")}, nil
	}

	deleted, errs := DeleteCandidates(context.Background(), []BackupCandidate{{
		FilePath: "gs://bucket-a/folder/old.sql",
	}}, "gcs", config.StorageConfig{})

	if len(deleted) != 0 {
		t.Fatalf("deleted = %d, want 0", len(deleted))
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %d, want 1", len(errs))
	}
}

func TestParseGCSURI(t *testing.T) {
	bucket, object, err := parseGCSURI("gs://bkt/path/to/file.sql")
	if err != nil {
		t.Fatalf("parseGCSURI() error = %v", err)
	}
	if bucket != "bkt" || object != "path/to/file.sql" {
		t.Fatalf("got bucket=%q object=%q", bucket, object)
	}

	if _, _, err := parseGCSURI("s3://bkt/file.sql"); err == nil {
		t.Fatal("expected error for non-gs scheme")
	}
}

func TestParseBucketObjectRefPlainPathWithDefaultBucket(t *testing.T) {
	bucket, object, err := parseBucketObjectRef("folder/file.sql", "s3", "bucket-a")
	if err != nil {
		t.Fatalf("parseBucketObjectRef() error = %v", err)
	}
	if bucket != "bucket-a" || object != "folder/file.sql" {
		t.Fatalf("got bucket=%q object=%q", bucket, object)
	}
}
