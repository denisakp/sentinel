package storage_test

// Storage backend contract test suite — the executable form of
// specs/011-storage-backend-contract-tests/contracts/storage-port.md.
//
// Vocabulary mapping (port ↔ current method):
//   Put   ↔ Upload
//   Get   ↔ Download
//   Stat  ↔ Exists
//   List  ↔ List
//   Delete↔ Delete
//
// Per-backend type assertions (LocalBackend, S3Backend, GDriveBackend,
// AzureBlobBackend, GCSBackend) live in backend_test.go — do not duplicate.
//
// Rule → Case coverage (see contracts/storage-port.md):
//   R1 → C1, C2, C10
//   R2 → C4
//   R3 → C3
//   R4 → C5, C6
//   R5 → C7, C8
//   R6 → C9
//   R7 → C11
//   R8 → C10
//   R9 → C12

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/denisakp/sentinel/internal/storage/local"
	"github.com/denisakp/sentinel/internal/storage/storagetesting"
	"github.com/denisakp/sentinel/internal/ports"
)

// Compile-time assertion that MockBackend satisfies the port. Kept here
// rather than in storagetesting/ to honour the import-cycle rule
// (sub-packages of internal/storage must not import internal/storage).
var _ ports.StorageBackend = (*storagetesting.MockBackend)(nil)

// ContractCase is one row of the contract table. Adding a case = adding a row.
type ContractCase struct {
	Name string
	Run  func(t *testing.T, h *caseHarness)
}

// caseHarness is the per-case helper passed to every contract Run func.
// It owns a temp directory for file I/O staging plus the backend under test.
type caseHarness struct {
	ctx         context.Context
	backend     ports.StorageBackend
	tmpDir      string
	backendName string
}

// writeTmpFile writes data to a uniquely named file under the harness temp
// dir and returns the absolute path.
func (h *caseHarness) writeTmpFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(h.tmpDir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("writeTmpFile %q: %v", p, err)
	}
	return p
}

// allCases is the canonical contract case table.
var allCases = []ContractCase{
	{Name: "roundtrip_small", Run: caseRoundtripSmall},
	{Name: "roundtrip_empty", Run: caseRoundtripEmpty},
	{Name: "not_found_get", Run: caseNotFoundGet},
	{Name: "not_found_exists", Run: caseNotFoundExists},
	{Name: "idempotent_delete", Run: caseIdempotentDelete},
	{Name: "delete_nonexistent", Run: caseDeleteNonexistent},
	{Name: "list_empty_prefix", Run: caseListEmptyPrefix},
	{Name: "list_populated_prefix", Run: caseListPopulatedPrefix},
	{Name: "keys_with_special_chars", Run: caseKeysWithSpecialChars},
	{Name: "large_object_streaming", Run: caseLargeObjectStreaming},
	{Name: "partial_failure_visibility", Run: casePartialFailureVisibility},
	{Name: "concurrent_put_same_key", Run: caseConcurrentPutSameKey},
}

// runContractSuite executes every case in the contract table against the
// supplied backend. It is the single entry point used by both the default
// and the integration-tagged wiring (FR-012 / SC-004).
func runContractSuite(t *testing.T, name string, backend ports.StorageBackend) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		for _, c := range allCases {
			c := c
			t.Run(c.Name, func(t *testing.T) {
				h := &caseHarness{
					ctx:         context.Background(),
					backend:     backend,
					tmpDir:      t.TempDir(),
					backendName: name,
				}
				c.Run(t, h)
			})
		}
	})
}

// TestStorageContract is the default-tier entry: local + mock.
func TestStorageContract(t *testing.T) {
	runContractSuite(t, "local", setupLocal(t))
	runContractSuite(t, "mock", setupMock(t))
}

func setupLocal(t *testing.T) ports.StorageBackend {
	t.Helper()
	return local.NewLocalBackend(t.TempDir())
}

func setupMock(t *testing.T) ports.StorageBackend {
	t.Helper()
	return storagetesting.NewMockBackend()
}

// ────────────────────────────────────────────────────────────────────────────
// Contract cases
// ────────────────────────────────────────────────────────────────────────────

func caseRoundtripSmall(t *testing.T, h *caseHarness) {
	key := storagetesting.RandKey(t, "roundtrip_small")
	payload := bytes.Repeat([]byte("sentinel-"), 128) // ~1 KiB
	src := h.writeTmpFile(t, "src.bin", payload)

	if err := h.backend.Upload(h.ctx, src, key); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	ok, err := h.backend.Exists(h.ctx, key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatal("Exists() = false after Upload, want true")
	}

	dst := filepath.Join(h.tmpDir, "dst.bin")
	if err := h.backend.Download(h.ctx, key, dst); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Download mismatch: got %d bytes, want %d", len(got), len(payload))
	}

	objects, err := h.backend.List(h.ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !containsKey(objects, key) {
		t.Fatalf("List did not contain uploaded key %q", key)
	}
}

func caseRoundtripEmpty(t *testing.T, h *caseHarness) {
	key := storagetesting.RandKey(t, "roundtrip_empty")
	src := h.writeTmpFile(t, "empty.bin", []byte{})

	if err := h.backend.Upload(h.ctx, src, key); err != nil {
		t.Fatalf("Upload(empty): %v", err)
	}

	ok, err := h.backend.Exists(h.ctx, key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatal("Exists() = false for zero-byte object, want true (size 0, not absent)")
	}

	dst := filepath.Join(h.tmpDir, "empty-dst.bin")
	if err := h.backend.Download(h.ctx, key, dst); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 bytes, got %d", len(got))
	}
}

func caseNotFoundGet(t *testing.T, h *caseHarness) {
	key := storagetesting.RandKey(t, "missing")
	dst := filepath.Join(h.tmpDir, "out.bin")
	err := h.backend.Download(h.ctx, key, dst)
	if err == nil {
		t.Fatal("Download(missing) returned nil error, want not-found-shaped error")
	}
	// Tolerated forms: os.ErrNotExist (mock, local), or any non-nil error
	// distinguishable from a transport panic. The contract does not yet
	// mandate errors.Is(fs.ErrNotExist) — see R3.
	_ = errors.Is(err, fs.ErrNotExist)
}

func caseNotFoundExists(t *testing.T, h *caseHarness) {
	key := storagetesting.RandKey(t, "missing")
	ok, err := h.backend.Exists(h.ctx, key)
	if err != nil {
		t.Fatalf("Exists(missing) returned error %v; want (false, nil)", err)
	}
	if ok {
		t.Fatal("Exists(missing) = true, want false")
	}
}

func caseIdempotentDelete(t *testing.T, h *caseHarness) {
	key := storagetesting.RandKey(t, "idempotent_delete")
	src := h.writeTmpFile(t, "tmp.bin", []byte("payload"))
	if err := h.backend.Upload(h.ctx, src, key); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if err := h.backend.Delete(h.ctx, key); err != nil {
		t.Fatalf("Delete #1: %v", err)
	}
	if err := h.backend.Delete(h.ctx, key); err != nil {
		t.Fatalf("Delete #2 (idempotent): %v", err)
	}
	ok, err := h.backend.Exists(h.ctx, key)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if ok {
		t.Fatal("Exists() = true after Delete, want false")
	}
}

func caseDeleteNonexistent(t *testing.T, h *caseHarness) {
	key := storagetesting.RandKey(t, "never_existed")
	if err := h.backend.Delete(h.ctx, key); err != nil {
		t.Fatalf("Delete(never-existed) returned error %v; want nil per R4", err)
	}
}

func caseListEmptyPrefix(t *testing.T, h *caseHarness) {
	prefix := storagetesting.RandKey(t, "empty_prefix") + "/"
	objects, err := h.backend.List(h.ctx, prefix)
	if err != nil {
		t.Fatalf("List(empty prefix) returned error %v; want (empty, nil)", err)
	}
	if len(objects) != 0 {
		t.Fatalf("List(empty prefix) returned %d objects, want 0", len(objects))
	}
}

func caseListPopulatedPrefix(t *testing.T, h *caseHarness) {
	prefix := storagetesting.RandKey(t, "list_pop") + "/"
	keys := []string{prefix + "a", prefix + "b", prefix + "c"}
	src := h.writeTmpFile(t, "tmp.bin", []byte("x"))
	for _, k := range keys {
		if err := h.backend.Upload(h.ctx, src, k); err != nil {
			t.Fatalf("Upload %q: %v", k, err)
		}
	}
	// Sibling key under different prefix — must not appear.
	siblingKey := storagetesting.RandKey(t, "sibling") + "/y"
	if err := h.backend.Upload(h.ctx, src, siblingKey); err != nil {
		t.Fatalf("Upload sibling: %v", err)
	}

	objects, err := h.backend.List(h.ctx, prefix)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, 0, len(objects))
	for _, o := range objects {
		got = append(got, o.Path)
	}
	sort.Strings(got)
	want := append([]string(nil), keys...)
	sort.Strings(want)
	if !equalStrings(got, want) {
		t.Fatalf("List(prefix=%q) = %v, want %v", prefix, got, want)
	}
}

func caseKeysWithSpecialChars(t *testing.T, h *caseHarness) {
	prefix := storagetesting.RandKey(t, "special")
	keys := []string{
		prefix + "/with spaces.bin",
		prefix + "/nested/path/file.bin",
		prefix + "/unicode-éñ漢.bin",
	}
	src := h.writeTmpFile(t, "tmp.bin", []byte("hello"))
	for _, k := range keys {
		if err := h.backend.Upload(h.ctx, src, k); err != nil {
			t.Fatalf("Upload %q: %v", k, err)
		}
		ok, err := h.backend.Exists(h.ctx, k)
		if err != nil || !ok {
			t.Fatalf("Exists(%q) = (%v, %v), want (true, nil)", k, ok, err)
		}
		dst := filepath.Join(h.tmpDir, "out.bin")
		if err := h.backend.Download(h.ctx, k, dst); err != nil {
			t.Fatalf("Download(%q): %v", k, err)
		}
		got, _ := os.ReadFile(dst)
		if string(got) != "hello" {
			t.Fatalf("Download(%q) content mismatch: %q", k, string(got))
		}
		if err := h.backend.Delete(h.ctx, k); err != nil {
			t.Fatalf("Delete(%q): %v", k, err)
		}
	}
}

func caseLargeObjectStreaming(t *testing.T, h *caseHarness) {
	key := storagetesting.RandKey(t, "large")
	src := filepath.Join(h.tmpDir, "large.bin")
	if err := os.WriteFile(src, storagetesting.LargePayloadBytes(), 0o644); err != nil {
		t.Fatalf("write large src: %v", err)
	}

	// Heap-delta guard for R8. Mock is excluded by design: it stores objects
	// in an in-memory map, so peak heap necessarily grows by the payload
	// size; the guard is meaningful for adapters that stream to a
	// non-memory backing store (local: file copy). Cloud SDK clients hold
	// opaque allocations and are also skipped per tasks.md T035.
	memGuard := h.backendName == "local"
	var beforeHeap uint64
	if memGuard {
		var ms runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&ms)
		beforeHeap = ms.HeapAlloc
	}

	if err := h.backend.Upload(h.ctx, src, key); err != nil {
		t.Fatalf("Upload(large): %v", err)
	}

	dst := filepath.Join(h.tmpDir, "large-dst.bin")
	if err := h.backend.Download(h.ctx, key, dst); err != nil {
		t.Fatalf("Download(large): %v", err)
	}

	if memGuard {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		delta := int64(ms.HeapAlloc) - int64(beforeHeap)
		const threshold = int64(96 * 1024 * 1024) // 1.5× payload
		if delta > threshold {
			t.Errorf("HeapAlloc delta %d bytes exceeds %d byte streaming budget (R8)", delta, threshold)
		}
	}

	f, err := os.Open(dst)
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	defer f.Close()
	gotHash, err := storagetesting.StreamingSHA256(f)
	if err != nil {
		t.Fatalf("StreamingSHA256: %v", err)
	}
	wantHash := storagetesting.LargePayloadSHA256()
	if encodeHex(gotHash) != wantHash {
		t.Fatalf("large-object SHA-256 mismatch: got %s want %s", encodeHex(gotHash), wantHash)
	}
}

func casePartialFailureVisibility(t *testing.T, h *caseHarness) {
	// Two phases:
	// (a) failed upload to a fresh key — post-state must be "no object".
	// (b) failed upload over an existing key — post-state must be the
	//     previous bytes intact OR no object (R7 allows either).
	//
	// Backends accept a file path for Upload. The simplest cross-backend
	// failure injection is a source path that does not exist: every
	// adapter opens the source first and returns an error before
	// committing any bytes to the destination. That exercises R7's
	// observable guarantee without coupling to backend internals.
	freshKey := storagetesting.RandKey(t, "partial_fresh")
	missingSrc := filepath.Join(h.tmpDir, "does-not-exist.bin")
	if err := h.backend.Upload(h.ctx, missingSrc, freshKey); err == nil {
		t.Fatal("Upload(missing src) returned nil error; expected failure")
	}
	ok, err := h.backend.Exists(h.ctx, freshKey)
	if err != nil {
		t.Fatalf("Exists after failed upload: %v", err)
	}
	if ok {
		t.Fatal("Exists() = true after failed Upload to fresh key — R7 violation (partial object visible)")
	}

	// Phase (b): pre-populate then attempt failing overwrite.
	overKey := storagetesting.RandKey(t, "partial_over")
	prev := []byte("previous-good-payload")
	prevSrc := h.writeTmpFile(t, "prev.bin", prev)
	if err := h.backend.Upload(h.ctx, prevSrc, overKey); err != nil {
		t.Fatalf("pre-Upload: %v", err)
	}
	if err := h.backend.Upload(h.ctx, missingSrc, overKey); err == nil {
		t.Fatal("Upload(missing src) over existing key returned nil error")
	}
	ok, err = h.backend.Exists(h.ctx, overKey)
	if err != nil {
		t.Fatalf("Exists post-fail (over): %v", err)
	}
	if ok {
		dst := filepath.Join(h.tmpDir, "over-dst.bin")
		if err := h.backend.Download(h.ctx, overKey, dst); err != nil {
			t.Fatalf("Download post-fail: %v", err)
		}
		got, _ := os.ReadFile(dst)
		if !bytes.Equal(got, prev) {
			t.Fatalf("R7 violation: post-fail object differs from previous bytes (got %d, want %d)", len(got), len(prev))
		}
	}
}

func caseConcurrentPutSameKey(t *testing.T, h *caseHarness) {
	key := storagetesting.RandKey(t, "concurrent")
	payloadA := bytes.Repeat([]byte("A"), 4096)
	payloadB := bytes.Repeat([]byte("B"), 4096)
	srcA := h.writeTmpFile(t, "a.bin", payloadA)
	srcB := h.writeTmpFile(t, "b.bin", payloadB)

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	go func() {
		defer wg.Done()
		errs[0] = h.backend.Upload(h.ctx, srcA, key)
	}()
	go func() {
		defer wg.Done()
		errs[1] = h.backend.Upload(h.ctx, srcB, key)
	}()
	wg.Wait()

	if errs[0] != nil && errs[1] != nil {
		t.Fatalf("both concurrent Uploads failed: %v / %v", errs[0], errs[1])
	}

	dst := filepath.Join(h.tmpDir, "out.bin")
	if err := h.backend.Download(h.ctx, key, dst); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, payloadA) && !bytes.Equal(got, payloadB) {
		t.Fatalf("R9 violation: final object is neither A nor B (len=%d)", len(got))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers (test-local)
// ────────────────────────────────────────────────────────────────────────────

func containsKey(objects []ports.StorageObject, key string) bool {
	for _, o := range objects {
		if o.Path == key || strings.HasSuffix(o.Path, key) {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func encodeHex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}
