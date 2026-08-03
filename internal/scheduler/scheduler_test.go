package scheduler

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

func TestJobSchedule_ReturnsParsedSchedule(t *testing.T) {
	s := NewScheduler(1)
	if err := s.AddJob("job-a", "*/5 * * * *", func() error { return nil }); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	got, err := s.JobSchedule("job-a")
	if err != nil {
		t.Fatalf("JobSchedule: %v", err)
	}
	if got == nil {
		t.Fatal("JobSchedule returned nil schedule")
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	want, err := parser.Parse("*/5 * * * *")
	if err != nil {
		t.Fatalf("parser.Parse: %v", err)
	}

	now := time.Date(2026, 6, 9, 10, 1, 0, 0, time.UTC)
	if gotNext, wantNext := got.Next(now), want.Next(now); !gotNext.Equal(wantNext) {
		t.Errorf("Next(%v) = %v, want %v", now, gotNext, wantNext)
	}
}

func TestJobSchedule_UnknownJob(t *testing.T) {
	s := NewScheduler(1)
	if _, err := s.JobSchedule("missing"); err == nil {
		t.Fatal("expected error for unknown job, got nil")
	}
}
