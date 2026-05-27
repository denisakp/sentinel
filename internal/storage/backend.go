package storage

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/storage/gcs"
	"github.com/denisakp/sentinel/internal/storage/local"
	"github.com/denisakp/sentinel/internal/storage/sentinel_s3"
)

// RestoreBackendParams carries the factory inputs for NewRestoreBackend.
// Not part of the StorageBackend port — factory is internal to the
// storage dispatcher.
type RestoreBackendParams struct {
	Type string

	LocalPath string

	S3Bucket          string
	S3Region          string
	S3BucketEndpoint  string
	S3AccessKeyID     string
	S3SecretAccessKey string

	GCSBucket          string
	GCSProjectID       string
	GCSCredentialsFile string
}

// NewRestoreBackend constructs a concrete backend matching p.Type. The
// returned value satisfies ports.StorageBackend.
func NewRestoreBackend(p RestoreBackendParams) (ports.StorageBackend, error) {
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
