package mysql

import (
	"fmt"
)

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
