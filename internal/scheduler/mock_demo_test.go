package scheduler_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
)

// TestMockBackendReachableFromScheduler demonstrates that the relocated
// storage fake (internal/ports/storagetesting.MockBackend) is reachable from
// the internal/scheduler driver package without importing any concrete
// adapter under internal/adapters/storage/*.
//
// The scheduler today wires storage construction through internal/storage
// (now aliased to internal/adapters/storage) and selects concrete backends
// via config.StorageType; the test exercises the fake's full Upload →
// List → Download → Delete loop against a real temp directory to confirm
// the fake honours the StorageBackend contract end-to-end.
func TestMockBackendReachableFromScheduler(t *testing.T) {
	ctx := context.Background()
	mock := storagetesting.NewMockBackend()

	// var _ assertion (compile-time conformance) lives in
	// internal/ports/storagetesting/mock_test.go; here we exercise it.
	var backend ports.StorageBackend = mock

	src := filepath.Join(t.TempDir(), "schedule-test.sql")
	if err := os.WriteFile(src, []byte("-- scheduler upload payload"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	const dest = "scheduled/job-001/2026-05-27.sql"

	if err := backend.Upload(ctx, src, dest); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	ok, err := backend.Exists(ctx, dest)
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !ok {
		t.Fatal("Exists() = false after Upload")
	}

	objs, err := backend.List(ctx, "scheduled/")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("List() = %d, want 1", len(objs))
	}
	if objs[0].Path != dest {
		t.Errorf("List()[0].Path = %q, want %q", objs[0].Path, dest)
	}

	roundtrip := filepath.Join(t.TempDir(), "downloaded.sql")
	if err := backend.Download(ctx, dest, roundtrip); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	got, err := os.ReadFile(roundtrip)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "-- scheduler upload payload" {
		t.Errorf("Download payload mismatch: got %q", string(got))
	}

	if err := backend.Delete(ctx, dest); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	ok, err = backend.Exists(ctx, dest)
	if err != nil {
		t.Fatalf("Exists() post-delete error = %v", err)
	}
	if ok {
		t.Error("Exists() = true after Delete, want false")
	}
}
