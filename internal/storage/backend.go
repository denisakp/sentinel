package storage

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/storage/gcs"
	"github.com/denisakp/sentinel/internal/storage/local"
	"github.com/denisakp/sentinel/internal/storage/sentinel_s3"
	storagetypes "github.com/denisakp/sentinel/internal/storage/types"
)

// StorageObject is re-exported from storage/types for backward compatibility.
type StorageObject = storagetypes.StorageObject

// StorageBackend is the v1.1.0 unified interface for all storage destinations.
// The existing Storage interface remains for backward compatibility.
type StorageBackend interface {
	Upload(ctx context.Context, src, dest string) error
	Download(ctx context.Context, src, dest string) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]StorageObject, error)
	Exists(ctx context.Context, path string) (bool, error)
}

// RestoreBackendParams holds the parameters needed to construct a restore StorageBackend.
type RestoreBackendParams struct {
	Type string

	// Local
	LocalPath string

	// S3
	S3Bucket          string
	S3Region          string
	S3BucketEndpoint  string
	S3AccessKeyID     string
	S3SecretAccessKey string

	// GCS
	GCSBucket          string
	GCSProjectID       string
	GCSCredentialsFile string
}

// NewRestoreBackend returns a StorageBackend for the given restore source parameters.
// Supported types: local, s3, gcs.
func NewRestoreBackend(p RestoreBackendParams) (StorageBackend, error) {
	switch p.Type {
	case "local":
		return local.NewLocalBackend(p.LocalPath), nil
	case "s3":
		client, err := sentinel_s3.NewS3Storage(&sentinel_s3.AmazonS3Storage{
			Bucket:    p.S3Bucket,
			Region:    p.S3Region,
			EndPoint:  p.S3BucketEndpoint,
			AccessKey: p.S3AccessKeyID,
			SecretKey: p.S3SecretAccessKey,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize s3 restore backend: %w", err)
		}
		return sentinel_s3.NewS3Backend(client), nil
	case "gcs":
		return gcs.NewGCSBackend(gcs.Config{
			Bucket:          p.GCSBucket,
			ProjectID:       p.GCSProjectID,
			CredentialsFile: p.GCSCredentialsFile,
		})
	default:
		return nil, fmt.Errorf("unsupported restore backend type: %s", p.Type)
	}
}
