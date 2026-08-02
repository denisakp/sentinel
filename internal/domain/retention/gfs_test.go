package retention_test

import (
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/domain/retention"
)

// deletedSet returns the FilePaths flagged for deletion.
func deletedSet(cands []retention.BackupCandidate) map[string]string {
	m := make(map[string]string, len(cands))
	for _, c := range cands {
		m[c.FilePath] = c.ReasonDeleted
	}
	return m
}

func assertDeleted(t *testing.T, cands []retention.BackupCandidate, wantDeleted ...string) {
	t.Helper()
	got := deletedSet(cands)
	if len(got) != len(wantDeleted) {
		t.Fatalf("want %d deleted %v, got %d %#v", len(wantDeleted), wantDeleted, len(got), got)
	}
	for _, fp := range wantDeleted {
		if _, ok := got[fp]; !ok {
			t.Fatalf("want %q deleted, got set %#v", fp, got)
		}
	}
}

func date(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.UTC)
}

// 1. Daily tier: keep newest of the 2 most-recent days; older same-day + 3rd day pruned.
func TestGFS_Daily_KeepAndDeleteEdge(t *testing.T) {
	now := date(2026, 8, 2, 12)
	records := []retention.BackupRecord{
		{FilePath: "d0a", Timestamp: date(2026, 8, 2, 6), Status: "success"},
		{FilePath: "d0b", Timestamp: date(2026, 8, 2, 1), Status: "success"},
		{FilePath: "d1", Timestamp: date(2026, 8, 1, 6), Status: "success"},
		{FilePath: "d2", Timestamp: date(2026, 7, 31, 6), Status: "success"},
	}
	got := retention.CalculateCandidates(records, retention.Policy{GFS: &retention.GFSPolicy{KeepDaily: 2}}, now)
	assertDeleted(t, got, "d0b", "d2") // keep d0a (newest of 08-02) + d1 (08-01)
}

// 2. Weekly tier respects ISO Mon–Sun boundary: Sunday and the next Monday are different weeks.
func TestGFS_Weekly_ISOBoundary(t *testing.T) {
	now := date(2023, 1, 3, 0)
	// 2023-01-01 is a Sunday (ISO 2022-W52); 2023-01-02 is a Monday (ISO 2023-W01).
	records := []retention.BackupRecord{
		{FilePath: "mon", Timestamp: date(2023, 1, 2, 6), Status: "success"},
		{FilePath: "sun", Timestamp: date(2023, 1, 1, 6), Status: "success"},
	}
	got := retention.CalculateCandidates(records, retention.Policy{GFS: &retention.GFSPolicy{KeepWeekly: 1}}, now)
	assertDeleted(t, got, "sun") // most-recent week is 2023-W01 (mon); sun is a prior week
}

// 3. Monthly gap: an empty month does not consume a slot; only occupied months anchor.
func TestGFS_Monthly_GapSkipped(t *testing.T) {
	now := date(2026, 8, 20, 0)
	records := []retention.BackupRecord{
		{FilePath: "aug", Timestamp: date(2026, 8, 15, 6), Status: "success"},
		{FilePath: "aug_old", Timestamp: date(2026, 8, 2, 6), Status: "success"},
		{FilePath: "jun", Timestamp: date(2026, 6, 10, 6), Status: "success"}, // July is a gap
		{FilePath: "may", Timestamp: date(2026, 5, 1, 6), Status: "success"},
	}
	// keep_monthly:12 but only 3 months occupied → keep aug/jun/may anchors, prune aug_old.
	got := retention.CalculateCandidates(records, retention.Policy{GFS: &retention.GFSPolicy{KeepMonthly: 12}}, now)
	assertDeleted(t, got, "aug_old")
}

// 4. Yearly tier: keep newest of the 2 most-recent years.
func TestGFS_Yearly(t *testing.T) {
	now := date(2026, 8, 2, 0)
	records := []retention.BackupRecord{
		{FilePath: "y2026", Timestamp: date(2026, 3, 1, 6), Status: "success"},
		{FilePath: "y2025", Timestamp: date(2025, 3, 1, 6), Status: "success"},
		{FilePath: "y2024", Timestamp: date(2024, 3, 1, 6), Status: "success"},
	}
	got := retention.CalculateCandidates(records, retention.Policy{GFS: &retention.GFSPolicy{KeepYearly: 2}}, now)
	assertDeleted(t, got, "y2024")
}

// 5. Overlapping anchors: the newest backup anchors all four tiers and is kept once.
func TestGFS_OverlappingAnchorsKeptOnce(t *testing.T) {
	now := date(2026, 8, 2, 12)
	records := []retention.BackupRecord{
		{FilePath: "newest", Timestamp: date(2026, 8, 2, 6), Status: "success"},
		{FilePath: "older", Timestamp: date(2025, 1, 1, 6), Status: "success"},
	}
	pol := retention.Policy{GFS: &retention.GFSPolicy{KeepDaily: 1, KeepWeekly: 1, KeepMonthly: 1, KeepYearly: 1}}
	got := retention.CalculateCandidates(records, pol, now)
	assertDeleted(t, got, "older") // newest anchors every tier, kept; never appears as candidate
}

// 6. GFS-only deletes every non-anchor, with the aggregate GFS reason.
func TestGFS_OnlyDeletesNonAnchors_Reason(t *testing.T) {
	now := date(2026, 8, 3, 0)
	records := []retention.BackupRecord{
		{FilePath: "d0", Timestamp: date(2026, 8, 2, 6), Status: "success"},
		{FilePath: "d1", Timestamp: date(2026, 8, 1, 6), Status: "success"},
		{FilePath: "d2", Timestamp: date(2026, 7, 31, 6), Status: "success"},
	}
	got := retention.CalculateCandidates(records, retention.Policy{GFS: &retention.GFSPolicy{KeepDaily: 1}}, now)
	assertDeleted(t, got, "d1", "d2")
	for _, c := range got {
		if c.ReasonDeleted != retention.ReasonNotRetainedByGFS {
			t.Fatalf("want reason %q, got %q for %s", retention.ReasonNotRetainedByGFS, c.ReasonDeleted, c.FilePath)
		}
	}
}

// 7. The worked example from contracts/gfs-retention.md §2.
func TestGFS_WorkedExample(t *testing.T) {
	now := date(2026, 8, 2, 0)
	records := []retention.BackupRecord{
		{FilePath: "d0", Timestamp: date(2026, 8, 2, 6), Status: "success"},
		{FilePath: "d0b", Timestamp: date(2026, 8, 2, 1), Status: "success"},
		{FilePath: "d1", Timestamp: date(2026, 8, 1, 6), Status: "success"},
		{FilePath: "w1", Timestamp: date(2026, 7, 25, 6), Status: "success"},
		{FilePath: "m1", Timestamp: date(2026, 6, 15, 6), Status: "success"},
		{FilePath: "y1", Timestamp: date(2025, 3, 10, 6), Status: "success"},
		{FilePath: "old", Timestamp: date(2023, 1, 1, 6), Status: "success"},
	}
	pol := retention.Policy{GFS: &retention.GFSPolicy{KeepDaily: 2, KeepWeekly: 1, KeepMonthly: 1, KeepYearly: 1}}
	got := retention.CalculateCandidates(records, pol, now)
	assertDeleted(t, got, "d0b", "w1", "m1", "y1", "old") // keep-set {d0, d1}
}

// 8. Empty / no-success history with a GFS policy is a no-op.
func TestGFS_EmptyHistoryNoop(t *testing.T) {
	now := date(2026, 8, 2, 0)
	if got := retention.CalculateCandidates(nil, retention.Policy{GFS: &retention.GFSPolicy{KeepDaily: 3}}, now); got != nil {
		t.Fatalf("want nil for empty history, got %#v", got)
	}
	failed := []retention.BackupRecord{{FilePath: "x", Timestamp: now, Status: "failed"}}
	if got := retention.CalculateCandidates(failed, retention.Policy{GFS: &retention.GFSPolicy{KeepDaily: 3}}, now); got != nil {
		t.Fatalf("want nil for no-success history, got %#v", got)
	}
}

// 9. Zero GFS policy contributes nothing (behaviour identical to no policy).
func TestGFS_ZeroPolicyIsNoop(t *testing.T) {
	now := date(2026, 8, 2, 0)
	records := []retention.BackupRecord{
		{FilePath: "a", Timestamp: date(2026, 8, 2, 6), Status: "success"},
		{FilePath: "b", Timestamp: date(2026, 8, 1, 6), Status: "success"},
	}
	if got := retention.CalculateCandidates(records, retention.Policy{GFS: &retention.GFSPolicy{}}, now); got != nil {
		t.Fatalf("want nil for zero GFS policy, got %#v", got)
	}
}

// 10. Union: keep_last keeps a record GFS would drop (flat protection wins).
func TestGFS_Union_FlatKeepsWhatGFSDrops(t *testing.T) {
	now := date(2026, 8, 3, 0)
	records := []retention.BackupRecord{
		{FilePath: "r0", Timestamp: date(2026, 8, 2, 6), Status: "success"},
		{FilePath: "r1", Timestamp: date(2026, 8, 1, 6), Status: "success"},
		{FilePath: "r2", Timestamp: date(2026, 7, 31, 6), Status: "success"},
	}
	// keep_last:2 keeps r0,r1; keep_daily:1 keeps only r0. Union keeps r0,r1 → only r2 deleted.
	pol := retention.Policy{KeepLast: 2, GFS: &retention.GFSPolicy{KeepDaily: 1}}
	got := retention.CalculateCandidates(records, pol, now)
	assertDeleted(t, got, "r2")
	if got[0].FilePath == "r2" && !strings.Contains(got[0].ReasonDeleted, retention.ReasonNotRetainedByGFS) {
		t.Fatalf("want combined reason including gfs, got %q", got[0].ReasonDeleted)
	}
	if !strings.Contains(got[0].ReasonDeleted, "exceeded keep_last") {
		t.Fatalf("want combined reason including keep_last, got %q", got[0].ReasonDeleted)
	}
}

// 11. Union: GFS keeps a record keep_last would drop (GFS protection wins).
func TestGFS_Union_GFSKeepsWhatFlatDrops(t *testing.T) {
	now := date(2026, 8, 20, 0)
	records := []retention.BackupRecord{
		{FilePath: "aug", Timestamp: date(2026, 8, 2, 6), Status: "success"},
		{FilePath: "jul", Timestamp: date(2026, 7, 15, 6), Status: "success"},
		{FilePath: "jun", Timestamp: date(2026, 6, 10, 6), Status: "success"},
	}
	// keep_last:1 keeps aug; keep_monthly:2 keeps aug,jul. Union → only jun deleted.
	pol := retention.Policy{KeepLast: 1, GFS: &retention.GFSPolicy{KeepMonthly: 2}}
	got := retention.CalculateCandidates(records, pol, now)
	assertDeleted(t, got, "jun")
}

// 12. Backward-compat: a nil GFS policy yields exactly the legacy keep_last result.
func TestGFS_NilPolicyMatchesLegacy(t *testing.T) {
	now := date(2026, 2, 12, 10)
	records := []retention.BackupRecord{
		{FilePath: "a", Timestamp: now.Add(-1 * time.Hour), Status: "success"},
		{FilePath: "b", Timestamp: now.Add(-2 * time.Hour), Status: "success"},
		{FilePath: "c", Timestamp: now.Add(-3 * time.Hour), Status: "success"},
	}
	legacy := retention.CalculateCandidates(records, retention.Policy{KeepLast: 1}, now)
	withNil := retention.CalculateCandidates(records, retention.Policy{KeepLast: 1, GFS: nil}, now)
	if len(legacy) != len(withNil) || len(legacy) != 2 {
		t.Fatalf("nil GFS must match legacy: legacy=%#v withNil=%#v", legacy, withNil)
	}
}

// 13. Baseline protection still applies under GFS: an old chain baseline GFS would
// prune is protected by ProtectActiveBaseline (FR-011).
func TestGFS_ActiveBaselineProtected(t *testing.T) {
	now := date(2026, 8, 2, 12)
	records := []retention.BackupRecord{
		{FilePath: "incr2", Timestamp: date(2026, 8, 2, 6), Status: "success", BackupType: "incremental", ChainID: "ch1", ChainIndex: 2},
		{FilePath: "incr1", Timestamp: date(2026, 8, 1, 6), Status: "success", BackupType: "incremental", ChainID: "ch1", ChainIndex: 1},
		{FilePath: "base0", Timestamp: date(2026, 6, 1, 6), Status: "success", BackupType: "full", ChainID: "ch1", ChainIndex: 0},
	}
	pol := retention.Policy{GFS: &retention.GFSPolicy{KeepDaily: 1}}
	cands := retention.CalculateCandidates(records, pol, now)
	// GFS marks incr1 + base0 (only incr2 anchors the daily bucket).
	assertDeleted(t, cands, "incr1", "base0")
	protected := retention.ProtectActiveBaseline(cands, records)
	got := deletedSet(protected)
	if _, ok := got["base0"]; ok {
		t.Fatalf("base0 (active baseline) must be protected under GFS, got %#v", got)
	}
	if _, ok := got["incr1"]; !ok {
		t.Fatalf("incr1 should remain a candidate, got %#v", got)
	}
}
