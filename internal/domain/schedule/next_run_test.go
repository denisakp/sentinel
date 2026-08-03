package schedule_test

import (
	"errors"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/domain/schedule"
)

type stubSchedule struct {
	when time.Time
}

func (s stubSchedule) Next(after time.Time) time.Time {
	if s.when.IsZero() {
		return after.Add(time.Hour)
	}
	return s.when
}

func TestNextRun_NilScheduleReturnsZero(t *testing.T) {
	if got := schedule.NextRun(nil, time.Now()); !got.IsZero() {
		t.Fatalf("want zero time, got %v", got)
	}
}

func TestNextRun_ForwardsToScheduleNext(t *testing.T) {
	want := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got := schedule.NextRun(stubSchedule{when: want}, time.Now())
	if !got.Equal(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestValidate_AcceptsValidJob(t *testing.T) {
	cases := []schedule.JobKind{schedule.KindBackup, schedule.KindRestore, schedule.KindRetention, schedule.KindIntegrityCheck}
	for _, k := range cases {
		if err := schedule.Validate(schedule.ScheduledJob{Name: "j", CronExpr: "* * * * *", Kind: k, Enabled: true}); err != nil {
			t.Errorf("Validate(kind=%q) unexpected error: %v", k, err)
		}
	}
}

func TestValidate_RejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		job  schedule.ScheduledJob
	}{
		{"empty name", schedule.ScheduledJob{CronExpr: "* * * * *", Kind: schedule.KindBackup}},
		{"empty cron", schedule.ScheduledJob{Name: "j", Kind: schedule.KindBackup}},
		{"empty kind", schedule.ScheduledJob{Name: "j", CronExpr: "* * * * *"}},
		{"unknown kind", schedule.ScheduledJob{Name: "j", CronExpr: "* * * * *", Kind: "noop"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := schedule.Validate(tc.job)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, schedule.ErrInvalidJob) {
				t.Fatalf("want errors.Is(err, ErrInvalidJob), got %v", err)
			}
		})
	}
}
