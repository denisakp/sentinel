package retention_test

import (
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/retention"
)

func TestCalculateCandidatesKeepLast(t *testing.T) {
	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	records := []retention.BackupRecord{
		{FilePath: "a", Timestamp: now.Add(-1 * time.Hour), FileSize: 100, Status: "success"},
		{FilePath: "b", Timestamp: now.Add(-2 * time.Hour), FileSize: 100, Status: "success"},
		{FilePath: "c", Timestamp: now.Add(-3 * time.Hour), FileSize: 100, Status: "success"},
	}

	candidates := retention.CalculateCandidates(records, retention.Policy{KeepLast: 1}, now)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
}

func TestCalculateCandidatesKeepDays(t *testing.T) {
	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	records := []retention.BackupRecord{
		{FilePath: "recent", Timestamp: now.Add(-24 * time.Hour), FileSize: 100, Status: "success"},
		{FilePath: "old", Timestamp: now.Add(-10 * 24 * time.Hour), FileSize: 100, Status: "success"},
	}

	candidates := retention.CalculateCandidates(records, retention.Policy{KeepDays: 7}, now)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].FilePath != "old" {
		t.Fatalf("expected 'old' to be deleted")
	}
}

func TestCalculateCandidatesCumulativeKeepLastAndKeepDays(t *testing.T) {
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	records := []retention.BackupRecord{
		{FilePath: "newest", Timestamp: now.Add(-1 * time.Hour), FileSize: 100, Status: "success"},
		{FilePath: "recent", Timestamp: now.Add(-48 * time.Hour), FileSize: 100, Status: "success"},
		{FilePath: "old-but-in-last", Timestamp: now.Add(-72 * time.Hour), FileSize: 100, Status: "success"},
		{FilePath: "old", Timestamp: now.Add(-10 * 24 * time.Hour), FileSize: 100, Status: "success"},
	}

	// keep_last=2 marks old-but-in-last and old.
	// keep_days=7 marks old.
	// Combined result remains cumulative and de-duplicated.
	candidates := retention.CalculateCandidates(records, retention.Policy{KeepLast: 2, KeepDays: 7}, now)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 cumulative candidates, got %d", len(candidates))
	}

	paths := map[string]bool{}
	for _, cand := range candidates {
		paths[cand.FilePath] = true
	}
	if !paths["old"] || !paths["old-but-in-last"] {
		t.Fatalf("unexpected cumulative candidates: %#v", candidates)
	}
}
