package storagetesting_test

import (
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
)

// Compile-time conformance assertions. The cross-adapter contract suite
// (TestStorageContract/mock/*) at internal/adapters/storage/contract_test.go
// remains the authoritative behavioural conformance check; these var
// declarations only guarantee that MockBackend's method set still satisfies
// the ports.
var (
	_ ports.StorageBackend = (*storagetesting.MockBackend)(nil)
	_ ports.StatusReporter = (*storagetesting.MockBackend)(nil)
)
