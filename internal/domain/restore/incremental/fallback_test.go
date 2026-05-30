package incremental

import "testing"

func TestEvaluateFallback(t *testing.T) {
	required := EvaluateFallback(false, "full-001", "incremental_capability_unavailable")
	if required.Proceed {
		t.Fatal("Proceed = true, want false")
	}
	if required.PlanStatus != "confirmation_required" {
		t.Fatalf("PlanStatus = %q, want confirmation_required", required.PlanStatus)
	}

	approved := EvaluateFallback(true, "full-001", "incremental_capability_unavailable")
	if !approved.Proceed {
		t.Fatal("Proceed = false, want true")
	}
	if approved.PlanStatus != "ready" {
		t.Fatalf("PlanStatus = %q, want ready", approved.PlanStatus)
	}
}
