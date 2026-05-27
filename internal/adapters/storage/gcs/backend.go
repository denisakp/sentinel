package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	gcsapi "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/denisakp/sentinel/internal/ports"
)

const gcsEmulatorHostEnv = "STORAGE_EMULATOR_HOST"

// ErrObjectNotFound is returned when a requested GCS object does not exist.
// Callers can use errors.Is(err, gcs.ErrObjectNotFound) to distinguish
// missing-object errors from other download failures.
var ErrObjectNotFound = errors.New("gcs: object not found")

// Config carries GCS backend configuration.
type Config struct {
	Bucket          string
	ProjectID       string
	CredentialsFile string
}

// GCSBackend implements StorageBackend for Google Cloud Storage.
type GCSBackend struct {
	bucket string
	client gcsBucketClient
}

// GoogleCloudStorage implements the legacy storage.Storage interface
// so existing backup engines can write to GCS without migration.
type GoogleCloudStorage struct {
	backend *GCSBackend
	bucket  string
}

// gcsBucketClient hides SDK details to keep behavior unit-testable.
type gcsBucketClient interface {
	Upload(ctx context.Context, srcPath, object string) error
	Download(ctx context.Context, object, destPath string) error
	Delete(ctx context.Context, object string) error
	List(ctx context.Context, prefix string) ([]ports.StorageObject, error)
	Exists(ctx context.Context, object string) (bool, error)
}

var newGCSBucketClient = func(ctx context.Context, cfg Config) (gcsBucketClient, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("gcs: bucket is required")
	}

	opts := make([]option.ClientOption, 0, 3)
	if ep := os.Getenv(gcsEmulatorHostEnv); ep != "" {
		opts = append(opts, option.WithEndpoint(ep+"/storage/v1/"))
		opts = append(opts, option.WithoutAuthentication())
	} else if strings.TrimSpace(cfg.CredentialsFile) != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	client, err := gcsapi.NewClient(ctx, opts...)
	if err != nil {
		if strings.TrimSpace(cfg.CredentialsFile) != "" {
			return nil, fmt.Errorf("gcs: failed to initialize client using gcs_credentials_file")
		}
		return nil, fmt.Errorf("gcs: failed to initialize client using application default credentials")
	}

	return &sdkBucketClient{
		bucket: client.Bucket(cfg.Bucket),
		client: client,
	}, nil
}

// NewGCSBackend constructs a GCSBackend from config.
func NewGCSBackend(cfg Config) (*GCSBackend, error) {
	ctx := context.Background()
	clt, err := newGCSBucketClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.CredentialsFile) != "" {
			return nil, fmt.Errorf("gcs: failed to initialize client using gcs_credentials_file")
		}
		return nil, fmt.Errorf("gcs: failed to initialize client using application default credentials")
	}
	return &GCSBackend{bucket: cfg.Bucket, client: clt}, nil
}

// NewGoogleCloudStorage constructs legacy storage adapter for GCS.
func NewGoogleCloudStorage(cfg Config) (*GoogleCloudStorage, error) {
	backend, err := NewGCSBackend(cfg)
	if err != nil {
		return nil, err
	}
	return &GoogleCloudStorage{backend: backend, bucket: cfg.Bucket}, nil
}

// Upload uploads local file src to GCS object dest.
func (b *GCSBackend) Upload(ctx context.Context, src, dest string) error {
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("gcs: destination object is required")
	}
	if err := b.client.Upload(ctx, src, dest); err != nil {
		return fmt.Errorf("gcs: failed to upload %q to %q: %w", src, dest, err)
	}
	return nil
}

// Download downloads GCS object src into local file dest.
func (b *GCSBackend) Download(ctx context.Context, src, dest string) error {
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("gcs: source object is required")
	}
	if err := b.client.Download(ctx, src, dest); err != nil {
		return fmt.Errorf("gcs: failed to download %q: %w", src, err)
	}
	return nil
}

// Delete removes object path from GCS.
func (b *GCSBackend) Delete(ctx context.Context, path string) error {
	object := extractObjectPath(path)
	if object == "" {
		return fmt.Errorf("gcs: object path is required")
	}
	if err := b.client.Delete(ctx, object); err != nil {
		return fmt.Errorf("gcs: failed to delete %q: %w", object, err)
	}
	return nil
}

// List returns objects under prefix.
func (b *GCSBackend) List(ctx context.Context, prefix string) ([]ports.StorageObject, error) {
	objects, err := b.client.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("gcs: failed to list objects: %w", err)
	}
	return objects, nil
}

// Exists checks whether an object exists.
func (b *GCSBackend) Exists(ctx context.Context, path string) (bool, error) {
	object := extractObjectPath(path)
	if object == "" {
		return false, fmt.Errorf("gcs: object path is required")
	}
	ok, err := b.client.Exists(ctx, object)
	if err != nil {
		return false, fmt.Errorf("gcs: failed to check existence for %q: %w", object, err)
	}
	return ok, nil
}

// Status returns repository status for this GCS backend.
func (b *GCSBackend) Status(ctx context.Context) (ports.RepoStatus, error) {
	objects, err := b.List(ctx, "")
	if err != nil {
		return ports.RepoStatus{Reachable: false, Error: err.Error()}, nil
	}

	var total int64
	var lastMod *time.Time
	for _, obj := range objects {
		total += obj.SizeBytes
		t := obj.LastModified
		if lastMod == nil || t.After(*lastMod) {
			lastMod = &t
		}
	}

	return ports.RepoStatus{
		Reachable:      true,
		BackupCount:    len(objects),
		TotalSizeBytes: total,
		LastBackup:     lastMod,
	}, nil
}

// GetBackupPath returns the destination bucket name for legacy backup engines.
func (g *GoogleCloudStorage) GetBackupPath(_ string) (string, error) {
	if g.bucket == "" {
		return "", fmt.Errorf("gcs: bucket is required")
	}
	return g.bucket, nil
}

// WriteBackup uploads data to GCS using object name derived from resource.
func (g *GoogleCloudStorage) WriteBackup(data []byte, resource string) error {
	object := extractObjectPath(resource)
	if object == "" {
		return fmt.Errorf("gcs: backup object is required")
	}

	tmpFile, err := os.CreateTemp("", "sentinel-gcs-*")
	if err != nil {
		return fmt.Errorf("gcs: failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("gcs: failed to write temp backup data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("gcs: failed to finalize temp backup file: %w", err)
	}

	if err := g.backend.Upload(context.Background(), tmpPath, object); err != nil {
		return err
	}

	return nil
}

// DeleteBackup removes backup artifact from GCS.
func (g *GoogleCloudStorage) DeleteBackup(ctx context.Context, path string) error {
	return g.backend.Delete(ctx, path)
}

type sdkBucketClient struct {
	bucket *gcsapi.BucketHandle
	client *gcsapi.Client
}

func (s *sdkBucketClient) Upload(ctx context.Context, srcPath, object string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %q: %w", srcPath, err)
	}
	defer f.Close()

	w := s.bucket.Object(object).NewWriter(ctx)
	if _, err := io.Copy(w, f); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to stream upload content: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to finalize upload: %w", err)
	}

	return nil
}

func (s *sdkBucketClient) Download(ctx context.Context, object, destPath string) error {
	r, err := s.bucket.Object(object).NewReader(ctx)
	if err != nil {
		if errors.Is(err, gcsapi.ErrObjectNotExist) {
			return fmt.Errorf("%w: %q", ErrObjectNotFound, object)
		}
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file %q: %w", destPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return fmt.Errorf("failed to write downloaded content: %w", err)
	}

	return nil
}

func (s *sdkBucketClient) Delete(ctx context.Context, object string) error {
	err := s.bucket.Object(object).Delete(ctx)
	if errors.Is(err, gcsapi.ErrObjectNotExist) {
		return nil
	}
	return err
}

func (s *sdkBucketClient) List(ctx context.Context, prefix string) ([]ports.StorageObject, error) {
	it := s.bucket.Objects(ctx, &gcsapi.Query{Prefix: prefix})
	objects := make([]ports.StorageObject, 0)
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		objects = append(objects, ports.StorageObject{
			Path:         attrs.Name,
			SizeBytes:    attrs.Size,
			LastModified: attrs.Updated,
			ETag:         attrs.Etag,
		})
	}
	return objects, nil
}

func (s *sdkBucketClient) Exists(ctx context.Context, object string) (bool, error) {
	_, err := s.bucket.Object(object).Attrs(ctx)
	if errors.Is(err, gcsapi.ErrObjectNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func extractObjectPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "gs://") {
		withoutScheme := strings.TrimPrefix(trimmed, "gs://")
		parts := strings.SplitN(withoutScheme, "/", 2)
		if len(parts) == 2 {
			return parts[1]
		}
		return ""
	}
	if strings.Contains(trimmed, "/") {
		return filepath.Base(trimmed)
	}
	return trimmed
}
