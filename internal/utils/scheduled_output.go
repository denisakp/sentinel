package utils

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const scheduledOutTimestampLayout = "2006-01-02T15-04-05"

var (
	scheduledOutCountersMu sync.Mutex
	scheduledOutCounters   = map[string]int{}
)

// BuildScheduledOutName builds a scheduler-only artifact name using
// <prefix>_<UTC-second>[-N]<canonicalExt> semantics.
func BuildScheduledOutName(rawOutput, canonicalExt, collisionScope string, ts time.Time) string {
	if ts.IsZero() {
		ts = NowUTC()
	}
	ts = ts.UTC()

	prefix := NormalizeScheduledOutputPrefix(rawOutput, canonicalExt)
	if prefix == "" {
		prefix = DefaultBackupOutName()
	}

	stamp := ts.Format(scheduledOutTimestampLayout)
	base := fmt.Sprintf("%s_%s", prefix, stamp)

	suffix := nextScheduledCollisionSuffix(collisionScope, base)
	if suffix > 0 {
		base = fmt.Sprintf("%s-%d", base, suffix)
	}

	if canonicalExt == "" {
		return base
	}
	return base + canonicalExt
}

// NormalizeScheduledOutputPrefix strips one trailing canonical extension
// from configured output to avoid double-extension names after timestamping.
func NormalizeScheduledOutputPrefix(rawOutput, canonicalExt string) string {
	if rawOutput == "" || canonicalExt == "" {
		return rawOutput
	}
	if strings.HasSuffix(rawOutput, canonicalExt) {
		return strings.TrimSuffix(rawOutput, canonicalExt)
	}
	return rawOutput
}

func nextScheduledCollisionSuffix(collisionScope, base string) int {
	key := collisionScope + "|" + base

	scheduledOutCountersMu.Lock()
	defer scheduledOutCountersMu.Unlock()

	suffix := scheduledOutCounters[key]
	scheduledOutCounters[key] = suffix + 1
	return suffix
}

func resetScheduledOutCountersForTest() {
	scheduledOutCountersMu.Lock()
	defer scheduledOutCountersMu.Unlock()
	scheduledOutCounters = map[string]int{}
}

// ResetScheduledOutCountersForTest clears scheduled output collision state for tests.
func ResetScheduledOutCountersForTest() {
	resetScheduledOutCountersForTest()
}
