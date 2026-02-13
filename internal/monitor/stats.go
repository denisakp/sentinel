package monitor

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// GetStatistics computes aggregate stats for a backup job.
func (m *Monitor) GetStatistics(ctx context.Context, backupName string, days int) (*Statistics, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("monitor database is not initialized")
	}
	if backupName == "" {
		return nil, fmt.Errorf("backup name is required")
	}

	filter := &Filter{BackupName: backupName}
	if days > 0 {
		filter.StartDate = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	}

	executions, err := m.ListExecutions(ctx, filter, 10000, 0)
	if err != nil {
		return nil, err
	}

	stats := &Statistics{
		BackupName: backupName,
		JobsPeriod: periodLabel(days),
		Trend:      "stable",
	}

	if len(executions) == 0 {
		return stats, nil
	}

	stats.TotalExecutions = len(executions)
	stats.LastExecution = &executions[0]

	var durations []int64
	for _, exec := range executions {
		if exec.Status == "success" {
			stats.SuccessCount++
		} else if exec.Status == "failure" {
			stats.FailureCount++
		}
		stats.TotalBackupSize += exec.FileSizeBytes
		durations = append(durations, exec.DurationMs)
	}

	if stats.TotalExecutions > 0 {
		stats.SuccessRate = float32(stats.SuccessCount) / float32(stats.TotalExecutions)
	}

	stats.AverageDurationMs = average(durations)
	stats.MedianDurationMs = median(durations)
	stats.MinDurationMs, stats.MaxDurationMs = minMax(durations)

	return stats, nil
}

func periodLabel(days int) string {
	if days <= 0 {
		return "all time"
	}
	return fmt.Sprintf("last %d days", days)
}

func average(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	var sum int64
	for _, v := range values {
		sum += v
	}
	return sum / int64(len(values))
}

func median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}

	return (sorted[mid-1] + sorted[mid]) / 2
}

func minMax(values []int64) (int64, int64) {
	if len(values) == 0 {
		return 0, 0
	}
	min := values[0]
	max := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}
