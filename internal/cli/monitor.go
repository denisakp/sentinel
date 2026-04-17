package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/utils"
	"github.com/spf13/cobra"
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Monitor backup history and statistics",
	Long:  "Query backup execution history, statistics, and export records for compliance.\n\nExamples:\n  sentinel monitor list --config sentinel.yaml --last 7d\n  sentinel monitor stats --config sentinel.yaml\n  sentinel monitor stats --config sentinel.yaml --job prod-postgres",
}

const noMatchingRecordsMessage = "no matching records"

var monitorListCmd = &cobra.Command{
	Use:   "list",
	Short: "List backup history",
	Long:  "List backup executions with optional filters and formats.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigFromFlags(cmd)
		if err != nil {
			return err
		}

		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			return err
		}
		defer mon.Close()

		filter, err := buildFilterFromFlags(cmd)
		if err != nil {
			return err
		}
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		format, _ := cmd.Flags().GetString("format")

		executions, err := mon.ListExecutions(context.Background(), filter, limit, offset)
		if err != nil {
			return err
		}

		switch strings.ToLower(format) {
		case "json":
			return printJSON(cmd, executions)
		case "csv":
			return printCSV(cmd, mon, filter)
		default:
			return printTable(cmd, executions)
		}
	},
}

var monitorStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate backup statistics",
	Long:  "Show aggregate statistics for a backup job over a time window.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigFromFlags(cmd)
		if err != nil {
			return err
		}

		job, _ := cmd.Flags().GetString("job")
		if job == "" {
			return fmt.Errorf("--job is required")
		}

		last, _ := cmd.Flags().GetString("last")
		days, err := parseLastDays(last)
		if err != nil {
			return err
		}

		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			return err
		}
		defer mon.Close()

		stats, err := loadStatistics(context.Background(), mon, job, days)
		if err != nil {
			return err
		}
		if stats.TotalExecutions == 0 {
			printNoMatchingRecords(cmd)
			return nil
		}

		printStats(cmd, stats)
		return nil
	},
}

var monitorShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show detailed information about a backup",
	Long:  "Show details for a single backup execution by ID.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigFromFlags(cmd)
		if err != nil {
			return err
		}

		id, _ := cmd.Flags().GetString("id")
		if id == "" && len(args) > 0 {
			id = args[0]
		}
		if id == "" {
			return fmt.Errorf("execution id is required")
		}

		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			return err
		}
		defer mon.Close()

		exec, err := mon.GetExecution(context.Background(), id)
		if err != nil {
			return err
		}

		printExecution(cmd, exec)
		return nil
	},
}

var monitorExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export backup history to JSON or CSV",
	Long:  "Export backup history for offline analysis.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigFromFlags(cmd)
		if err != nil {
			return err
		}

		format, _ := cmd.Flags().GetString("format")
		output, _ := cmd.Flags().GetString("output")

		filter, err := buildFilterFromFlags(cmd)
		if err != nil {
			return err
		}

		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			return err
		}
		defer mon.Close()

		data, err := mon.ExportHistory(context.Background(), format, filter)
		if err != nil {
			return err
		}

		if output == "" {
			cmd.Println(string(data))
			return nil
		}

		if err := os.WriteFile(output, data, 0o644); err != nil {
			return fmt.Errorf("failed to write export file: %w", err)
		}

		cmd.Printf("exported %s history to %s\n", strings.ToLower(format), output)
		return nil
	},
}

func init() {
	monitorCmd.AddCommand(monitorListCmd)
	monitorCmd.AddCommand(monitorStatsCmd)
	monitorCmd.AddCommand(monitorShowCmd)
	monitorCmd.AddCommand(monitorExportCmd)

	monitorCmd.PersistentFlags().StringP("config", "c", "", "Path to YAML configuration file")

	monitorListCmd.Flags().StringP("last", "l", "7d", "Time range (e.g., '7d', '30d', '12h')")
	monitorListCmd.Flags().String("job", "", "Filter by backup job name")
	monitorListCmd.Flags().String("status", "", "Filter by status (success/failure)")
	monitorListCmd.Flags().String("type", "", "Filter by database type")
	monitorListCmd.Flags().String("storage", "", "Filter by storage backend")
	monitorListCmd.Flags().String("format", "table", "Output format (table/json/csv)")
	monitorListCmd.Flags().Int("limit", 50, "Max results to return")
	monitorListCmd.Flags().Int("offset", 0, "Pagination offset")

	monitorStatsCmd.Flags().String("job", "", "Backup job name (optional; omit for all jobs)")
	monitorStatsCmd.Flags().StringP("last", "l", "30d", "Time range (e.g., '7d', '30d', '12h')")

	monitorShowCmd.Flags().String("id", "", "Backup execution ID")

	monitorExportCmd.Flags().StringP("format", "f", "json", "Export format (json/csv)")
	monitorExportCmd.Flags().StringP("output", "o", "", "Output file path")
	monitorExportCmd.Flags().StringP("last", "l", "", "Time range (e.g., '7d', '30d', '12h')")
	monitorExportCmd.Flags().String("job", "", "Filter by backup job name")
	monitorExportCmd.Flags().String("status", "", "Filter by status (success/failure)")
	monitorExportCmd.Flags().String("type", "", "Filter by database type")
	monitorExportCmd.Flags().String("storage", "", "Filter by storage backend")
}

func loadConfigFromFlags(cmd *cobra.Command) (*config.Configuration, error) {
	path, _ := cmd.Flags().GetString("config")
	return LoadAndValidateConfig(path)
}

func buildFilterFromFlags(cmd *cobra.Command) (*monitor.Filter, error) {
	job, _ := cmd.Flags().GetString("job")
	status, _ := cmd.Flags().GetString("status")
	storage, _ := cmd.Flags().GetString("storage")
	dbType, _ := cmd.Flags().GetString("type")
	last, _ := cmd.Flags().GetString("last")

	filter := &monitor.Filter{
		BackupName:     job,
		Status:         status,
		DatabaseType:   dbType,
		StorageBackend: storage,
	}

	if last != "" {
		duration, err := parseLastDuration(last)
		if err != nil {
			return nil, err
		}
		filter.StartDate = time.Now().UTC().Add(-duration)
	}

	return filter, nil
}

func parseLastDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}

	if strings.HasSuffix(value, "d") {
		n, err := parseInt(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day duration '%s'", value)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}

	if strings.HasSuffix(value, "min") {
		n, err := parseInt(strings.TrimSuffix(value, "min"))
		if err != nil {
			return 0, fmt.Errorf("invalid minute duration '%s'", value)
		}
		return time.Duration(n) * time.Minute, nil
	}

	if strings.HasSuffix(value, "h") || strings.HasSuffix(value, "m") || strings.HasSuffix(value, "s") {
		return time.ParseDuration(value)
	}

	return 0, fmt.Errorf("invalid duration '%s'", value)
}

func parseLastDays(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	if strings.HasSuffix(value, "d") {
		return parseInt(strings.TrimSuffix(value, "d"))
	}

	duration, err := parseLastDuration(value)
	if err != nil {
		return 0, err
	}
	return int(duration.Hours() / 24), nil
}

func parseInt(value string) (int, error) {
	var out int
	_, err := fmt.Sscanf(value, "%d", &out)
	if err != nil {
		return 0, err
	}
	if out < 0 {
		return 0, fmt.Errorf("value must be positive")
	}
	return out, nil
}

func printJSON(cmd *cobra.Command, executions []monitor.Execution) error {
	data, err := json.MarshalIndent(executions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json output: %w", err)
	}
	cmd.Println(string(data))
	return nil
}

func printCSV(cmd *cobra.Command, mon *monitor.Monitor, filter *monitor.Filter) error {
	data, err := mon.ExportHistory(context.Background(), "csv", filter)
	if err != nil {
		return err
	}
	cmd.Println(string(data))
	return nil
}

func printTable(cmd *cobra.Command, executions []monitor.Execution) error {
	if len(executions) == 0 {
		printNoMatchingRecords(cmd)
		return nil
	}

	headers := []string{"ID", "JOB", "STATUS", "TIMESTAMP", "DURATION", "ERROR"}
	headers = []string{"ID", "JOB", "TYPE", "CHAIN", "STATUS", "TIMESTAMP", "DURATION", "DELTA", "ERROR"}
	rows := make([][]string, 0, len(executions))
	for _, exec := range executions {
		duration := utils.FmtDuration(time.Duration(exec.DurationMs) * time.Millisecond)
		chain := "-"
		if exec.ChainID != "" {
			chain = fmt.Sprintf("%s#%d", exec.ChainID, exec.ChainIndex)
		}
		delta := "-"
		if exec.DeltaSizeBytes > 0 {
			delta = fmt.Sprintf("%d", exec.DeltaSizeBytes)
		}
		backupType := exec.BackupType
		if backupType == "" {
			backupType = "full"
		}
		rows = append(rows, []string{
			exec.ID,
			exec.BackupName,
			backupType,
			chain,
			exec.Status,
			utils.FmtTimestamp(exec.Timestamp),
			duration,
			delta,
			truncatePreview(exec.ErrorMessage, defaultErrorPreviewLen),
		})
	}

	cmd.Print(formatAlignedTable(headers, rows))
	return nil
}

func loadStatistics(ctx context.Context, mon *monitor.Monitor, job string, days int) (*monitor.Statistics, error) {
	if strings.TrimSpace(job) == "" {
		return mon.GetAggregateStatistics(ctx, days)
	}
	return mon.GetStatistics(ctx, job, days)
}

func printNoMatchingRecords(cmd *cobra.Command) {
	cmd.Println(noMatchingRecordsMessage)
}

func printStats(cmd *cobra.Command, stats *monitor.Statistics) {
	cmd.Printf("Backup Job: %s\n", stats.BackupName)
	cmd.Printf("Period: %s\n\n", stats.JobsPeriod)
	cmd.Printf("Executions: %d\n", stats.TotalExecutions)
	cmd.Printf("Success Rate: %.1f%% (%d/%d)\n", stats.SuccessRate*100, stats.SuccessCount, stats.TotalExecutions)
	cmd.Printf("Failures: %d\n\n", stats.FailureCount)
	cmd.Printf("Duration:\n")
	cmd.Printf("  Average: %s\n", utils.FmtDuration(time.Duration(stats.AverageDurationMs)*time.Millisecond))
	cmd.Printf("  Median: %s\n", utils.FmtDuration(time.Duration(stats.MedianDurationMs)*time.Millisecond))
	cmd.Printf("  Min: %s\n", utils.FmtDuration(time.Duration(stats.MinDurationMs)*time.Millisecond))
	cmd.Printf("  Max: %s\n", utils.FmtDuration(time.Duration(stats.MaxDurationMs)*time.Millisecond))
	cmd.Printf("\nSize:\n")
	cmd.Printf("  Total: %d\n", stats.TotalBackupSize)
	cmd.Printf("  Average: %d\n\n", averageSize(stats.TotalBackupSize, stats.TotalExecutions))
	cmd.Printf("Trend: %s\n", stats.Trend)
	if stats.LastExecution != nil {
		cmd.Printf("\nLast Execution:\n")
		cmd.Printf("  Status: %s\n", stats.LastExecution.Status)
		cmd.Printf("  Time: %s\n", utils.FmtTimestamp(stats.LastExecution.Timestamp))
		cmd.Printf("  Duration: %s\n", utils.FmtDuration(time.Duration(stats.LastExecution.DurationMs)*time.Millisecond))
		cmd.Printf("  Size: %d\n", stats.LastExecution.FileSizeBytes)
	}
}

func printExecution(cmd *cobra.Command, exec *monitor.Execution) {
	cmd.Printf("Execution Details\n")
	cmd.Printf("=================\n\n")
	cmd.Printf("ID: %s\n", exec.ID)
	cmd.Printf("Backup Job: %s\n", exec.BackupName)
	cmd.Printf("Database Type: %s\n", exec.DatabaseType)
	cmd.Printf("Started: %s\n", utils.FmtTimestamp(exec.Timestamp))
	cmd.Printf("Duration: %s\n\n", utils.FmtDuration(time.Duration(exec.DurationMs)*time.Millisecond))
	cmd.Printf("Status: %s\n", exec.Status)
	if exec.BackupType != "" {
		cmd.Printf("Backup Type: %s\n", exec.BackupType)
	}
	if exec.ChainID != "" {
		cmd.Printf("Chain ID: %s\n", exec.ChainID)
		cmd.Printf("Chain Index: %d\n", exec.ChainIndex)
	}
	cmd.Printf("File: %s\n", exec.FilePath)
	cmd.Printf("Size: %d\n", exec.FileSizeBytes)
	if exec.DeltaSizeBytes > 0 {
		cmd.Printf("Delta Size: %d\n", exec.DeltaSizeBytes)
	}
	if exec.FullBackupSizeBytes > 0 {
		cmd.Printf("Full Backup Size: %d\n", exec.FullBackupSizeBytes)
	}
	if exec.Checksum != "" {
		cmd.Printf("Checksum: %s\n", exec.Checksum)
	}
	if exec.ErrorMessage != "" {
		cmd.Printf("Error: %s\n", exec.ErrorMessage)
	}
}

func averageSize(total int64, count int) int64 {
	if count == 0 {
		return 0
	}
	return total / int64(count)
}
