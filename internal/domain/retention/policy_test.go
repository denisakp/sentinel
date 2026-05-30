package retention_test

import (
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/domain/retention"
)

func TestCalculateCandidates_KeepLast(t *testing.T) {
	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	records := []retention.BackupRecord{
		{FilePath: "a", Timestamp: now.Add(-1 * time.Hour), Status: "success"},
		{FilePath: "b", Timestamp: now.Add(-2 * time.Hour), Status: "success"},
		{FilePath: "c", Timestamp: now.Add(-3 * time.Hour), Status: "success"},
	}
	got := retention.CalculateCandidates(records, retention.Policy{KeepLast: 1}, now)
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(got))
	}
}

func TestCalculateCandidates_KeepDays(t *testing.T) {
	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	records := []retention.BackupRecord{
		{FilePath: "recent", Timestamp: now.Add(-24 * time.Hour), Status: "success"},
		{FilePath: "old", Timestamp: now.Add(-10 * 24 * time.Hour), Status: "success"},
	}
	got := retention.CalculateCandidates(records, retention.Policy{KeepDays: 7}, now)
	if len(got) != 1 || got[0].FilePath != "old" {
		t.Fatalf("want only 'old' pruned, got %#v", got)
	}
}

func TestCalculateCandidates_EmptyPolicyKeepsAll(t *testing.T) {
	now := time.Now().UTC()
	records := []retention.BackupRecord{
		{FilePath: "a", Timestamp: now, Status: "success"},
	}
	if got := retention.CalculateCandidates(records, retention.Policy{}, now); got != nil {
		t.Fatalf("want nil, got %#v", got)
	}
}

func TestCalculateCandidates_NoSuccessReturnsNil(t *testing.T) {
	now := time.Now().UTC()
	records := []retention.BackupRecord{
		{FilePath: "a", Timestamp: now, Status: "failed"},
	}
	if got := retention.CalculateCandidates(records, retention.Policy{KeepLast: 1}, now); got != nil {
		t.Fatalf("want nil for no-success records, got %#v", got)
	}
}

func TestProtectActiveBaseline_RemovesBaselineFromCandidates(t *testing.T) {
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	records := []retention.BackupRecord{
		{FilePath: "incr-2", Timestamp: now, Status: "success", BackupType: "incremental", ChainID: "ch1", ChainIndex: 2},
		{FilePath: "incr-1", Timestamp: now.Add(-1 * time.Hour), Status: "success", BackupType: "incremental", ChainID: "ch1", ChainIndex: 1},
		{FilePath: "base-0", Timestamp: now.Add(-2 * time.Hour), Status: "success", BackupType: "full", ChainID: "ch1", ChainIndex: 0},
	}
	candidates := []retention.BackupCandidate{
		{FilePath: "base-0", BackupType: "full", ChainID: "ch1", ChainIndex: 0},
	}
	got := retention.ProtectActiveBaseline(candidates, records)
	if len(got) != 0 {
		t.Fatalf("expected base-0 protected (empty candidates), got %#v", got)
	}
}

func TestProtectActiveBaseline_NoChainNoChange(t *testing.T) {
	candidates := []retention.BackupCandidate{{FilePath: "a"}}
	records := []retention.BackupRecord{{FilePath: "a"}}
	got := retention.ProtectActiveBaseline(candidates, records)
	if len(got) != 1 {
		t.Fatalf("want unchanged, got %#v", got)
	}
}
