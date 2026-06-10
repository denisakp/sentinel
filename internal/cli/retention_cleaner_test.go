package cli

// Ported from internal/retention/cleaner_test.go (package deleted by spec
// 038 Sub-PR J). The per-backend fakes are replaced by the storage-registry
// seam (newRetentionDeleteBackend) + ports/storagetesting.MockBackend.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storage "github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	domainret "github.com/denisakp/sentinel/internal/domain/retention"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
)

// failingDeleteBackend wraps MockBackend with an always-failing Delete.
type failingDeleteBackend struct {
	*storagetesting.MockBackend
	deleteErr error
}

func (f *failingDeleteBackend) Delete(_ context.Context, _ string) error {
	return f.deleteErr
}

func stubRetentionBackend(t *testing.T, fn func(p *storage.BackendParams) (ports.StorageBackend, error)) {
	t.Helper()
	original := newRetentionDeleteBackend
	t.Cleanup(func() { newRetentionDeleteBackend = original })
	newRetentionDeleteBackend = fn
}

func TestDeleteRetentionCandidatesLocal(t *testing.T) {
	f := filepath.Join(t.TempDir(), "old.sql")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	deleted, errs := deleteRetentionCandidates(context.Background(), []domainret.BackupCandidate{{
		FilePath:      f,
		FileSize:      1,
		ReasonDeleted: "keep_last_exceeded",
	}}, config.StorageConfig{Type: "local"})

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

func TestDeleteRetentionCandidatesGCS(t *testing.T) {
	mock := storagetesting.NewMockBackend()
	mock.PutBytes("folder/old.sql", []byte("x"))
	stubRetentionBackend(t, func(p *storage.BackendParams) (ports.StorageBackend, error) {
		if p.StorageType != "gcs" {
			t.Fatalf("storage type = %q", p.StorageType)
		}
		if p.GCSBucket != "bucket-a" {
			t.Fatalf("bucket = %q", p.GCSBucket)
		}
		if p.GCSCredentialsFile != "/tmp/creds.json" {
			t.Fatalf("credentials file = %q", p.GCSCredentialsFile)
		}
		return mock, nil
	})

	deleted, errs := deleteRetentionCandidates(context.Background(), []domainret.BackupCandidate{{
		FilePath:      "gs://bucket-a/folder/old.sql",
		FileSize:      10,
		ReasonDeleted: "keep_days_exceeded",
	}}, config.StorageConfig{Type: "gcs", GCSCredentialsFile: "/tmp/creds.json"})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(deleted))
	}
	if ok, _ := mock.Exists(context.Background(), "folder/old.sql"); ok {
		t.Fatal("expected object deleted from backend")
	}
}

func TestDeleteRetentionCandidatesS3(t *testing.T) {
	mock := storagetesting.NewMockBackend()
	mock.PutBytes("folder/old.sql", []byte("x"))
	stubRetentionBackend(t, func(p *storage.BackendParams) (ports.StorageBackend, error) {
		if p.StorageType != "s3" {
			t.Fatalf("storage type = %q", p.StorageType)
		}
		if p.AWSBucket != "bucket-a" {
			t.Fatalf("bucket = %q", p.AWSBucket)
		}
		return mock, nil
	})

	deleted, errs := deleteRetentionCandidates(context.Background(), []domainret.BackupCandidate{{
		FilePath:      "s3://bucket-a/folder/old.sql",
		FileSize:      10,
		ReasonDeleted: "keep_days_exceeded",
	}}, config.StorageConfig{Type: "s3", S3Bucket: "bucket-a"})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(deleted))
	}
	if ok, _ := mock.Exists(context.Background(), "folder/old.sql"); ok {
		t.Fatal("expected object deleted from backend")
	}
}

func TestDeleteRetentionCandidatesAzurePlainPath(t *testing.T) {
	mock := storagetesting.NewMockBackend()
	mock.PutBytes("folder/old.sql", []byte("x"))
	stubRetentionBackend(t, func(p *storage.BackendParams) (ports.StorageBackend, error) {
		if p.StorageType != "azure" {
			t.Fatalf("storage type = %q", p.StorageType)
		}
		if p.AzureContainer != "container-a" {
			t.Fatalf("container = %q", p.AzureContainer)
		}
		return mock, nil
	})

	deleted, errs := deleteRetentionCandidates(context.Background(), []domainret.BackupCandidate{{
		FilePath:      "folder/old.sql",
		FileSize:      10,
		ReasonDeleted: "keep_last_exceeded",
	}}, config.StorageConfig{Type: "azure", AzureContainer: "container-a"})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(deleted))
	}
	if ok, _ := mock.Exists(context.Background(), "folder/old.sql"); ok {
		t.Fatal("expected blob deleted from backend")
	}
}

func TestDeleteRetentionCandidatesGCSDeleteFailure(t *testing.T) {
	stubRetentionBackend(t, func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return &failingDeleteBackend{
			MockBackend: storagetesting.NewMockBackend(),
			deleteErr:   errors.New("boom"),
		}, nil
	})

	deleted, errs := deleteRetentionCandidates(context.Background(), []domainret.BackupCandidate{{
		FilePath: "gs://bucket-a/folder/old.sql",
	}}, config.StorageConfig{Type: "gcs"})

	if len(deleted) != 0 {
		t.Fatalf("deleted = %d, want 0", len(deleted))
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %d, want 1", len(errs))
	}
}

func TestDeleteRetentionCandidatesUnsupportedBackend(t *testing.T) {
	deleted, errs := deleteRetentionCandidates(context.Background(), []domainret.BackupCandidate{{
		FilePath: "gdrive://folder/backup.sql",
	}}, config.StorageConfig{Type: "google-drive"})

	if len(deleted) != 0 {
		t.Fatalf("expected no deletions for unsupported backend, got %d", len(deleted))
	}
	if len(errs) == 0 {
		t.Fatal("expected unsupported backend delete error")
	}
	if !strings.Contains(errs[0].Error(), "retention delete not supported") {
		t.Fatalf("unexpected error: %v", errs[0])
	}
}

func TestDeleteRetentionCandidatesSkipsProtectedBaseline(t *testing.T) {
	mock := storagetesting.NewMockBackend()
	mock.PutBytes("folder/full.sql", []byte("x"))
	stubRetentionBackend(t, func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return mock, nil
	})

	deleted, errs := deleteRetentionCandidates(context.Background(), []domainret.BackupCandidate{{
		FilePath:      "gs://bucket-a/folder/full.sql",
		ReasonDeleted: domainret.ReasonProtectedActiveBaseline,
	}}, config.StorageConfig{Type: "gcs"})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %d, want 0 (protected baseline)", len(deleted))
	}
	if ok, _ := mock.Exists(context.Background(), "folder/full.sql"); !ok {
		t.Fatal("protected baseline object must remain in backend")
	}
}

func TestParseGCSStyleURI(t *testing.T) {
	bucket, object, err := parseBucketObjectRef("gs://bkt/path/to/file.sql", "gs", "")
	if err != nil {
		t.Fatalf("parseBucketObjectRef() error = %v", err)
	}
	if bucket != "bkt" || object != "path/to/file.sql" {
		t.Fatalf("got bucket=%q object=%q", bucket, object)
	}

	if _, _, err := parseBucketObjectRef("s3://bkt/file.sql", "gs", ""); err == nil {
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
