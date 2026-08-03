// Package storage is the unified storage-adapter registry. It exposes exactly
// two top-level constructors:
//
//   - NewBackend(p BackendParams) (ports.StorageBackend, error)
//     constructs a concrete backend implementing the StorageBackend port.
//
//   - NewStorage(p BackendParams) (Storage, error)  [see writer.go]
//     constructs the driver-side write-path helper (legacy Storage interface,
//     preserved verbatim for zero observable diff).
//
// This file is the stub used during commit A: every storage type returns
// "unsupported storage type: <value>" so the package compiles before any
// backend has been relocated. The full switch lands at commit G (task T017)
// once backends exist at internal/adapters/storage/{local,s3,gcs,gdrive,azure}.
package storage

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/adapters/storage/azure"
	"github.com/denisakp/sentinel/internal/adapters/storage/gcs"
	"github.com/denisakp/sentinel/internal/adapters/storage/gdrive"
	"github.com/denisakp/sentinel/internal/adapters/storage/local"
	"github.com/denisakp/sentinel/internal/adapters/storage/s3"
	"github.com/denisakp/sentinel/internal/ports"
)

// BackendParams is the union of every field needed to construct any of the
// five supported storage backends.
//
// Field set is the union of today's internal/storage.Params and
// internal/storage.RestoreBackendParams; every field appears in at least one
// of the two pre-migration shapes. Field-level validation is owned by
// internal/config/validator.go.
type BackendParams struct {
	// StorageType selects the backend. Allowed: "local", "s3", "gcs",
	// "google-drive", "azure". Empty string defaults to "local".
	StorageType string

	// OutName is consulted only by the legacy Storage write path (writer.go).
	OutName string

	// Local
	LocalPath string

	// S3-compatible
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSRegion          string
	AWSBucket          string
	AWSBucketEndpoint  string

	// Google Drive
	GoogleDriveFolderId  string
	GoogleServiceAccount string

	// GCS
	GCSBucket          string
	GCSProjectID       string
	GCSCredentialsFile string

	// Azure Blob
	AzureStorageAccount string
	AzureStorageKey     string
	AzureContainer      string
}

// Params is a transitional alias for BackendParams, kept during the storage
// migration so existing call sites that imported internal/storage.Params can
// switch to internal/adapters/storage with a path-only rewrite. Removed once
// every importer uses BackendParams directly.
type Params = BackendParams

// NewBackend constructs a concrete backend implementing ports.StorageBackend
// (and ports.StatusReporter, satisfied by the same concrete type for all five
// backends).
//
// Single replacement for:
//   - internal/storage/storage.go::NewStorage          (legacy; missed azure)
//   - internal/storage/backend.go::NewRestoreBackend    (incomplete; three types)
//   - internal/storage/factory/factory.go::NewBackupBackend
func NewBackend(p *BackendParams) (ports.StorageBackend, error) {
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
		client, err := s3.NewS3Storage(&s3.AmazonS3Storage{
			Bucket:    p.AWSBucket,
			Region:    p.AWSRegion,
			EndPoint:  p.AWSBucketEndpoint,
			AccessKey: p.AWSAccessKeyID,
			SecretKey: p.AWSSecretAccessKey,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize s3 backend: %w", err)
		}
		return s3.NewS3Backend(client), nil
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
			return nil, fmt.Errorf("failed to initialize google-drive backend: %w", err)
		}
		return gdrive.NewGDriveBackend(client), nil
	case "azure":
		backend, err := azure.NewBlobBackendFromKey(p.AzureStorageAccount, p.AzureContainer, p.AzureStorageKey)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize azure backend: %w", err)
		}
		return backend, nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}
