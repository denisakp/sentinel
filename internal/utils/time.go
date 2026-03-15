package utils

import (
	"fmt"
	"sync"
	"time"
)

var (
	nowFnMu sync.RWMutex
	nowFn   = time.Now
)

// Now returns the current time using the package clock seam.
func Now() time.Time {
	nowFnMu.RLock()
	defer nowFnMu.RUnlock()
	return nowFn()
}

// NowUTC returns the current UTC time using the package clock seam.
func NowUTC() time.Time {
	return Now().UTC()
}

// SetNowForTest overrides the package clock and returns a restore function.
func SetNowForTest(fn func() time.Time) func() {
	nowFnMu.Lock()
	prev := nowFn
	nowFn = fn
	nowFnMu.Unlock()

	return func() {
		nowFnMu.Lock()
		nowFn = prev
		nowFnMu.Unlock()
	}
}

// FmtDuration formats a duration as a human-readable string
// Examples: "1s", "12s", "1m 30s", "2h 15m"
func FmtDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s > 0 {
			return fmt.Sprintf("%dm %ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dh", h)
}

// FmtTimestamp formats a time.Time as a human-readable string
// Examples: "2026-02-12 14:30:45", "2026-02-12T14:30:45Z"
func FmtTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// FmtTimestampISO formats a time.Time in ISO 8601 format
func FmtTimestampISO(t time.Time) string {
	return t.Format(time.RFC3339)
}

// ParseDuration parses a duration string like "1h", "30m", "45s"
// Returns time.Duration with error if parsing fails
func ParseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// ParseCronExpression validates a 5-field cron expression format
// Valid format: "minute hour day month weekday"
// Examples: "0 2 * * *" (2 AM daily), "0 0 1 * *" (monthly at midnight)
// This is a simple format check; detailed parsing is handled by robfig/cron/v3
func ParseCronExpression(expr string) error {
	// This will be called by robfig/cron/v3 parser
	// Here we just do basic validation: should have 5 fields when split by space
	fields := 0
	inField := false
	for _, ch := range expr {
		if ch == ' ' || ch == '\t' {
			if inField {
				fields++
				inField = false
			}
		} else if !inField {
			inField = true
		}
	}
	if inField {
		fields++
	}

	if fields != 5 {
		return fmt.Errorf("invalid cron expression '%s': expected 5 fields (minute hour day month weekday), got %d", expr, fields)
	}
	return nil
}

// GetBackupTimestamp returns a timestamp string suitable for backup filenames
// Format: SENTINEL_2006-01-02T15-04-05 (sortable, filesystem-safe)
func GetBackupTimestamp() string {
	return Now().Format("SENTINEL_2006-01-02T15-04-05")
}
