package ports

import (
	"context"
	"time"
)

// StorageBackend abstracts *internal/storage/local.LocalBackend (current
// concrete implementation) and the four siblings (sentinel_s3.S3Backend,
// gcs.GCSBackend, gdrive.GDriveBackend, azure.AzureBackend).
//
// The shape is intentionally mirrored verbatim from the current backend.go;
// splitting into reader/writer/lister sub-interfaces is deferred.
type StorageBackend interface {
	Upload(ctx context.Context, src, dest string) error
	Download(ctx context.Context, src, dest string) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]StorageObject, error)
	Exists(ctx context.Context, path string) (bool, error)
}

// StorageObject represents a single object returned by a StorageBackend List call.
//
// Relocated from internal/storage/types/types.go (single source of truth).
type StorageObject struct {
	Path         string
	SizeBytes    int64
	LastModified time.Time
	ETag         string
}

// RepoStatus summarises the current state of a storage repository.
//
// Returned by the Status method on the StatusReporter sibling port (see
// below). Relocated from internal/storage/types/types.go;
// internal/storage/types/ is fully removed once every importer points here.
type RepoStatus struct {
	Reachable      bool
	BackupCount    int
	TotalSizeBytes int64
	LastBackup     *time.Time // nil if no backups exist
	Error          string
}

// StatusReporter is a sibling port to StorageBackend exposing the
// Status(ctx) (RepoStatus, error) method that all five concrete backends
// already implement. Kept as a separate interface (rather than embedded in
// StorageBackend) so test fakes can opt in and so the StorageBackend
// shape stays verbatim.
//
// Driving adapters that need status type-assert on the result of
// internal/adapters/storage.NewBackend:
//
//	if sr, ok := backend.(ports.StatusReporter); ok {
//	    st, _ := sr.Status(ctx)
//	    // render st
//	}
type StatusReporter interface {
	Status(ctx context.Context) (RepoStatus, error)
}
