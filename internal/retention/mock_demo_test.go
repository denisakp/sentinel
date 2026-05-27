package retention_test

import (
	"context"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
)

// TestMockBackendReachableFromRetention demonstrates that the relocated
// storage fake (internal/ports/storagetesting.MockBackend) is reachable from
// the internal/retention domain package without importing any concrete
// adapter under internal/adapters/storage/*. This is the unit-test reach
// promised by spec 029 FR-014 / FR-009.
//
// The retention domain itself currently dispatches deletion through a set of
// single-method interfaces (s3DeleteBackend, gcsDeleteBackend, ...) rather
// than ports.StorageBackend, so this test does not exercise a retention code
// path through the mock; it asserts only that the fake is wired and behaves
// per the contract.
func TestMockBackendReachableFromRetention(t *testing.T) {
	ctx := context.Background()
	mock := storagetesting.NewMockBackend()

	// Seed a fake backup object.
	mock.PutBytes("backups/db1/2026-01-01.sql", []byte("-- backup payload"))

	// List
	objs, err := mock.List(ctx, "backups/")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("List() = %d objects, want 1", len(objs))
	}
	if !strings.HasSuffix(objs[0].Path, ".sql") {
		t.Errorf("List()[0].Path = %q, want suffix .sql", objs[0].Path)
	}

	// Status conformance to ports.StatusReporter.
	var sr ports.StatusReporter = mock
	st, err := sr.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !st.Reachable {
		t.Error("Status().Reachable = false, want true")
	}
	if st.BackupCount != 1 {
		t.Errorf("Status().BackupCount = %d, want 1", st.BackupCount)
	}

	// Delete and re-check.
	if err := mock.Delete(ctx, "backups/db1/2026-01-01.sql"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	ok, err := mock.Exists(ctx, "backups/db1/2026-01-01.sql")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if ok {
		t.Error("Exists() = true after Delete, want false")
	}
}
