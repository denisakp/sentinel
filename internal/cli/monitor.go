package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
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
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		if err := os.WriteFile(output, data, 0o644); err != nil {
			return fmt.Errorf("failed to write export file: %w", err)
		}

		// A status line, not data. Kept on stderr so that
		// `monitor export --output f.json` writes nothing to stdout, which is what
		// a caller piping this command expects.
		fmt.Fprintf(cmd.ErrOrStderr(), "exported %s history to %s\n", strings.ToLower(format), output)
		return nil
	},
}

func init() {
	monitorCmd.AddCommand(monitorListCmd)
	monitorCmd.AddCommand(monitorStatsCmd)
	monitorCmd.AddCommand(monitorShowCmd)
	monitorCmd.AddCommand(monitorExportCmd)
	monitorCmd.AddCommand(monitorDoctorCmd)

	monitorCmd.PersistentFlags().StringP("config", "c", "", "Path to YAML configuration file")

	monitorDoctorCmd.Flags().Bool("repair", false, "Apply any pending monitor schema migrations")
	monitorDoctorCmd.Flags().Bool("json", false, "Emit the doctor report as JSON")

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

func buildFilterFromFlags(cmd *cobra.Command) (*ports.Filter, error) {
	job, _ := cmd.Flags().GetString("job")
	status, _ := cmd.Flags().GetString("status")
	storage, _ := cmd.Flags().GetString("storage")
	dbType, _ := cmd.Flags().GetString("type")
	last, _ := cmd.Flags().GetString("last")

	filter := &ports.Filter{
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

// Output streams in this file are chosen EXPLICITLY, via cmd.OutOrStdout() and
// cmd.ErrOrStderr(), rather than through Cobra's cmd.Print family.
//
// Cobra's Print, Printf and Println write to OutOrStderr(), so every one of the
// 42 call sites here sent its output to stderr. `monitor export --format json >
// history.json` produced an empty file while the data scrolled past on the
// terminal, and `monitor list --format json | jq` piped nothing, with no error
// either way. The whole point of export is to be redirected (#165).
//
// `monitor doctor` and `repair` were already correct, so the inconsistency sat
// inside one command group. Being explicit also removes the implicit default that
// caused this: these calls no longer change stream depending on whether some
// caller happened to have set an output writer.

func printJSON(cmd *cobra.Command, executions []ports.Execution) error {
	data, err := json.MarshalIndent(executions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json output: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func printCSV(cmd *cobra.Command, mon *monitor.Monitor, filter *ports.Filter) error {
	data, err := mon.ExportHistory(context.Background(), "csv", filter)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func printTable(cmd *cobra.Command, executions []ports.Execution) error {
	out := cmd.OutOrStdout()
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

	fmt.Fprint(out, formatAlignedTable(headers, rows))
	return nil
}

func loadStatistics(ctx context.Context, mon *monitor.Monitor, job string, days int) (*monitor.Statistics, error) {
	if strings.TrimSpace(job) == "" {
		return mon.GetAggregateStatistics(ctx, days)
	}
	return mon.GetStatistics(ctx, job, days)
}

func printNoMatchingRecords(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, noMatchingRecordsMessage)
}

func printStats(cmd *cobra.Command, stats *monitor.Statistics) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Backup Job: %s\n", stats.BackupName)
	fmt.Fprintf(out, "Period: %s\n\n", stats.JobsPeriod)
	fmt.Fprintf(out, "Executions: %d\n", stats.TotalExecutions)
	fmt.Fprintf(out, "Success Rate: %.1f%% (%d/%d)\n", stats.SuccessRate*100, stats.SuccessCount, stats.TotalExecutions)
	fmt.Fprintf(out, "Failures: %d\n\n", stats.FailureCount)
	fmt.Fprintf(out, "Duration:\n")
	fmt.Fprintf(out, "  Average: %s\n", utils.FmtDuration(time.Duration(stats.AverageDurationMs)*time.Millisecond))
	fmt.Fprintf(out, "  Median: %s\n", utils.FmtDuration(time.Duration(stats.MedianDurationMs)*time.Millisecond))
	fmt.Fprintf(out, "  Min: %s\n", utils.FmtDuration(time.Duration(stats.MinDurationMs)*time.Millisecond))
	fmt.Fprintf(out, "  Max: %s\n", utils.FmtDuration(time.Duration(stats.MaxDurationMs)*time.Millisecond))
	fmt.Fprintf(out, "\nSize:\n")
	fmt.Fprintf(out, "  Total: %d\n", stats.TotalBackupSize)
	fmt.Fprintf(out, "  Average: %d\n\n", averageSize(stats.TotalBackupSize, stats.TotalExecutions))
	fmt.Fprintf(out, "Trend: %s\n", stats.Trend)
	if stats.LastExecution != nil {
		fmt.Fprintf(out, "\nLast Execution:\n")
		fmt.Fprintf(out, "  Status: %s\n", stats.LastExecution.Status)
		fmt.Fprintf(out, "  Time: %s\n", utils.FmtTimestamp(stats.LastExecution.Timestamp))
		fmt.Fprintf(out, "  Duration: %s\n", utils.FmtDuration(time.Duration(stats.LastExecution.DurationMs)*time.Millisecond))
		fmt.Fprintf(out, "  Size: %d\n", stats.LastExecution.FileSizeBytes)
	}
}

func printExecution(cmd *cobra.Command, exec *ports.Execution) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Execution Details\n")
	fmt.Fprintf(out, "=================\n\n")
	fmt.Fprintf(out, "ID: %s\n", exec.ID)
	fmt.Fprintf(out, "Backup Job: %s\n", exec.BackupName)
	fmt.Fprintf(out, "Database Type: %s\n", exec.DatabaseType)
	fmt.Fprintf(out, "Started: %s\n", utils.FmtTimestamp(exec.Timestamp))
	fmt.Fprintf(out, "Duration: %s\n\n", utils.FmtDuration(time.Duration(exec.DurationMs)*time.Millisecond))
	fmt.Fprintf(out, "Status: %s\n", exec.Status)
	if exec.BackupType != "" {
		fmt.Fprintf(out, "Backup Type: %s\n", exec.BackupType)
	}
	if exec.ChainID != "" {
		fmt.Fprintf(out, "Chain ID: %s\n", exec.ChainID)
		fmt.Fprintf(out, "Chain Index: %d\n", exec.ChainIndex)
	}
	fmt.Fprintf(out, "File: %s\n", exec.FilePath)
	fmt.Fprintf(out, "Size: %d\n", exec.FileSizeBytes)
	if exec.DeltaSizeBytes > 0 {
		fmt.Fprintf(out, "Delta Size: %d\n", exec.DeltaSizeBytes)
	}
	if exec.FullBackupSizeBytes > 0 {
		fmt.Fprintf(out, "Full Backup Size: %d\n", exec.FullBackupSizeBytes)
	}
	if exec.Checksum != "" {
		fmt.Fprintf(out, "Checksum: %s\n", exec.Checksum)
	}
	if exec.ErrorMessage != "" {
		fmt.Fprintf(out, "Error: %s\n", exec.ErrorMessage)
	}
}

func averageSize(total int64, count int) int64 {
	if count == 0 {
		return 0
	}
	return total / int64(count)
}
