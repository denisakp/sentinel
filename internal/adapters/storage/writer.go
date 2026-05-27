package storage

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/adapters/storage/gcs"
	"github.com/denisakp/sentinel/internal/adapters/storage/gdrive"
	"github.com/denisakp/sentinel/internal/adapters/storage/local"
	"github.com/denisakp/sentinel/internal/adapters/storage/s3"
)

// Storage is the driver-side write/read-path interface used by the backup
// pipeline (pre-streaming codepath) and the dump engines.
//
// It is NOT a port: collapsing it into ports.StorageBackend is deferred to a
// future spec (spec 029 Clarification Q1, 2026-05-27). The interface is
// preserved verbatim from internal/storage/storage.go for FR-010/FR-011
// zero-observable-diff during the migration.
type Storage interface {
	// GetBackupPath returns the absolute path to store the backup under
	// outName for filesystem-backed backends, or the equivalent object key
	// for object-store backends.
	GetBackupPath(outName string) (string, error)

	// WriteBackup writes the backup data under outName.
	WriteBackup(data []byte, outName string) error

	// DeleteBackup removes a backup artefact (used by the failure-cleanup
	// path).
	DeleteBackup(ctx context.Context, path string) error
}

// NewStorage constructs the driver-side Storage write helper.
//
// Coverage matches today's internal/storage.NewStorage exactly: local, s3,
// google-drive, gcs. The azure case is intentionally NOT handled here — it
// falls through to the unsupported-type error so the migration preserves
// FR-010/FR-011 zero observable diff (azure was not in the legacy NewStorage
// switch either).
func NewStorage(p *BackendParams) (Storage, error) {
	if p == nil {
		return nil, fmt.Errorf("storage params are required")
	}
	storageType := p.StorageType
	if storageType == "" {
		storageType = "local"
	}

	switch storageType {
	case "local":
		return &local.LocalStorage{}, nil
	case "s3":
		return s3.NewS3Storage(&s3.AmazonS3Storage{
			Bucket:    p.AWSBucket,
			Region:    p.AWSRegion,
			EndPoint:  p.AWSBucketEndpoint,
			AccessKey: p.AWSAccessKeyID,
			SecretKey: p.AWSSecretAccessKey,
		})
	case "google-drive":
		return gdrive.NewGoogleDriveStorage(&gdrive.GoogleDriveStorage{
			FolderId:           p.GoogleDriveFolderId,
			ServiceAccountFile: p.GoogleServiceAccount,
		})
	case "gcs":
		return gcs.NewGoogleCloudStorage(gcs.Config{
			Bucket:          p.GCSBucket,
			ProjectID:       p.GCSProjectID,
			CredentialsFile: p.GCSCredentialsFile,
		})
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}
