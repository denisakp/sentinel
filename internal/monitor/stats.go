package monitor

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

type MetricDefinition struct {
	Name string
	Help string
	Type string
}

var IncrementalMetricDefinitions = []MetricDefinition{
	{Name: "sentinel_incremental_backup_chain_depth", Help: "Latest observed incremental backup chain depth by job.", Type: "gauge"},
	{Name: "sentinel_incremental_backup_delta_size_bytes", Help: "Latest observed incremental backup delta size in bytes by job.", Type: "gauge"},
	{Name: "sentinel_incremental_restore_chain_depth", Help: "Latest observed incremental restore chain depth by job.", Type: "gauge"},
	{Name: "sentinel_incremental_restore_assembly_duration_ms", Help: "Latest observed incremental restore assembly duration in milliseconds by job.", Type: "gauge"},
	{Name: "sentinel_incremental_restore_fallback_total", Help: "Total incremental restore fallback decisions by decision and reason.", Type: "counter"},
}

type IncrementalMetricsSnapshot struct {
	BackupChainDepth          map[string]int   `json:"backup_chain_depth"`
	BackupDeltaSizeBytes      map[string]int64 `json:"backup_delta_size_bytes"`
	RestoreChainDepth         map[string]int   `json:"restore_chain_depth"`
	RestoreAssemblyDurationMs map[string]int64 `json:"restore_assembly_duration_ms"`
	RestoreFallbackTotal      map[string]int64 `json:"restore_fallback_total"`
}

var incrementalMetricsState = struct {
	sync.Mutex
	snapshot IncrementalMetricsSnapshot
}{
	snapshot: IncrementalMetricsSnapshot{
		BackupChainDepth:          make(map[string]int),
		BackupDeltaSizeBytes:      make(map[string]int64),
		RestoreChainDepth:         make(map[string]int),
		RestoreAssemblyDurationMs: make(map[string]int64),
		RestoreFallbackTotal:      make(map[string]int64),
	},
}

func ResetIncrementalMetrics() {
	incrementalMetricsState.Lock()
	defer incrementalMetricsState.Unlock()
	incrementalMetricsState.snapshot = IncrementalMetricsSnapshot{
		BackupChainDepth:          make(map[string]int),
		BackupDeltaSizeBytes:      make(map[string]int64),
		RestoreChainDepth:         make(map[string]int),
		RestoreAssemblyDurationMs: make(map[string]int64),
		RestoreFallbackTotal:      make(map[string]int64),
	}
}

func SnapshotIncrementalMetrics() IncrementalMetricsSnapshot {
	incrementalMetricsState.Lock()
	defer incrementalMetricsState.Unlock()
	return IncrementalMetricsSnapshot{
		BackupChainDepth:          copyIntMap(incrementalMetricsState.snapshot.BackupChainDepth),
		BackupDeltaSizeBytes:      copyInt64Map(incrementalMetricsState.snapshot.BackupDeltaSizeBytes),
		RestoreChainDepth:         copyIntMap(incrementalMetricsState.snapshot.RestoreChainDepth),
		RestoreAssemblyDurationMs: copyInt64Map(incrementalMetricsState.snapshot.RestoreAssemblyDurationMs),
		RestoreFallbackTotal:      copyInt64Map(incrementalMetricsState.snapshot.RestoreFallbackTotal),
	}
}

func ObserveIncrementalBackup(job string, exec *ports.Execution) {
	if exec == nil || job == "" || exec.BackupType != "incremental" {
		return
	}
	incrementalMetricsState.Lock()
	defer incrementalMetricsState.Unlock()
	incrementalMetricsState.snapshot.BackupChainDepth[job] = exec.ChainIndex
	incrementalMetricsState.snapshot.BackupDeltaSizeBytes[job] = exec.DeltaSizeBytes
}

func ObserveIncrementalRestore(job string, exec *ports.RestoreExecution) {
	if exec == nil || job == "" {
		return
	}
	incrementalMetricsState.Lock()
	defer incrementalMetricsState.Unlock()
	if exec.RestoreMode == "incremental" {
		incrementalMetricsState.snapshot.RestoreChainDepth[job] = exec.ChainDepth
		incrementalMetricsState.snapshot.RestoreAssemblyDurationMs[job] = exec.AssemblyDurationMs
	}
	if exec.FallbackDecision != "" && exec.FallbackDecision != "none" {
		key := exec.FallbackDecision + "|" + exec.FallbackReason
		incrementalMetricsState.snapshot.RestoreFallbackTotal[key]++
	}
}

func copyIntMap(src map[string]int) map[string]int {
	dst := make(map[string]int, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func copyInt64Map(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

// GetStatistics computes aggregate stats for a backup job.
func (m *Monitor) GetStatistics(ctx context.Context, backupName string, days int) (*Statistics, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("monitor database is not initialized")
	}
	if backupName == "" {
		return nil, fmt.Errorf("backup name is required")
	}

	filter := &ports.Filter{BackupName: backupName}
	if days > 0 {
		filter.StartDate = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	}

	executions, err := m.ListExecutions(ctx, filter, 10000, 0)
	if err != nil {
		return nil, err
	}

	return buildStatistics(executions, backupName, days), nil
}

// GetAggregateStatistics computes aggregate stats across all backup jobs.
func (m *Monitor) GetAggregateStatistics(ctx context.Context, days int) (*Statistics, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("monitor database is not initialized")
	}

	filter := &ports.Filter{}
	if days > 0 {
		filter.StartDate = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	}

	executions, err := m.ListExecutions(ctx, filter, 10000, 0)
	if err != nil {
		return nil, err
	}

	return buildStatistics(executions, "all jobs", days), nil
}

func buildStatistics(executions []ports.Execution, backupName string, days int) *Statistics {
	stats := &Statistics{
		BackupName: backupName,
		JobsPeriod: periodLabel(days),
		Trend:      "stable",
	}

	if len(executions) == 0 {
		return stats
	}

	stats.TotalExecutions = len(executions)
	stats.LastExecution = &executions[0]

	var durations []int64
	for _, exec := range executions {
		switch NormalizeStatus(exec.Status) {
		case ports.StatusCompleted:
			stats.SuccessCount++
		case ports.StatusFailed:
			stats.FailureCount++
		}
		stats.TotalBackupSize += exec.FileSizeBytes
		durations = append(durations, exec.DurationMs)
	}

	stats.SuccessRate = float32(stats.SuccessCount) / float32(stats.TotalExecutions)
	stats.AverageDurationMs = average(durations)
	stats.MedianDurationMs = median(durations)
	stats.MinDurationMs, stats.MaxDurationMs = minMax(durations)

	return stats
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
	slices.Sort(sorted)

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
