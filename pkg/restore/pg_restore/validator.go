package pg_restore

import (
	"fmt"
)

// validateRestoreFormat validates the restore format
func ValidateRestoreFormat(format string) error {
	validFormats := map[string]bool{
		"c": true, // custom
		"d": true, // directory
		"t": true, // tar
		"p": true, // plain
	}

	if format == "" {
		return nil // optional
	}

	if !validFormats[format] {
		return fmt.Errorf("invalid restore format '%s'; must be one of: c (custom), d (directory), t (tar), p (plain)", format)
	}

	return nil
}

// ValidateOnConflict validates conflict resolution strategy
func ValidateOnConflict(strategy string) error {
	validStrategies := map[string]bool{
		"ignore":  true,
		"replace": true,
		"error":   true,
		"":        true,
	}

	if !validStrategies[strategy] {
		return fmt.Errorf("invalid conflict strategy '%s'; must be one of: ignore, replace, error", strategy)
	}

	return nil
}

// ValidateRequiredArgs validates required RestoreArgs fields
func ValidateRequiredArgs(ra *RestoreArgs) error {
	if ra == nil {
		return fmt.Errorf("restore arguments cannot be nil")
	}

	if ra.Database == "" {
		return fmt.Errorf("database name is required")
	}

	if ra.Username == "" {
		return fmt.Errorf("username is required")
	}

	if ra.BackupPath == "" && (ra.Storage == nil || ra.Storage.OutName == "") {
		return fmt.Errorf("backup path is required (either BackupPath or Storage.OutName)")
	}

	return nil
}
