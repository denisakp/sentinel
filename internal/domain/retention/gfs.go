package retention

import (
	"fmt"
	"time"
)

// computeGFSKeepSet returns the set of FilePaths retained by the GFS policy.
//
// records MUST be pre-sorted newest-first and pre-filtered to Status=="success".
// For each enabled tier it buckets records by a UTC calendar key (day / ISO week /
// month / year), designates the newest backup in each bucket as that bucket's
// anchor, and retains the anchors of the N most-recent OCCUPIED buckets. Empty
// calendar periods produce no key and are skipped (never backfilled). A backup
// that anchors several tiers is added once (set semantics). Pure.
//
// The current wall-clock is not needed: "most-recent N periods" is defined
// relative to the records themselves (the newest occupied buckets), not to now.
func computeGFSKeepSet(records []BackupRecord, g *GFSPolicy) map[string]struct{} {
	keep := map[string]struct{}{}
	if g.IsZero() {
		return keep
	}

	addTier(keep, records, g.KeepDaily, func(t time.Time) string {
		return t.UTC().Format("2006-01-02")
	})
	addTier(keep, records, g.KeepWeekly, func(t time.Time) string {
		y, w := t.UTC().ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	})
	addTier(keep, records, g.KeepMonthly, func(t time.Time) string {
		return t.UTC().Format("2006-01")
	})
	addTier(keep, records, g.KeepYearly, func(t time.Time) string {
		return t.UTC().Format("2006")
	})

	return keep
}

// addTier adds the anchors of the n most-recent occupied buckets for one tier.
// records are newest-first, so the first record encountered for a bucket key is
// that bucket's newest backup (its anchor), and buckets are encountered in
// most-recent-first order.
func addTier(keep map[string]struct{}, records []BackupRecord, n int, key func(time.Time) string) {
	if n <= 0 {
		return
	}

	anchor := map[string]string{} // bucket key -> anchor FilePath
	order := make([]string, 0)    // bucket keys, newest bucket first
	for i := range records {
		k := key(records[i].Timestamp)
		if _, ok := anchor[k]; ok {
			continue
		}
		anchor[k] = records[i].FilePath
		order = append(order, k)
	}

	limit := n
	if limit > len(order) {
		limit = len(order)
	}
	for i := 0; i < limit; i++ {
		keep[anchor[order[i]]] = struct{}{}
	}
}
