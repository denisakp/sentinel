package incremental_test

import (
	"testing"
)

// TestMongoDBOplogIncrementalRestore_Integration is a placeholder for the
// MongoDB oplog-based incremental restore integration scenario.
// Skipped until a live MongoDB replica-set environment is available in CI.
func TestMongoDBOplogIncrementalRestore_Integration(t *testing.T) {
	t.Skip("integration: requires live MongoDB replica-set — set SENTINEL_INTEGRATION_MONGO=1 to enable")
}
