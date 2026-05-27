package gcs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

type fakeBucketClient struct {
	uploadErr   error
	downloadErr error
	deleteErr   error
	existsErr   error
	listErr     error
	existsValue bool
	listValue   []ports.StorageObject

	uploadCalls   []string
	downloadCalls []string
	deleteCalls   []string
	existsCalls   []string
}

func (f *fakeBucketClient) Upload(_ context.Context, srcPath, object string) error {
	f.uploadCalls = append(f.uploadCalls, srcPath+"->"+object)
	return f.uploadErr
}

func (f *fakeBucketClient) Download(_ context.Context, object, destPath string) error {
	f.downloadCalls = append(f.downloadCalls, object+"->"+destPath)
	return f.downloadErr
}

func (f *fakeBucketClient) Delete(_ context.Context, object string) error {
	f.deleteCalls = append(f.deleteCalls, object)
	return f.deleteErr
}

func (f *fakeBucketClient) List(_ context.Context, _ string) ([]ports.StorageObject, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listValue, nil
}

func (f *fakeBucketClient) Exists(_ context.Context, object string) (bool, error) {
	f.existsCalls = append(f.existsCalls, object)
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.existsValue, nil
}

func TestGCSBackend_UploadListExistsDelete(t *testing.T) {
	fake := &fakeBucketClient{
		existsValue: true,
		listValue: []ports.StorageObject{{
			Path:         "backups/test.sql",
			SizeBytes:    12,
			LastModified: time.Now().UTC(),
		}},
	}
	backend := &GCSBackend{bucket: "bucket", client: fake}

	src := filepath.Join(t.TempDir(), "src.sql")
	if err := os.WriteFile(src, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := backend.Upload(context.Background(), src, "backups/test.sql"); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if _, err := backend.List(context.Background(), "backups/"); err != nil {
		t.Fatalf("List() error = %v", err)
	}

	exists, err := backend.Exists(context.Background(), "backups/test.sql")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Fatalf("Exists() = false, want true")
	}

	if err := backend.Delete(context.Background(), "backups/test.sql"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestNewGCSBackend_CredentialPrecedence(t *testing.T) {
	original := newGCSBucketClient
	t.Cleanup(func() { newGCSBucketClient = original })

	var got Config
	newGCSBucketClient = func(_ context.Context, cfg Config) (gcsBucketClient, error) {
		got = cfg
		return &fakeBucketClient{}, nil
	}

	_, err := NewGCSBackend(Config{
		Bucket:          "bucket",
		ProjectID:       "proj",
		CredentialsFile: "/tmp/creds.json",
	})
	if err != nil {
		t.Fatalf("NewGCSBackend() error = %v", err)
	}

	if got.CredentialsFile != "/tmp/creds.json" {
		t.Fatalf("CredentialsFile = %q, want /tmp/creds.json", got.CredentialsFile)
	}
}

func TestNewGCSBackend_ADCFallback(t *testing.T) {
	original := newGCSBucketClient
	t.Cleanup(func() { newGCSBucketClient = original })

	newGCSBucketClient = func(_ context.Context, cfg Config) (gcsBucketClient, error) {
		if cfg.CredentialsFile != "" {
			t.Fatalf("expected empty credentials file for ADC path")
		}
		return &fakeBucketClient{}, nil
	}

	_, err := NewGCSBackend(Config{Bucket: "bucket"})
	if err != nil {
		t.Fatalf("NewGCSBackend() error = %v", err)
	}
}

func TestNewGCSBackend_SanitizedAuthError(t *testing.T) {
	original := newGCSBucketClient
	t.Cleanup(func() { newGCSBucketClient = original })

	secretPath := "/tmp/very-sensitive-creds.json"
	secretToken := "ya29.secret-token"
	newGCSBucketClient = func(_ context.Context, _ Config) (gcsBucketClient, error) {
		return nil, errors.New("auth failed token=" + secretToken + " keyfile=" + secretPath)
	}

	_, err := NewGCSBackend(Config{Bucket: "bucket", CredentialsFile: secretPath})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error")
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("error leaked auth token: %q", err.Error())
	}
	if strings.Contains(err.Error(), secretPath) {
		t.Fatalf("error leaked credentials path: %q", err.Error())
	}
}

func TestNewGCSBackend_SanitizedADCAuthError(t *testing.T) {
	original := newGCSBucketClient
	t.Cleanup(func() { newGCSBucketClient = original })

	secretToken := "adc-secret-token"
	newGCSBucketClient = func(_ context.Context, _ Config) (gcsBucketClient, error) {
		return nil, errors.New("adc auth failed token=" + secretToken)
	}

	_, err := NewGCSBackend(Config{Bucket: "bucket"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("error leaked ADC token: %q", err.Error())
	}
}

func TestGCSBackend_Download_Success(t *testing.T) {
	tmp := t.TempDir()
	destPath := filepath.Join(tmp, "restored.sql")

	fake := &fakeBucketClient{} // no error — download is a no-op in the fake
	backend := &GCSBackend{bucket: "bucket", client: fake}

	if err := backend.Download(context.Background(), "backups/test.sql", destPath); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if len(fake.downloadCalls) != 1 {
		t.Fatalf("expected 1 download call, got %d", len(fake.downloadCalls))
	}
	expected := "backups/test.sql->" + destPath
	if fake.downloadCalls[0] != expected {
		t.Fatalf("downloadCalls[0] = %q, want %q", fake.downloadCalls[0], expected)
	}
}

func TestGCSBackend_Download_NotFound(t *testing.T) {
	notFoundErr := fmt.Errorf("%w: %q", ErrObjectNotFound, "missing.sql")
	fake := &fakeBucketClient{downloadErr: notFoundErr}
	backend := &GCSBackend{bucket: "bucket", client: fake}

	err := backend.Download(context.Background(), "missing.sql", "/tmp/dest.sql")
	if err == nil {
		t.Fatal("Download() expected error, got nil")
	}
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("errors.Is(err, ErrObjectNotFound) = false; got: %v", err)
	}
}

func TestGCSBackend_Download_GSUriNotFound(t *testing.T) {
	notFoundErr := fmt.Errorf("%w: %q", ErrObjectNotFound, "path/to/backup.sql")
	fake := &fakeBucketClient{downloadErr: notFoundErr}
	backend := &GCSBackend{bucket: "bucket", client: fake}

	err := backend.Download(context.Background(), "gs://bucket/path/to/backup.sql", "/tmp/dest.sql")
	if err == nil {
		t.Fatal("Download() expected error, got nil")
	}
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("errors.Is(err, ErrObjectNotFound) = false; got: %v", err)
	}
}

func TestExtractObjectPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "gs uri", in: "gs://my-bucket/path/to/file.sql", want: "path/to/file.sql"},
		{name: "plain object", in: "backup.sql", want: "backup.sql"},
		{name: "path fallback", in: "bucket/backup.sql", want: "backup.sql"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractObjectPath(tt.in)
			if got != tt.want {
				t.Fatalf("extractObjectPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
