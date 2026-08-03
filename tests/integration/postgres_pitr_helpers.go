package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PITRFixture defines reusable metadata for PostgreSQL PITR integration tests.
type PITRFixture struct {
	BackupID       string
	Database       string
	DatabaseType   string
	WindowStartUTC time.Time
	WindowEndUTC   time.Time
	TargetTimeUTC  time.Time
	TimelineID     string
	WALPrefix      string
}

// NewPITRFixture creates a deterministic PITR fixture for integration tests.
func NewPITRFixture() PITRFixture {
	start := time.Date(2026, 3, 20, 20, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 20, 23, 59, 0, 0, time.UTC)
	return PITRFixture{
		BackupID:       "postgres-base-001",
		Database:       "appdb",
		DatabaseType:   "postgres",
		WindowStartUTC: start,
		WindowEndUTC:   end,
		TargetTimeUTC:  start.Add(2 * time.Hour),
		TimelineID:     "1",
		WALPrefix:      "wal/archive/appdb",
	}
}

// WritePITRManifest writes a minimal manifest with advanced PITR metadata for tests.
func WritePITRManifest(t *testing.T, dir string, fixture PITRFixture) string {
	t.Helper()

	manifestPath := filepath.Join(dir, "backup.manifest.json")
	payload := map[string]any{
		"backup_id":     fixture.BackupID,
		"database":      fixture.Database,
		"database_type": fixture.DatabaseType,
		"created_at":    fixture.WindowStartUTC.Format(time.RFC3339),
		"size_bytes":    1024,
		"hash": map[string]any{
			"algorithm": "sha256",
			"value":     "fixture-hash",
		},
		"advanced_restore": map[string]any{
			"capabilities":                    []string{"full", "pitr"},
			"initial_release_supported":       true,
			"recoverable_window_start_utc":    fixture.WindowStartUTC.Format(time.RFC3339),
			"recoverable_window_end_utc":      fixture.WindowEndUTC.Format(time.RFC3339),
			"base_backup_kind":                "physical",
			"requires_integrity_verification": true,
			"postgres_recovery": map[string]any{
				"timeline_id":           fixture.TimelineID,
				"wal_start_lsn":         "0/1000000",
				"wal_end_lsn":           "0/2000000",
				"backup_start_time_utc": fixture.WindowStartUTC.Format(time.RFC3339),
				"backup_end_time_utc":   fixture.WindowEndUTC.Format(time.RFC3339),
				"wal_archive_prefix":    fixture.WALPrefix,
			},
		},
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal fixture manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0600); err != nil {
		t.Fatalf("failed to write fixture manifest: %v", err)
	}

	return manifestPath
}
