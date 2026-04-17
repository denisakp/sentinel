package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/scheduler"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage automated backup and restore scheduling",
	Long:  "Start, stop, list, or view status of scheduled backups and restores defined in YAML configuration.\n\nExamples:\n  sentinel schedule start --config sentinel.yaml\n  sentinel schedule list --config sentinel.yaml",
}

var runScheduledRestoreExecution = scheduler.ExecuteScheduledRestoreWithRunner

var scheduleStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the backup and restore scheduler",
	Long:  "Start the scheduler and run backups/restores at their configured cron schedules.",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("config")
		cfg, err := LoadAndValidateConfig(path)
		if err != nil {
			return err
		}

		// Initialize monitor for execution tracking and reconciliation
		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			return fmt.Errorf("failed to initialize monitor: %w", err)
		}
		defer func() {
			if closeErr := mon.Close(); closeErr != nil {
				cmd.PrintErrf("warning: failed to close monitor: %v\n", closeErr)
			}
		}()

		// Reconcile stale executions before starting scheduler
		ctx := context.Background()
		reconciledCount, err := mon.ReconcileStaleExecutions(ctx)
		if err != nil {
			return fmt.Errorf("failed to reconcile stale executions: %w", err)
		}
		if reconciledCount > 0 {
			cmd.Printf("Reconciled %d stale execution(s) from previous shutdown\n", reconciledCount)
		}

		s := scheduler.NewScheduler(cfg.MaxConcurrentBackups)

		// Add backup jobs
		for _, job := range cfg.Databases {
			if job.Enabled != nil && !*job.Enabled {
				continue
			}
			if job.Schedule == "" {
				continue
			}
			jobCopy := job
			if err := s.AddJob(job.Name, job.Schedule, func() error {
				return executeBackupJobWithMode(cmd, cfg, jobCopy, executionModeScheduled, backupRunOptions{})
			}); err != nil {
				return err
			}
		}

		// Add restore jobs
		restoreLimiter := make(chan struct{}, cfg.Scheduler.MaxConcurrentRestores)
		for name, restoreJob := range cfg.Restores {
			if restoreJob.Enabled != nil && !*restoreJob.Enabled {
				continue
			}
			if restoreJob.Schedule == "" {
				continue
			}
			restoreCopy := restoreJob
			restoreCopy.Name = name
			if err := s.AddJob(name, restoreJob.Schedule, func() error {
				return executeRestoreJob(cmd, cfg, mon, restoreCopy, restoreLimiter)
			}); err != nil {
				return err
			}
		}

		if err := s.Start(); err != nil {
			return err
		}

		cmd.Printf("Scheduler started with %d backup job(s) and %d restore job(s)\n",
			len(cfg.Databases), len(cfg.Restores))
		stopCh := make(chan os.Signal, 1)
		signal.Notify(stopCh, syscall.SIGTERM, syscall.SIGINT)
		<-stopCh

		cmd.Println("scheduler stopping...")
		if err := s.Stop(); err != nil {
			return err
		}
		cmd.Println("scheduler stopped")
		return nil
	},
}

var scheduleStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the backup scheduler",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("schedule stop is not supported without a running daemon; use Ctrl+C on 'schedule start'")
	},
}

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all scheduled backups and restores",
	Long:  "List all scheduled backups and restores with their next execution time in table or JSON format.",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("config")
		cfg, err := LoadAndValidateConfig(path)
		if err != nil {
			return err
		}

		s := scheduler.NewScheduler(cfg.MaxConcurrentBackups)
		for _, job := range cfg.Databases {
			if job.Enabled != nil && !*job.Enabled {
				continue
			}
			if job.Schedule == "" {
				continue
			}
			if err := s.AddJob(job.Name, job.Schedule, func() error { return nil }); err != nil {
				return err
			}
		}
		// Add restore jobs to list
		for name, restoreJob := range cfg.Restores {
			if restoreJob.Enabled != nil && !*restoreJob.Enabled {
				continue
			}
			if restoreJob.Schedule == "" {
				continue
			}
			if err := s.AddJob(name, restoreJob.Schedule, func() error { return nil }); err != nil {
				return err
			}
		}
		if err := s.Start(); err != nil {
			return err
		}
		defer func() {
			_ = s.Stop()
		}()

		infos := s.ListJobs()
		rows := buildScheduleListRows(infos, cfg.Restores)
		format, _ := cmd.Flags().GetString("format")
		switch strings.ToLower(format) {
		case "json":
			data, err := json.MarshalIndent(rows, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal schedule list json: %w", err)
			}
			cmd.Println(string(data))
		default:
			cmd.Print(renderScheduleListTable(rows))
		}
		return nil
	},
}

type scheduleListRow struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	Schedule      string `json:"schedule"`
	NextExecution string `json:"next_execution"`
	LastStatus    string `json:"last_status"`
}

func buildScheduleListRows(infos []scheduler.JobInfo, restoreJobs map[string]config.RestoreJob) []scheduleListRow {
	rows := make([]scheduleListRow, 0, len(infos))
	for _, info := range infos {
		jobType := "backup"
		if _, ok := restoreJobs[info.Name]; ok {
			jobType = "restore"
		}
		rows = append(rows, scheduleListRow{
			Type:          jobType,
			Name:          info.Name,
			Schedule:      info.ScheduleExpr,
			NextExecution: info.NextExecution.Format(time.RFC3339),
			LastStatus:    info.LastStatus,
		})
	}
	return rows
}

func renderScheduleListTable(rows []scheduleListRow) string {
	headers := []string{"TYPE", "NAME", "SCHEDULE", "NEXT EXECUTION"}
	data := make([][]string, 0, len(rows))
	for _, row := range rows {
		data = append(data, []string{row.Type, row.Name, row.Schedule, row.NextExecution})
	}
	return formatAlignedTable(headers, data)
}

var scheduleStatusCmd = &cobra.Command{
	Use:   "status <job-name>",
	Short: "Show scheduler status",
	Long: "Show the status and recent execution history for a scheduled backup job.\n\n" +
		"Examples:\n" +
		"  sentinel schedule status postgres-sample --config sentinel.yaml",
	Example: "  sentinel schedule status postgres-sample --config sentinel.yaml",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("config")
		cfg, err := LoadAndValidateConfig(path)
		if err != nil {
			return err
		}

		s := scheduler.NewScheduler(cfg.MaxConcurrentBackups)
		for _, job := range cfg.Databases {
			if job.Enabled != nil && !*job.Enabled {
				continue
			}
			if job.Schedule == "" {
				continue
			}
			if err := s.AddJob(job.Name, job.Schedule, func() error { return nil }); err != nil {
				return err
			}
		}
		if err := s.Start(); err != nil {
			return err
		}
		defer func() {
			_ = s.Stop()
		}()

		status, err := s.JobStatus(args[0])
		if err != nil {
			return err
		}
		cmd.Printf("Job: %s\n", status.Name)
		cmd.Printf("Schedule: %s\n", status.Schedule)
		cmd.Printf("Next Execution: %s\n", status.NextExecution.Format(time.RFC3339))
		cmd.Printf("Last Execution: %s\n", status.LastExecution.Format(time.RFC3339))
		cmd.Printf("Last Status: %s\n", status.LastStatus)
		if status.LastError != "" {
			cmd.Printf("Last Error: %s\n", status.LastError)
		}
		return nil
	},
}

func init() {
	scheduleCmd.AddCommand(scheduleStartCmd)
	scheduleCmd.AddCommand(scheduleStopCmd)
	scheduleCmd.AddCommand(scheduleListCmd)
	scheduleCmd.AddCommand(scheduleStatusCmd)

	// Add --config flag to schedule commands
	scheduleStartCmd.Flags().StringP("config", "c", "", "Path to YAML configuration file")
	scheduleListCmd.Flags().StringP("config", "c", "", "Path to YAML configuration file")
	scheduleListCmd.Flags().String("format", "table", "Output format (table/json)")
	scheduleStatusCmd.Flags().StringP("config", "c", "", "Path to YAML configuration file")
}

// executeRestoreJob executes a single restore job
func executeRestoreJob(
	cmd *cobra.Command,
	cfg *config.Configuration,
	mon *monitor.Monitor,
	job config.RestoreJob,
	limiter chan struct{},
) error {
	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
		cmd.Printf("Executing restore job: %s (type: %s, database: %s)\\n", job.Name, job.Type, job.Database)
	}

	result, err := runScheduledRestoreExecution(ctx, cfg, job.Name, job, mon, limiter, runRestoreExecution)
	if err != nil {
		return err
	}

	if result != nil && result.Status == monitor.StatusSkipped && cmd != nil {
		cmd.Printf("Restore job %s skipped: %s\\n", job.Name, result.Reason)
	}

	return nil
}
