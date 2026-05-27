package storage_test

import (
	"strings"
	"testing"

	storage "github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
)

// TestNewBackend_Local exercises the only fully-constructable case under
// unit-test conditions (no external credentials). The four remote backends
// have their own integration coverage under build tag "integration"; see
// contract_integration_test.go.
func TestNewBackend_Local(t *testing.T) {
	root := t.TempDir()
	backend, err := storage.NewBackend(&storage.BackendParams{
		StorageType: "local",
		LocalPath:   root,
	})
	if err != nil {
		t.Fatalf("NewBackend(local) error = %v", err)
	}
	if backend == nil {
		t.Fatal("NewBackend(local) returned nil backend")
	}
	if _, ok := backend.(ports.StorageBackend); !ok {
		t.Errorf("returned value does not satisfy ports.StorageBackend")
	}
	if _, ok := backend.(ports.StatusReporter); !ok {
		t.Errorf("returned value does not satisfy ports.StatusReporter")
	}
}

// TestNewBackend_DefaultLocal verifies empty StorageType defaults to "local".
func TestNewBackend_DefaultLocal(t *testing.T) {
	backend, err := storage.NewBackend(&storage.BackendParams{LocalPath: t.TempDir()})
	if err != nil {
		t.Fatalf("NewBackend(default) error = %v", err)
	}
	if backend == nil {
		t.Fatal("NewBackend(default) returned nil")
	}
}

// TestNewBackend_UnsupportedType verifies the error wording mandated by
// spec 029 FR-005.
func TestNewBackend_UnsupportedType(t *testing.T) {
	tests := []string{"unknown", "ftp", "bogus", "S3"}
	for _, st := range tests {
		t.Run(st, func(t *testing.T) {
			_, err := storage.NewBackend(&storage.BackendParams{StorageType: st})
			if err == nil {
				t.Fatalf("NewBackend(%q) returned nil error, want unsupported", st)
			}
			want := "unsupported storage type: " + st
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err.Error(), want)
			}
		})
	}
}

// TestNewBackend_RemoteTypesValidate checks that requesting each remote
// backend with empty credentials surfaces a construction error from the
// underlying SDK (or succeeds when the SDK defers validation to first use).
// The point is to prove the case arms are wired — not to validate SDK
// behaviour itself.
func TestNewBackend_RemoteTypesWired(t *testing.T) {
	cases := []struct {
		name string
		typ  string
	}{
		{"s3", "s3"},
		{"gcs", "gcs"},
		{"google-drive", "google-drive"},
		{"azure", "azure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := storage.NewBackend(&storage.BackendParams{StorageType: tc.typ})
			// We accept either a successful construction (SDK deferred) or
			// any non-unsupported error. The thing we MUST NOT see is the
			// "unsupported storage type" wording — that would mean the
			// switch arm is missing.
			if err != nil && strings.HasPrefix(err.Error(), "unsupported storage type:") {
				t.Fatalf("NewBackend(%q) returned %q — switch arm missing?", tc.typ, err.Error())
			}
		})
	}
}
