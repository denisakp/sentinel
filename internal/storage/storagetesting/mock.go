package storagetesting

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	storagetypes "github.com/denisakp/sentinel/internal/storage/types"
)

// MockBackend is an in-memory storage backend used by the contract suite
// (self-conformance) and by higher-level orchestration tests that must stay
// hermetic. Safe for concurrent use.
//
// It implements the methods of internal/storage.StorageBackend; the
// compile-time assertion lives in the storage_test package to avoid a
// sub-package → parent-package import (see CLAUDE.md import-cycle rule).
type MockBackend struct {
	mu      sync.RWMutex
	objects map[string]mockObject
}

type mockObject struct {
	data    []byte
	modTime time.Time
}

// NewMockBackend constructs an empty in-memory backend.
func NewMockBackend() *MockBackend {
	return &MockBackend{objects: make(map[string]mockObject)}
}

func (m *MockBackend) Upload(_ context.Context, src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("mock: failed to read source %q: %w", src, err)
	}
	m.mu.Lock()
	m.objects[dest] = mockObject{data: data, modTime: time.Now().UTC()}
	m.mu.Unlock()
	return nil
}

func (m *MockBackend) Download(_ context.Context, src, dest string) error {
	m.mu.RLock()
	obj, ok := m.objects[src]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("mock: object not found: %q: %w", src, os.ErrNotExist)
	}
	data := append([]byte(nil), obj.data...)
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("mock: failed to write destination %q: %w", dest, err)
	}
	return nil
}

func (m *MockBackend) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	delete(m.objects, path)
	m.mu.Unlock()
	return nil
}

func (m *MockBackend) List(_ context.Context, prefix string) ([]storagetypes.StorageObject, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]storagetypes.StorageObject, 0)
	for k, obj := range m.objects {
		if prefix != "" && !strings.HasPrefix(k, prefix) {
			continue
		}
		out = append(out, storagetypes.StorageObject{
			Path:         k,
			SizeBytes:    int64(len(obj.data)),
			LastModified: obj.modTime,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (m *MockBackend) Exists(_ context.Context, path string) (bool, error) {
	m.mu.RLock()
	_, ok := m.objects[path]
	m.mu.RUnlock()
	return ok, nil
}

// PutBytes seeds the store without going through a temp file.
func (m *MockBackend) PutBytes(key string, data []byte) {
	cp := append([]byte(nil), data...)
	m.mu.Lock()
	m.objects[key] = mockObject{data: cp, modTime: time.Now().UTC()}
	m.mu.Unlock()
}

// GetBytes returns the bytes stored at key, if present.
func (m *MockBackend) GetBytes(key string) ([]byte, bool) {
	m.mu.RLock()
	obj, ok := m.objects[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return append([]byte(nil), obj.data...), true
}
