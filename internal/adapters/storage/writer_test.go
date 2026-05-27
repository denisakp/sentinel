package storage_test

import (
	"strings"
	"testing"

	storage "github.com/denisakp/sentinel/internal/adapters/storage"
)

// TestNewStorage_Local verifies the legacy Storage write interface for
// the local backend (the only one constructable without external creds).
// We avoid calling GetBackupPath here — its current implementation has a
// pre-existing side effect of creating a directory relative to CWD, which
// is out of scope for spec 029. Construction smoke is sufficient.
func TestNewStorage_Local(t *testing.T) {
	s, err := storage.NewStorage(&storage.BackendParams{
		StorageType: "local",
		LocalPath:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewStorage(local) error = %v", err)
	}
	if s == nil {
		t.Fatal("NewStorage(local) returned nil")
	}
}

// TestNewStorage_DefaultLocal verifies empty StorageType defaults to "local".
func TestNewStorage_DefaultLocal(t *testing.T) {
	s, err := storage.NewStorage(&storage.BackendParams{LocalPath: t.TempDir()})
	if err != nil {
		t.Fatalf("NewStorage(default) error = %v", err)
	}
	if s == nil {
		t.Fatal("NewStorage(default) returned nil")
	}
}

// TestNewStorage_UnsupportedType matches the wording mandated by FR-005.
// Note: per FR-010/FR-011 zero-diff, "azure" is INTENTIONALLY treated as
// unsupported by NewStorage (the legacy storage.NewStorage also did not
// handle azure). Use NewBackend if you need azure.
func TestNewStorage_UnsupportedType(t *testing.T) {
	tests := []string{"azure", "unknown", "ftp", "S3"}
	for _, st := range tests {
		t.Run(st, func(t *testing.T) {
			_, err := storage.NewStorage(&storage.BackendParams{StorageType: st})
			if err == nil {
				t.Fatalf("NewStorage(%q) returned nil error, want unsupported", st)
			}
			want := "unsupported storage type: " + st
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err.Error(), want)
			}
		})
	}
}

// TestNewStorage_KnownTypesWired ensures each known type the legacy switch
// covered (local, s3, google-drive, gcs) reaches its case arm and is NOT
// reported as unsupported.
func TestNewStorage_KnownTypesWired(t *testing.T) {
	for _, typ := range []string{"local", "s3", "google-drive", "gcs"} {
		t.Run(typ, func(t *testing.T) {
			_, err := storage.NewStorage(&storage.BackendParams{
				StorageType: typ,
				LocalPath:   t.TempDir(),
			})
			if err != nil && strings.HasPrefix(err.Error(), "unsupported storage type:") {
				t.Fatalf("NewStorage(%q) returned %q — switch arm missing?", typ, err.Error())
			}
		})
	}
}
