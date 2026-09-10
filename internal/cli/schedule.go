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

	"github.com/denisakp/sentinel/internal/adapters/lock"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/domain/schedule"
	"github.com/denisakp/sentinel/internal/ports"
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
		if err := validateScheduledJobs(cfg); err != nil {
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

		// Reconcile stale executions before starting the scheduler, using the
		// lock-aware logic `sentinel repair` already has.
		//
		// This used to call mon.ReconcileStaleExecutions, which has no age guard
		// and no liveness check: every row still marked `running` became
		// `interrupted`, including a backup running at that very moment. So
		// restarting the scheduler, a routine operation, corrupted the history of
		// work in progress: the run was recorded as interrupted while it carried
		// on and completed normally, and anything reacting to a failed run acted
		// on a false signal (#195).
		//
		// The issue supposed this also needed backups to take a lock, which they
		// did not at the time. They do now (#163), so a live backup holds a lock
		// and classifyStaleRunning can tell it from an abandoned one. A lock held
		// by another host is left alone rather than finalised from here.
		ctx := context.Background()
		reconciledCount, err := reconcileStaleExecutionsOnStart(ctx, mon, cfg)
		if err != nil {
			return fmt.Errorf("failed to reconcile stale executions: %w", err)
		}
		if reconciledCount > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Reconciled %d stale execution(s) from previous shutdown\n", reconciledCount)
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
				return scheduler.RunBackupWithRetry(ctx, jobCopy.Name, jobCopy.Database, func() (err error) {
					// Per-attempt panic recovery so a panicking worker
					// consumes a retry attempt rather than aborting the
					// retry loop.
					defer func() {
						if pErr, _ := scheduler.HandlePanic(recover()); pErr != nil {
							err = pErr
						}
					}()
					// Enforce scheduler.job_timeout_minutes. The deadline
					// reaches the dump subprocess through BuildContext, so a
					// hung pg_dump is killed rather than holding a
					// concurrency slot forever. Before this the key was
					// defaulted, documented, and read by nothing (#194).
					return scheduler.RunWithTimeout(ctx, cfg.Scheduler.JobTimeoutMinutes, func(jobCtx context.Context) error {
						return executeBackupJobWithMode(jobCtx, cmd, cfg, jobCopy, executionModeScheduled, backupRunOptions{})
					})
				})
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

		// Add the scheduled integrity sweep. Additive:
		// registered only when enabled with a cron; the backup/restore loops
		// above are untouched. Inherits AddJob's skip-if-running + panic
		// recovery for free.
		integrityScheduled := false
		if ic := cfg.Integrity.ScheduledCheck; ic.Enabled && ic.Cron != "" {
			if err := s.AddJob(config.IntegrityCheckJobName, ic.Cron, func() error {
				return runScheduledIntegrityCheck(ctx, cfg, mon, ic)
			}); err != nil {
				return err
			}
			integrityScheduled = true
		}

		if err := s.Start(); err != nil {
			return err
		}

		fmt.Fprintf(cmd.ErrOrStderr(), "Scheduler started with %d backup job(s) and %d restore job(s)\n",
			len(cfg.Databases), len(cfg.Restores))
		if integrityScheduled {
			fmt.Fprintf(cmd.ErrOrStderr(), "Scheduled integrity sweep registered (%s)\n", cfg.Integrity.ScheduledCheck.Cron)
		}
		stopCh := make(chan os.Signal, 1)
		signal.Notify(stopCh, syscall.SIGTERM, syscall.SIGINT)
		<-stopCh

		fmt.Fprintln(cmd.ErrOrStderr(), "scheduler stopping...")
		if err := s.Stop(); err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "scheduler stopped")
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
		// Add the scheduled integrity sweep to the listing.
		if ic := cfg.Integrity.ScheduledCheck; ic.Enabled && ic.Cron != "" {
			if err := s.AddJob(config.IntegrityCheckJobName, ic.Cron, func() error { return nil }); err != nil {
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
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
		default:
			fmt.Fprint(cmd.OutOrStdout(), renderScheduleListTable(rows))
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

func buildScheduleListRows(infos []schedule.JobInfo, restoreJobs map[string]config.RestoreJob) []scheduleListRow {
	rows := make([]scheduleListRow, 0, len(infos))
	for _, info := range infos {
		jobType := "backup"
		if _, ok := restoreJobs[info.Name]; ok {
			jobType = "restore"
		}
		if info.Name == config.IntegrityCheckJobName {
			jobType = string(schedule.KindIntegrityCheck)
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

// Output streams here are chosen explicitly. `schedule list` and `schedule
// status` are queried for data, so their results go to stdout and can be
// redirected or piped. `schedule start` is a daemon, so its lifecycle and
// progress messages go to stderr and stay out of anyone's pipe.
//
// Cobra's cmd.Print family writes to OutOrStderr(), which sent every one of
// these to stderr, so `schedule list --format json | jq` received nothing. Issue
// #165 reported this for the monitor command group; the same defect was here, and
// the documentation already said so.

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
		fmt.Fprintf(cmd.OutOrStdout(), "Job: %s\n", status.Name)
		fmt.Fprintf(cmd.OutOrStdout(), "Schedule: %s\n", status.Schedule)
		fmt.Fprintf(cmd.OutOrStdout(), "Next Execution: %s\n", status.NextExecution.Format(time.RFC3339))
		fmt.Fprintf(cmd.OutOrStdout(), "Last Execution: %s\n", status.LastExecution.Format(time.RFC3339))
		fmt.Fprintf(cmd.OutOrStdout(), "Last Status: %s\n", status.LastStatus)
		if status.LastError != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Last Error: %s\n", status.LastError)
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

// validateScheduledJobs runs the pure domain validation (schedule.Validate)
// over every job the scheduler would register: enabled backup jobs with a
// schedule and enabled restore jobs with a schedule. Cron-expression parsing
// stays in the scheduler runtime adapter.
func validateScheduledJobs(cfg *config.Configuration) error {
	for _, job := range cfg.Databases {
		if job.Enabled != nil && !*job.Enabled {
			continue
		}
		if job.Schedule == "" {
			continue
		}
		if err := schedule.Validate(schedule.ScheduledJob{
			Name:     job.Name,
			CronExpr: job.Schedule,
			Kind:     schedule.KindBackup,
			Enabled:  true,
		}); err != nil {
			return fmt.Errorf("backup job %q: %w", job.Name, err)
		}
	}
	for name, job := range cfg.Restores {
		if job.Enabled != nil && !*job.Enabled {
			continue
		}
		if job.Schedule == "" {
			continue
		}
		if err := schedule.Validate(schedule.ScheduledJob{
			Name:     name,
			CronExpr: job.Schedule,
			Kind:     schedule.KindRestore,
			Enabled:  true,
		}); err != nil {
			return fmt.Errorf("restore job %q: %w", name, err)
		}
	}
	if ic := cfg.Integrity.ScheduledCheck; ic.Enabled && ic.Cron != "" {
		if err := schedule.Validate(schedule.ScheduledJob{
			Name:     config.IntegrityCheckJobName,
			CronExpr: ic.Cron,
			Kind:     schedule.KindIntegrityCheck,
			Enabled:  true,
		}); err != nil {
			return fmt.Errorf("scheduled integrity check: %w", err)
		}
	}
	return nil
}

// executeRestoreJob executes a single restore job
func executeRestoreJob(
	cmd *cobra.Command,
	cfg *config.Configuration,
	mon ports.Recorder,
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

	if result != nil && result.Status == ports.StatusSkipped && cmd != nil {
		cmd.Printf("Restore job %s skipped: %s\\n", job.Name, result.Reason)
	}

	return nil
}

// reconcileStaleExecutionsOnStart finalises rows left in `running` by a previous
// process, and ONLY those.
//
// It reuses classifyStaleRunning, the same decision `sentinel repair` makes, so
// the two cannot disagree about what counts as abandoned. A row whose job still
// holds a live lock is left running; a lock held by another host is left to that
// host; only a row with no live lock is marked interrupted.
//
// Returns the number of rows finalised.
func reconcileStaleExecutionsOnStart(ctx context.Context, mon *monitor.Monitor, cfg *config.Configuration) (int, error) {
	rows, err := mon.GetStaleRunningExecutions(ctx)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}

	lm := lock.NewManager(cfg.Scheduler.LockDir)
	threshold := staleThreshold(cfg)
	thisHost, _ := os.Hostname()

	finalised := 0
	for i := range rows {
		r := rows[i]
		jl, _ := lm.ReadLock(r.BackupName)
		decision, _ := classifyStaleRunning(jl, threshold, thisHost)
		if decision != staleFinalize {
			continue
		}
		if rerr := mon.RecordInterrupted(ctx, r.ID, false, nil, "unclean shutdown (scheduler start)"); rerr != nil {
			return finalised, rerr
		}
		finalised++
	}
	return finalised, nil
}
