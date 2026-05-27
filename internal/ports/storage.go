package ports

import (
	"context"
	"time"
)

// StorageBackend abstracts *internal/storage/local.LocalBackend (current
// concrete implementation) and the four siblings (sentinel_s3.S3Backend,
// gcs.GCSBackend, gdrive.GDriveBackend, azure.AzureBackend).
//
// The shape is intentionally mirrored verbatim from the current backend.go
// in spec 028; splitting into reader/writer/lister sub-interfaces is
// deferred to the storage adapter spec (029) per the spec's Assumptions.
type StorageBackend interface {
	Upload(ctx context.Context, src, dest string) error
	Download(ctx context.Context, src, dest string) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]StorageObject, error)
	Exists(ctx context.Context, path string) (bool, error)
}

// StorageObject represents a single object returned by a StorageBackend List call.
//
// Relocated from internal/storage/types/types.go (single source of truth per
// spec 028 FR-003a). RepoStatus stays in internal/storage/types/ — it is a
// status-command rendering artefact, not a port-method parameter or return.
type StorageObject struct {
	Path         string
	SizeBytes    int64
	LastModified time.Time
	ETag         string
}
