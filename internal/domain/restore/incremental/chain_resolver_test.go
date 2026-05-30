package incremental

import (
	"strings"
	"testing"
)

func validChain() []ChainArtifact {
	return []ChainArtifact{
		{
			BackupID:        "full-001",
			ChainIndex:      0,
			TimelineID:      "1",
			ManifestPresent: true,
			HashVerified:    true,
		},
		{
			BackupID:         "incr-002",
			BaselineBackupID: "full-001",
			ChainIndex:       1,
			TimelineID:       "1",
			ManifestPresent:  true,
			HashVerified:     true,
		},
		{
			BackupID:         "incr-003",
			BaselineBackupID: "full-001",
			ChainIndex:       2,
			TimelineID:       "1",
			ManifestPresent:  true,
			HashVerified:     true,
		},
	}
}

func TestResolveOrderedChain_ValidChain(t *testing.T) {
	resolved, err := ResolveOrderedChain(validChain(), "")
	if err != nil {
		t.Fatalf("ResolveOrderedChain() error = %v", err)
	}
	if resolved.Depth != 3 {
		t.Fatalf("Depth = %d, want 3", resolved.Depth)
	}
	if resolved.TargetBackupID != "incr-003" {
		t.Fatalf("TargetBackupID = %q, want incr-003", resolved.TargetBackupID)
	}
}

func TestResolveOrderedChain_Rule1BaselineMissing(t *testing.T) {
	artifacts := validChain()
	artifacts[0].ChainIndex = 1
	_, err := ResolveOrderedChain(artifacts, "")
	if err == nil || !strings.Contains(err.Error(), "rule_1") {
		t.Fatalf("expected rule_1 error, got %v", err)
	}
}

func TestResolveOrderedChain_Rule2IntermediateMissing(t *testing.T) {
	artifacts := validChain()
	artifacts[1].ManifestPresent = false
	_, err := ResolveOrderedChain(artifacts, "")
	if err == nil || !strings.Contains(err.Error(), "rule_2") {
		t.Fatalf("expected rule_2 error, got %v", err)
	}
}

func TestResolveOrderedChain_Rule3BaselineMismatch(t *testing.T) {
	artifacts := validChain()
	artifacts[2].BaselineBackupID = "full-xyz"
	_, err := ResolveOrderedChain(artifacts, "")
	if err == nil || !strings.Contains(err.Error(), "rule_3") {
		t.Fatalf("expected rule_3 error, got %v", err)
	}
}

func TestResolveOrderedChain_Rule4NonContiguousIndex(t *testing.T) {
	artifacts := validChain()
	artifacts[2].ChainIndex = 5
	_, err := ResolveOrderedChain(artifacts, "")
	if err == nil || !strings.Contains(err.Error(), "rule_4") {
		t.Fatalf("expected rule_4 error, got %v", err)
	}
}

func TestResolveOrderedChain_Rule5TimelineMismatch(t *testing.T) {
	artifacts := validChain()
	artifacts[2].TimelineID = "2"
	_, err := ResolveOrderedChain(artifacts, "")
	if err == nil || !strings.Contains(err.Error(), "timeline_mismatch") {
		t.Fatalf("expected timeline mismatch error, got %v", err)
	}
}

func TestResolveOrderedChain_Rule6InvalidTarget(t *testing.T) {
	_, err := ResolveOrderedChain(validChain(), "incr-999")
	if err == nil || !strings.Contains(err.Error(), "rule_6") {
		t.Fatalf("expected rule_6 error, got %v", err)
	}
}

func TestResolveOrderedChain_Rule7HashVerificationFailed(t *testing.T) {
	artifacts := validChain()
	artifacts[1].HashVerified = false
	_, err := ResolveOrderedChain(artifacts, "")
	if err == nil || !strings.Contains(err.Error(), "rule_7") {
		t.Fatalf("expected rule_7 error, got %v", err)
	}
}
