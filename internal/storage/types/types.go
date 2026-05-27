// Package types defines shared value types for storage backends.
// It is a leaf package with no dependencies on other internal packages,
// allowing sub-packages (local, s3, gdrive, azure) and the parent storage
// package to share these types without import cycles.
package types

import "time"

// RepoStatus summarises the current state of a storage repository.
//
// Used only by the `sentinel storage status` command; not part of the
// StorageBackend port surface.
type RepoStatus struct {
	Reachable      bool
	BackupCount    int
	TotalSizeBytes int64
	LastBackup     *time.Time // nil if no backups exist
	Error          string
}
