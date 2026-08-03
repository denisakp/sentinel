package incremental

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PrerequisiteResult captures a fail-fast validation outcome before backup execution.
type PrerequisiteResult struct {
	Passed    bool
	ErrorCode string
	Detail    string
}

func pass() PrerequisiteResult {
	return PrerequisiteResult{Passed: true}
}

func fail(code, detail string) PrerequisiteResult {
	return PrerequisiteResult{Passed: false, ErrorCode: code, Detail: detail}
}

// ValidatePostgresIncrementalPrerequisites validates PostgreSQL incremental requirements.
func ValidatePostgresIncrementalPrerequisites(versionMajor int, walSummaryEnabled bool) PrerequisiteResult {
	if versionMajor < 17 {
		return fail("postgres_version_insufficient", "postgres 17+ required for incremental backup")
	}
	if !walSummaryEnabled {
		return fail("wal_summary_disabled", "wal_summary must be enabled for incremental backup")
	}
	return pass()
}

// ValidateMySQLIncrementalPrerequisites validates MySQL/MariaDB incremental requirements.
func ValidateMySQLIncrementalPrerequisites(logBinEnabled bool, binlogPath string) PrerequisiteResult {
	if !logBinEnabled {
		return fail("log_bin_off", "binary logging is required for incremental backup")
	}
	return ValidateMySQLBinlogPathPrerequisite(binlogPath)
}

// ValidateMySQLBinlogPathPrerequisite validates binlog_path local mount requirements.
func ValidateMySQLBinlogPathPrerequisite(binlogPath string) PrerequisiteResult {
	trimmed := strings.TrimSpace(binlogPath)
	if trimmed == "" {
		return fail("binlog_path_missing", "mysql.binlog_path is required for incremental backup")
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return fail("binlog_path_invalid", "mysql.binlog_path must resolve to an absolute local path")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fail("binlog_path_not_found", "mysql.binlog_path must exist and be locally mounted")
		}
		return fail("binlog_path_unreadable", "mysql.binlog_path must be readable by sentinel")
	}
	if !info.IsDir() {
		return fail("binlog_path_not_directory", "mysql.binlog_path must point to a directory")
	}
	return pass()
}

// ValidateMongoIncrementalPrerequisites validates MongoDB incremental requirements.
func ValidateMongoIncrementalPrerequisites(replicaSetEnabled bool) PrerequisiteResult {
	if !replicaSetEnabled {
		return fail("oplog_unavailable_standalone", "replica set is required for oplog-based incremental backup")
	}
	return pass()
}

// ValidateMongoOplogWindowPrerequisite validates that the observed oplog retention
// window (windowHours) meets the configured warn threshold (warnHours).
// When warnHours is 0 the check is disabled and always passes.
func ValidateMongoOplogWindowPrerequisite(windowHours, warnHours int) PrerequisiteResult {
	if warnHours <= 0 {
		return pass()
	}
	if windowHours < warnHours {
		return fail(
			"oplog_window_below_threshold",
			fmt.Sprintf("mongodb oplog window is %dh, below configured warn threshold of %dh", windowHours, warnHours),
		)
	}
	return pass()
}

// Ensure validates a prerequisite result and returns a typed error for pipeline callers.
func Ensure(result PrerequisiteResult) error {
	if result.Passed {
		return nil
	}
	return fmt.Errorf("%s: %s", result.ErrorCode, result.Detail)
}
