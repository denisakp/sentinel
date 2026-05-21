package monitor

import (
	"strconv"
	"strings"
	"testing"
)

// TestBinarySchemaVersionMatchesMigrations asserts the invariant that
// BinarySchemaVersion equals the highest version number present under
// migrations/. Forgetting to bump the constant after adding a migration
// (or vice versa) fails here.
func TestBinarySchemaVersionMatchesMigrations(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}

	highest := 0
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		count++
		parts := strings.SplitN(strings.TrimSuffix(e.Name(), ".sql"), "_", 2)
		if len(parts) != 2 {
			t.Fatalf("unexpected migration name %q", e.Name())
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			t.Fatalf("non-numeric version prefix in %q: %v", e.Name(), err)
		}
		if n > highest {
			highest = n
		}
	}

	if count != BinarySchemaVersion {
		t.Errorf("found %d migration files but BinarySchemaVersion=%d; expected equality", count, BinarySchemaVersion)
	}
	if highest != BinarySchemaVersion {
		t.Errorf("highest migration version=%d but BinarySchemaVersion=%d; expected equality", highest, BinarySchemaVersion)
	}
}
