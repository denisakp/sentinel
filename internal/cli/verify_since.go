package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseSince parses a recency window for `backup verify --all --since`.
//
// Operators think in days and weeks, but stdlib time.ParseDuration has no
// day/week unit. parseSince accepts a trailing `d` (×24h) or `w` (×168h)
// suffix on an integer (e.g. "30d", "4w") and otherwise delegates to
// time.ParseDuration (so hour-based durations like "720h" still work). The
// returned duration is always positive; an empty string is an error (the
// caller only calls parseSince when --since was supplied). Spec 051 / PRD 34.
func parseSince(s string) (time.Duration, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("empty --since value")
	}

	last := trimmed[len(trimmed)-1]
	switch last {
	case 'd', 'w':
		n, err := strconv.Atoi(trimmed[:len(trimmed)-1])
		if err != nil {
			return 0, fmt.Errorf("invalid --since value %q: %w", s, err)
		}
		if n < 0 {
			return 0, fmt.Errorf("invalid --since value %q: must be non-negative", s)
		}
		unit := 24 * time.Hour
		if last == 'w' {
			unit = 7 * 24 * time.Hour
		}
		return time.Duration(n) * unit, nil
	default:
		d, err := time.ParseDuration(trimmed)
		if err != nil {
			return 0, fmt.Errorf("invalid --since value %q: %w", s, err)
		}
		if d < 0 {
			return 0, fmt.Errorf("invalid --since value %q: must be non-negative", s)
		}
		return d, nil
	}
}
