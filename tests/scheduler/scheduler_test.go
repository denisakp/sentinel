package scheduler_test

import (
	"testing"

	"github.com/denisakp/sentinel/internal/scheduler"
)

func TestAddJobInvalidCron(t *testing.T) {
	s := scheduler.NewScheduler(1)
	if err := s.AddJob("job", "invalid", func() error { return nil }); err == nil {
		t.Fatalf("expected error for invalid cron expression")
	}
}
