// Package factory constructs a v1.1.0 StorageBackend for backup ingress from
// storage.Params. It lives outside internal/storage to break the import cycle:
// internal/config imports internal/storage, and internal/storage/azure imports
// internal/config (for AzureConfig). A factory in internal/storage cannot import
// either azure or config without inducing a cycle. Hosting the wiring here keeps
// internal/storage agnostic of the azure/gdrive sub-packages and their configs.
package factory

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/storage/azure"
	"github.com/denisakp/sentinel/internal/storage/gcs"
	"github.com/denisakp/sentinel/internal/storage/gdrive"
	"github.com/denisakp/sentinel/internal/storage/local"
	"github.com/denisakp/sentinel/internal/storage/sentinel_s3"
)

// NewBackupBackend constructs a StorageBackend for backup ingress from p.
// Supported types: local, s3, gcs, google-drive, azure. Empty StorageType
// defaults to local.
func NewBackupBackend(p *storage.Params) (storage.StorageBackend, error) {
	if p == nil {
		return nil, fmt.Errorf("storage params are required")
	}
	storageType := p.StorageType
	if storageType == "" {
		storageType = "local"
	}
	switch storageType {
	case "local":
		return local.NewLocalBackend(p.LocalPath), nil
	case "s3":
		client, err := sentinel_s3.NewS3Storage(&sentinel_s3.AmazonS3Storage{
			Bucket:    p.AWSBucket,
			Region:    p.AWSRegion,
			EndPoint:  p.AWSBucketEndpoint,
			AccessKey: p.AWSAccessKeyID,
			SecretKey: p.AWSSecretAccessKey,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize s3 backup backend: %w", err)
		}
		return sentinel_s3.NewS3Backend(client), nil
	case "gcs":
		return gcs.NewGCSBackend(gcs.Config{
			Bucket:          p.GCSBucket,
			ProjectID:       p.GCSProjectID,
			CredentialsFile: p.GCSCredentialsFile,
		})
	case "google-drive":
		client, err := gdrive.NewGoogleDriveStorage(&gdrive.GoogleDriveStorage{
			FolderId:           p.GoogleDriveFolderId,
			ServiceAccountFile: p.GoogleServiceAccount,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize google-drive backup backend: %w", err)
		}
		return gdrive.NewGDriveBackend(client), nil
	case "azure":
		backend, err := azure.NewBlobBackendFromKey(p.AzureStorageAccount, p.AzureContainer, p.AzureStorageKey)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize azure backup backend: %w", err)
		}
		return backend, nil
	default:
		return nil, fmt.Errorf("unsupported backup backend type: %s", storageType)
	}
}
