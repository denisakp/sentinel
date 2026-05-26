package lock

import (
	"encoding/json"
	"testing"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

// TestJobLock_JSONShape_v1Stable pins the on-disk ports.JobLock JSON shape so
// any accidental struct edit trips CI (FR-010 / ADR 0007 contract).
func TestJobLock_JSONShape_v1Stable(t *testing.T) {
	jl := ports.JobLock{
		PID:       42,
		JobName:   "demo",
		StartTime: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		Hostname:  "h",
	}
	got, err := json.Marshal(jl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"pid":42,"job_name":"demo","start_time":"2025-01-02T03:04:05Z","hostname":"h"}`
	if string(got) != want {
		t.Errorf("JSON shape drift\n got:  %s\n want: %s", got, want)
	}
}
