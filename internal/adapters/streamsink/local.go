package streamsink

import "github.com/denisakp/sentinel/internal/adapters/storage"

// IsLocal reports whether params name local storage, where the artifact's
// destination is a plain file the dump can be streamed into directly.
//
// An empty StorageType means local, matching storage.NewStorage.
func IsLocal(p *storage.BackendParams) bool {
	return p != nil && (p.StorageType == "" || p.StorageType == "local")
}
