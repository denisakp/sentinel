package storage

import (
	"context"

	storagetypes "github.com/denisakp/sentinel/internal/storage/types"
)

// RepoStatus is re-exported from storage/types for backward compatibility.
type RepoStatus = storagetypes.RepoStatus

// Repository provides lifecycle management for a storage destination.
// Prune is excluded from v1.1.0 (see clarification Q3 in spec).
type Repository interface {
	Init(ctx context.Context) error
	Status(ctx context.Context) (*RepoStatus, error)
	Lock(ctx context.Context) error
	Unlock(ctx context.Context) error
	Verify(ctx context.Context, backupID string) error
}
