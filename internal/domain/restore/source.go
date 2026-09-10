package restore

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/denisakp/sentinel/internal/ports"
)

// manifestSuffix marks the sidecar a backup writes next to its artifact.
// A sidecar is never itself a chain artifact, so it is excluded from
// candidate matching unless the caller named one explicitly (#150).
const manifestSuffix = ".manifest.json"

func ResolveChainObject(backupID string, objects []ports.StorageObject) (ports.StorageObject, error) {
	if backupID == "" {
		return ports.StorageObject{}, fmt.Errorf("resolve chain object: empty backup id")
	}

	var exact []ports.StorageObject
	for _, obj := range objects {
		if obj.Path == backupID {
			exact = append(exact, obj)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}

	// A backup records its artifact as <local_path>/<out_name>, while a
	// restore source lists relative to its own local_path. The id is then
	// "backups/shop.sql" and the object is "shop.sql": the same file seen
	// from two roots. Compare on the final element so either side may carry
	// the deeper prefix. Two files that share a name in different
	// directories stay ambiguous rather than resolving by position.
	wantManifest := strings.HasSuffix(backupID, manifestSuffix)
	idBase := filepath.Base(backupID)

	var candidates []ports.StorageObject
	for _, obj := range objects {
		if strings.HasSuffix(obj.Path, manifestSuffix) != wantManifest {
			continue
		}
		if matchesBackupIDBoundary(filepath.Base(obj.Path), idBase) {
			candidates = append(candidates, obj)
		}
	}

	switch len(candidates) {
	case 0:
		return ports.StorageObject{}, fmt.Errorf("%w: %s", ErrSourceObjectNotFound, backupID)
	case 1:
		return candidates[0], nil
	default:
		paths := make([]string, 0, len(candidates))
		for _, c := range candidates {
			paths = append(paths, c.Path)
		}
		sort.Strings(paths)
		return ports.StorageObject{}, fmt.Errorf("%w: %s matches multiple objects: %s", ErrAmbiguousBackupID, backupID, strings.Join(paths, ", "))
	}
}

func matchesBackupIDBoundary(base, backupID string) bool {
	if base == backupID {
		return true
	}
	if len(base) <= len(backupID) {
		return false
	}
	if base[:len(backupID)] != backupID {
		return false
	}
	return base[len(backupID)] == '.'
}
