package benchmarks

import (
	"fmt"
	"testing"

	"github.com/denisakp/sentinel/internal/scheduler"
)

const schedulerBenchJobs = 100

func BenchmarkSchedulerAddJob(b *testing.B) {
	b.ReportAllocs()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := scheduler.NewScheduler(5)
		for j := 0; j < schedulerBenchJobs; j++ {
			name := fmt.Sprintf("job-%03d", j)
			if err := s.AddJob(name, "0 2 * * *", func() error { return nil }); err != nil {
				b.Fatalf("add job: %v", err)
			}
		}
	}
}
