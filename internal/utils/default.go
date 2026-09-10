package utils

import (
	"fmt"
	"path/filepath"
	"strings"
)

// DefaultValue returns the default value if the value is empty
func DefaultValue(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// DefaultBackupOutName returns the default backup output name
func DefaultBackupOutName() string {
	return fmt.Sprintf("SENTINEL_%s", Now().Format("2006-01-02T15-04-05"))
}

// Placeholders recognised inside an explicit `output:` value. See FinalOutName.
const (
	// PlaceholderTimestamp expands to the run's UTC timestamp, the same format
	// the generated default name uses.
	PlaceholderTimestamp = "{timestamp}"
	// PlaceholderDate expands to the run's UTC date.
	PlaceholderDate = "{date}"
)

// HasOutNamePlaceholder reports whether an `output:` value asks to be expanded
// per run.
func HasOutNamePlaceholder(outName string) bool {
	return strings.Contains(outName, PlaceholderTimestamp) ||
		strings.Contains(outName, PlaceholderDate)
}

// FinalOutName resolves a configured `output:` value into the artifact filename
// for this run.
//
// An explicit value is returned VERBATIM unless it contains a placeholder. That
// is deliberate and backward-compatible: `output: shop.sql` has always written
// shop.sql, and silently interpolating a timestamp into it would change the
// filename every existing installation produces, breaking scripts and alerts
// that expect the old name.
//
// The cost of the literal form is that every run truncates the previous artifact,
// so retention has nothing to prune and an incremental chain collapses onto one
// file (#193). `output: shop-{timestamp}.sql` opts into a distinct artifact per
// run. Omitting `output:` also gives distinct names, and since the fix for #151 a
// manifest is written in that case too, so there is no longer any reason to set a
// literal name for integrity's sake.
func FinalOutName(outName string) string {
	if HasOutNamePlaceholder(outName) {
		now := Now().UTC()
		expanded := strings.ReplaceAll(outName, PlaceholderTimestamp, now.Format("2006-01-02T15-04-05"))
		expanded = strings.ReplaceAll(expanded, PlaceholderDate, now.Format("2006-01-02"))
		outName = expanded
	}

	ext := filepath.Ext(outName)

	if ext == "" {
		return DefaultValue(outName, DefaultBackupOutName()) + ".sql"
	}

	return outName
}

func FullPath(path, fileName string) string {
	return filepath.Join(path, fileName)
}
