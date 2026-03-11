package retention_test

import (
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/retention"
)

func TestCalculateCandidatesKeepsOne(t *testing.T) {
	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	records := []retention.BackupRecord{
		{FilePath: "a", Timestamp: now.Add(-1 * time.Hour), FileSize: 100, Status: "success"},
		{FilePath: "b", Timestamp: now.Add(-2 * time.Hour), FileSize: 100, Status: "success"},
	}

	candidates := retention.CalculateCandidates(records, retention.Policy{KeepLast: 0, KeepDays: 0}, now)
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(candidates))
	}

	// KeepDays: 1 means delete backups older than 1 day
	// Record b is 2 hours old (within 1 day), record a is 1 hour old (within 1 day)
	// So with KeepDays: 1 and KeepLast: 0, both should be kept (no candidates)
	candidates = retention.CalculateCandidates(records, retention.Policy{KeepLast: 0, KeepDays: 1}, now)
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates (both within 1 day), got %d", len(candidates))
	}

	// KeepDays: 0 with KeepLast: 1 should delete all but the latest
	candidates = retention.CalculateCandidates(records, retention.Policy{KeepLast: 1, KeepDays: 0}, now)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate (keep_last=1), got %d", len(candidates))
	}

	// Test safety rule: if all would be deleted, keep the latest
	// Create 2 old backups (both older than 1 day)
	oldRecords := []retention.BackupRecord{
		{FilePath: "old1", Timestamp: now.Add(-48 * time.Hour), FileSize: 100, Status: "success"},
		{FilePath: "old2", Timestamp: now.Add(-47 * time.Hour), FileSize: 100, Status: "success"},
	}
	candidates = retention.CalculateCandidates(oldRecords, retention.Policy{KeepLast: 0, KeepDays: 1}, now)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate (safety: keep at least one), got %d", len(candidates))
	}
	if candidates[0].FilePath != "old1" {
		t.Fatalf("expected old1 to be candidate, got %s", candidates[0].FilePath)
	}
}
