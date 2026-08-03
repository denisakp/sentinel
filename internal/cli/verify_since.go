package cli

import (
	"time"

	"github.com/denisakp/sentinel/internal/config"
)

// parseSince parses a recency window for `backup verify --all --since`.
//
// It delegates to config.ParseSinceWindow — the single grammar shared with the
// scheduled integrity check's since validation — so both accept exactly the
// same input (a trailing `d`/`w` integer suffix, or any time.ParseDuration
// value; always non-negative; empty is an error).
func parseSince(s string) (time.Duration, error) {
	return config.ParseSinceWindow(s)
}
