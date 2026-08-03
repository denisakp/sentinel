package incremental

import "testing"

// TestValidateMongoIncrementalPrerequisites_StandaloneRejected verifies that
// standalone MongoDB (non-replica-set) is rejected for incremental backup.
func TestValidateMongoIncrementalPrerequisites_StandaloneRejected(t *testing.T) {
	result := ValidateMongoIncrementalPrerequisites(false)
	if result.Passed {
		t.Fatal("expected failure for standalone MongoDB, got pass")
	}
	if result.ErrorCode != "oplog_unavailable_standalone" {
		t.Fatalf("ErrorCode = %q, want oplog_unavailable_standalone", result.ErrorCode)
	}
}

// TestValidateMongoIncrementalPrerequisites_ReplicaSetPasses verifies that
// replica-set MongoDB passes incremental prerequisites.
func TestValidateMongoIncrementalPrerequisites_ReplicaSetPasses(t *testing.T) {
	result := ValidateMongoIncrementalPrerequisites(true)
	if !result.Passed {
		t.Fatalf("expected pass for replica-set MongoDB, got error: %s — %s", result.ErrorCode, result.Detail)
	}
}

// TestValidateMongoOplogWindowPrerequisite_BelowThreshold verifies that an oplog
// window below the configured warn threshold fails validation.
func TestValidateMongoOplogWindowPrerequisite_BelowThreshold(t *testing.T) {
	result := ValidateMongoOplogWindowPrerequisite(2, 24)
	if result.Passed {
		t.Fatal("expected failure when oplog window 2h is below warn threshold 24h")
	}
	if result.ErrorCode != "oplog_window_below_threshold" {
		t.Fatalf("ErrorCode = %q, want oplog_window_below_threshold", result.ErrorCode)
	}
}

// TestValidateMongoOplogWindowPrerequisite_AtThreshold verifies that an oplog
// window exactly at the configured threshold passes.
func TestValidateMongoOplogWindowPrerequisite_AtThreshold(t *testing.T) {
	result := ValidateMongoOplogWindowPrerequisite(24, 24)
	if !result.Passed {
		t.Fatalf("expected pass when oplog window equals threshold, got error: %s", result.ErrorCode)
	}
}

// TestValidateMongoOplogWindowPrerequisite_AboveThreshold verifies that an oplog
// window exceeding the warn threshold passes.
func TestValidateMongoOplogWindowPrerequisite_AboveThreshold(t *testing.T) {
	result := ValidateMongoOplogWindowPrerequisite(48, 24)
	if !result.Passed {
		t.Fatalf("expected pass when oplog window exceeds threshold, got error: %s", result.ErrorCode)
	}
}

// TestValidateMongoOplogWindowPrerequisite_ZeroWarnHoursDisabled verifies that
// a zero warn_hours disables the threshold check.
func TestValidateMongoOplogWindowPrerequisite_ZeroWarnHoursDisabled(t *testing.T) {
	result := ValidateMongoOplogWindowPrerequisite(1, 0)
	if !result.Passed {
		t.Fatalf("expected pass when warn_hours=0 (disabled), got error: %s", result.ErrorCode)
	}
}
