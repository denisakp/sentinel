package integration

import (
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/restore"
)

func TestPostgresPITRPlanningFromManifest(t *testing.T) {
	fixture := NewPITRFixture()
	dir := t.TempDir()
	manifestPath := WritePITRManifest(t, dir, fixture)

	request := &config.AdvancedRestoreRequest{
		RestoreMode:      "pitr",
		PITRTimestampUTC: &fixture.TargetTimeUTC,
		PITRInputValue:   fixture.TargetTimeUTC.Format(time.RFC3339),
	}

	plan, err := restore.PlanAdvancedRestoreFromManifestPath(
		config.RestoreJob{Type: "postgres", RestoreMode: "pitr"},
		request,
		manifestPath,
	)
	if err != nil {
		t.Fatalf("PlanAdvancedRestoreFromManifestPath() error = %v", err)
	}
	if plan.Status != restore.PlanStatusReady {
		t.Fatalf("Status = %q, want %q", plan.Status, restore.PlanStatusReady)
	}
	if plan.Mode != restore.AdvancedRestoreModePITR {
		t.Fatalf("Mode = %q, want %q", plan.Mode, restore.AdvancedRestoreModePITR)
	}
}

func TestPostgresPITRPlanningRejectsOutsideWindow(t *testing.T) {
	fixture := NewPITRFixture()
	dir := t.TempDir()
	manifestPath := WritePITRManifest(t, dir, fixture)

	outside := fixture.WindowEndUTC.Add(2 * time.Hour)
	request := &config.AdvancedRestoreRequest{
		RestoreMode:      "pitr",
		PITRTimestampUTC: &outside,
		PITRInputValue:   outside.Format(time.RFC3339),
	}

	plan, err := restore.PlanAdvancedRestoreFromManifestPath(
		config.RestoreJob{Type: "postgres", RestoreMode: "pitr"},
		request,
		manifestPath,
	)
	if err != nil {
		t.Fatalf("PlanAdvancedRestoreFromManifestPath() error = %v", err)
	}
	if plan.Status != restore.PlanStatusRejected {
		t.Fatalf("Status = %q, want %q", plan.Status, restore.PlanStatusRejected)
	}
	if plan.ReasonCode != restore.ReasonCodePITROutsideRecoverableWindow {
		t.Fatalf("ReasonCode = %q, want %q", plan.ReasonCode, restore.ReasonCodePITROutsideRecoverableWindow)
	}
}
