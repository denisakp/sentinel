package incremental

import "testing"

func TestDecide_NoBaselineStartsFull(t *testing.T) {
	decision, err := Decide(ChainState{}, false)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Type != "full" {
		t.Fatalf("Type = %q, want full", decision.Type)
	}
	if decision.ChainIndex != 0 {
		t.Fatalf("ChainIndex = %d, want 0", decision.ChainIndex)
	}
}

func TestDecide_IncrementalWhenBaselineExists(t *testing.T) {
	decision, err := Decide(ChainState{
		ChainID:          "chain-1",
		BaselineBackupID: "base-1",
		CurrentIndex:     1,
		MaxDepth:         6,
	}, false)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Type != "incremental" {
		t.Fatalf("Type = %q, want incremental", decision.Type)
	}
	if decision.ChainIndex != 2 {
		t.Fatalf("ChainIndex = %d, want 2", decision.ChainIndex)
	}
}

func TestDecide_MaxDepthForcesFull(t *testing.T) {
	decision, err := Decide(ChainState{
		ChainID:          "chain-1",
		BaselineBackupID: "base-1",
		CurrentIndex:     6,
		MaxDepth:         6,
	}, false)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Type != "full" {
		t.Fatalf("Type = %q, want full", decision.Type)
	}
}

func TestDecide_DefaultMaxDepthRolloverWhenUnset(t *testing.T) {
	decision, err := Decide(ChainState{
		ChainID:          "chain-1",
		BaselineBackupID: "base-1",
		CurrentIndex:     6,
		MaxDepth:         0,
	}, false)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Type != "full" {
		t.Fatalf("Type = %q, want full", decision.Type)
	}
	if decision.Reason != ReasonMaxDepth {
		t.Fatalf("Reason = %q, want %q", decision.Reason, ReasonMaxDepth)
	}
	if decision.ChainIndex != 0 {
		t.Fatalf("ChainIndex = %d, want 0", decision.ChainIndex)
	}
}

func TestDecide_MaxDepthRolloverStartsNewChainID(t *testing.T) {
	originalNow := nowUTCUnix
	nowUTCUnix = func() int64 { return 123456789 }
	t.Cleanup(func() { nowUTCUnix = originalNow })

	decision, err := Decide(ChainState{
		ChainID:          "chain-1",
		BaselineBackupID: "base-1",
		CurrentIndex:     6,
		MaxDepth:         6,
	}, false)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Type != "full" {
		t.Fatalf("Type = %q, want full", decision.Type)
	}
	if decision.ChainID == "chain-1" {
		t.Fatalf("ChainID = %q, expected rollover to a new chain id", decision.ChainID)
	}
	if decision.ChainID != "chain-123456789" {
		t.Fatalf("ChainID = %q, want chain-123456789", decision.ChainID)
	}
}

func TestDecide_ForceFull(t *testing.T) {
	decision, err := Decide(ChainState{
		ChainID:          "chain-1",
		BaselineBackupID: "base-1",
		CurrentIndex:     3,
		MaxDepth:         6,
	}, true)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Type != "full" {
		t.Fatalf("Type = %q, want full", decision.Type)
	}
	if decision.Reason != ReasonForceFull {
		t.Fatalf("Reason = %q, want %q", decision.Reason, ReasonForceFull)
	}
}

func TestValidatePrerequisites(t *testing.T) {
	if err := Ensure(ValidatePostgresIncrementalPrerequisites(17, true)); err != nil {
		t.Fatalf("postgres prerequisites expected success, got %v", err)
	}
	binlogDir := t.TempDir()
	if err := Ensure(ValidateMySQLIncrementalPrerequisites(true, binlogDir)); err != nil {
		t.Fatalf("mysql prerequisites expected success, got %v", err)
	}
	if err := Ensure(ValidateMongoIncrementalPrerequisites(true)); err != nil {
		t.Fatalf("mongo prerequisites expected success, got %v", err)
	}
}
