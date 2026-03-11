package storage

import (
	"context"

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
