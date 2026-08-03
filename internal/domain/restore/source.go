package restore

// Pure source-resolution helpers. Relocated from internal/restore/source.go
// (the I/O staging half lives in the driving runtime,
// internal/adapters/restore/runtime).

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/denisakp/sentinel/internal/ports"
)

// ResolveChainObject locates the single storage object that corresponds to
// the requested backup ID. An exact full-path match wins outright. Otherwise
// the match is performed against the filename component (filepath.Base) of
// each candidate: the filename must equal the backup ID or begin with the
// backup ID followed immediately by a '.' (the extension boundary).
// Comparisons are byte-for-byte and case-sensitive; intermediate path
// segments are never matched. Returns ErrAmbiguousBackupID (wrapped with the
// candidate list) when two or more objects satisfy the boundary rule and no
// exact full-path match exists, ErrSourceObjectNotFound when no object
// satisfies the rule.
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

	var candidates []ports.StorageObject
	for _, obj := range objects {
		if matchesBackupIDBoundary(filepath.Base(obj.Path), backupID) {
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

// matchesBackupIDBoundary reports whether base equals backupID or begins
// with backupID followed immediately by '.' (the extension boundary).
// Byte-for-byte; case-sensitive. Underscore is NOT a boundary: backup IDs
// themselves may contain underscores (e.g. b_01), so allowing '_' would let
// b_01 match b_01_extra — an exact collision that must be forbidden.
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
