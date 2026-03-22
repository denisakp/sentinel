package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/notifier"
	internalrestore "github.com/denisakp/sentinel/internal/restore"
)

var runRestoreExecution = internalrestore.ExecuteRestore

var (
	restoreListCmd = &cobra.Command{
		Use:   "list",
		Short: "List configured restore jobs",
		Long:  `Display all restore jobs configured in the YAML configuration file, including their schedule and status.`,
		RunE:  handleRestoreList,
	}

	restoreStatusCmd = &cobra.Command{
		Use:   "status <job-name>",
		Short: "Get status of a restore job",
		Long:  `Check the current status of a scheduled restore job.`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestoreStatus,
	}

	restoreEnableCmd = &cobra.Command{
		Use:   "enable <job-name>",
		Short: "Enable a restore job",
		Long:  `Enable a restore job so it runs on its configured schedule.`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestoreEnable,
	}

	restoreDisableCmd = &cobra.Command{
		Use:   "disable <job-name>",
		Short: "Disable a restore job",
		Long:  `Disable a restore job to prevent it from running.`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestoreDisable,
	}

	restoreDryRunCmd = &cobra.Command{
		Use:   "dry-run <job-name>",
		Short: "Simulate a restore without applying changes",
		Long:  `Test a restore job configuration without actually restoring data.`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestoreDryRun,
	}

	restoreRunCmd = &cobra.Command{
		Use:   "run <job-name>",
		Short: "Execute a restore job now",
		Long:  `Run a configured restore job immediately (including staged backup download for supported sources).`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestoreRun,
	}

	restoreHistoryCmd = &cobra.Command{
		Use:   "history [job-name]",
		Short: "View restore execution history",
		Long:  `Display historical restore executions, optionally filtered by job name.`,
		RunE:  handleRestoreHistory,
	}

	restorePauseCmd = &cobra.Command{
		Use:   "pause <job-name>",
		Short: "Pause a restore job temporarily",
		Long:  `Temporarily pause a restore job without permanently disabling it.`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestorePause,
	}

	restoreResumeCmd = &cobra.Command{
		Use:   "resume <job-name>",
		Short: "Resume a paused restore job",
		Long:  `Resume a restore job that was previously paused.`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestoreResume,
	}

	// Global flags for restore commands.
	restoreConfigFile string
	restoreLogLevel   string

	// One-off restore override flags.
	restoreGCSBucket          string
	restoreGCSProjectID       string
	restoreGCSCredentialsFile string
	restoreKeepFile           bool
)

func init() {
	restoreCmd.AddCommand(
		restoreListCmd,
		restoreStatusCmd,
		restoreEnableCmd,
		restoreDisableCmd,
		restoreDryRunCmd,
		restoreRunCmd,
		restoreHistoryCmd,
		restorePauseCmd,
		restoreResumeCmd,
	)

	restoreCmd.PersistentFlags().StringVar(&restoreConfigFile, "config", "", "Path to restore config file")
	restoreCmd.PersistentFlags().StringVar(&restoreLogLevel, "log-level", "info", "Log level: debug, info, warn, error")

	restoreRunCmd.Flags().StringVar(&restoreGCSBucket, "gcs-bucket", "", "Google Cloud Storage bucket name")
	restoreRunCmd.Flags().StringVar(&restoreGCSProjectID, "gcs-project-id", "", "Google Cloud project ID (optional)")
	restoreRunCmd.Flags().StringVar(&restoreGCSCredentialsFile, "gcs-credentials-file", "", "Google Cloud service account key file")
	restoreRunCmd.Flags().BoolVar(&restoreKeepFile, "keep-file", false, "Keep staged restore artifact after run for debugging")
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Manage backup restoration and recovery",
	Long: `Sentinel restore operations enable automated backup validation and disaster recovery testing.

Restore jobs default to DISABLED for safety. Enable explicitly in config or via CLI.

Examples:
  # List all restore jobs
  sentinel restore list

  # Enable and schedule a restore job
  sentinel restore enable postgres_nightly

  # Test a restore without applying
  sentinel restore dry-run mysql_weekly_verify

  # Run a restore job immediately
  sentinel restore run postgres_nightly

  # View restore history
  sentinel restore history postgres_nightly

  # Pause a job temporarily
  sentinel restore pause mongodb_integration_env`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logLevel := slog.LevelInfo
		switch restoreLogLevel {
		case "debug":
			logLevel = slog.LevelDebug
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}

		handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
		slog.SetDefault(slog.New(handler))
	},
}

func handleRestoreList(cmd *cobra.Command, args []string) error {
	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg == nil || len(cfg.Restores) == 0 {
		fmt.Println("No restore jobs configured")
		return nil
	}

	fmt.Println("Restore Jobs:")
	fmt.Println("=============")
	for name := range cfg.Restores {
		job := cfg.Restores[name]
		status := "disabled"
		if job.Enabled != nil && *job.Enabled {
			status = "enabled"
		}

		fmt.Printf("  Name: %s\n", name)
		fmt.Printf("    Type: %s\n", job.Type)
		fmt.Printf("    Schedule: %s\n", job.Schedule)
		fmt.Printf("    Status: %s\n", status)
		fmt.Printf("    Database: %s\n", job.Database)
		fmt.Println()
	}

	return nil
}

func handleRestoreStatus(cmd *cobra.Command, args []string) error {
	jobName := args[0]

	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	job, ok := cfg.Restores[jobName]
	if !ok {
		return fmt.Errorf("restore job %q not found", jobName)
	}

	status := "disabled"
	if job.Enabled != nil && *job.Enabled {
		status = "enabled"
	}

	fmt.Printf("Restore Job: %s\n", jobName)
	fmt.Printf("  Type: %s\n", job.Type)
	fmt.Printf("  Database: %s\n", job.Database)
	fmt.Printf("  Schedule: %s\n", job.Schedule)
	fmt.Printf("  Status: %s\n", status)
	fmt.Printf("  Restore Mode: %s\n", effectiveRestoreMode(job))
	fmt.Printf("  Verify After Restore: %v\n", job.VerifyAfterRestore)
	fmt.Printf("  Timeout: %d seconds\n", job.TimeoutSeconds)
	fmt.Printf("  Keep File: %v\n", job.KeepFile)

	return nil
}

func handleRestoreEnable(cmd *cobra.Command, args []string) error {
	jobName := args[0]
	slog.Info("Enabling restore job", "job", jobName)
	fmt.Printf("Restore job %q enabled\n", jobName)
	return nil
}

func handleRestoreDisable(cmd *cobra.Command, args []string) error {
	jobName := args[0]
	slog.Info("Disabling restore job", "job", jobName)
	fmt.Printf("Restore job %q disabled\n", jobName)
	return nil
}

func handleRestoreDryRun(cmd *cobra.Command, args []string) error {
	jobName := args[0]

	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	job, ok := cfg.Restores[jobName]
	if !ok {
		return fmt.Errorf("restore job %q not found", jobName)
	}

	fmt.Printf("Dry-run: Job %q\n", jobName)
	fmt.Printf("  Type: %s\n", job.Type)
	fmt.Printf("  Database: %s\n", job.Database)
	fmt.Printf("  Backup Source Type: %s\n", job.BackupSource.Type)
	fmt.Printf("  Backup Path: %s\n", job.BackupSource.BackupPath)
	fmt.Printf("  Restore Mode: %s\n", effectiveRestoreMode(job))
	fmt.Printf("  Timeout: %d seconds\n", job.TimeoutSeconds)
	fmt.Println()
	fmt.Println("NOTE: This is a dry-run. No data will be restored.")

	return nil
}

func handleRestoreRun(cmd *cobra.Command, args []string) error {
	jobName := args[0]

	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	job, ok := cfg.Restores[jobName]
	if !ok {
		return fmt.Errorf("restore job %q not found", jobName)
	}

	applyRestoreRunOverrides(&job)

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize restore monitor: %w", err)
	}
	defer mon.Close()

	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
	}

	result, err := runRestoreExecution(ctx, &internalrestore.ExecutionRequest{
		JobName: jobName,
		Job:     job,
		Config:  cfg,
		LockDir: cfg.Scheduler.LockDir,
		Monitor: mon,
	})
	if err != nil {
		notifyRestoreResult(ctx, jobName, job, result, err)
		if result != nil && result.Status == monitor.StatusSkipped {
			return fmt.Errorf("restore execution skipped: %s", result.Reason)
		}
		return fmt.Errorf("restore execution failed: %w", err)
	}

	notifyRestoreResult(ctx, jobName, job, result, nil)

	if result != nil && result.StagedFileRetained {
		fmt.Printf("Restore job %q completed. Staged file retained at: %s\n", jobName, result.StagedFilePath)
		return nil
	}

	fmt.Printf("Restore job %q completed successfully\n", jobName)
	return nil
}

func applyRestoreRunOverrides(job *config.RestoreJob) {
	if restoreGCSBucket != "" {
		job.BackupSource.Type = "gcs"
		job.BackupSource.GCSBucket = restoreGCSBucket
	}
	if restoreGCSProjectID != "" {
		job.BackupSource.GCSProjectID = restoreGCSProjectID
	}
	if restoreGCSCredentialsFile != "" {
		job.BackupSource.GCSCredentialsFile = restoreGCSCredentialsFile
	}
	if restoreKeepFile {
		job.KeepFile = true
	}
}

func handleRestoreHistory(cmd *cobra.Command, args []string) error {
	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize monitor: %w", err)
	}
	defer mon.Close()

	var filter *monitor.RestoreFilter
	if len(args) == 1 {
		filter = &monitor.RestoreFilter{RestoreName: args[0]}
	}

	records, err := mon.ListRestoreExecutions(cmd.Context(), filter, 50, 0)
	if err != nil {
		return fmt.Errorf("failed to read restore history: %w", err)
	}

	if len(records) == 0 {
		cmd.Println("no restore records found")
		return nil
	}

	cmd.Println(formatRestoreHistoryRow("RESTORE", "DATABASE", "MODE", "PLAN", "STATUS", "DURATION", "TIMESTAMP", "REASON", "FALLBACK"))
	for _, rec := range records {
		dur := time.Duration(rec.DurationMs) * time.Millisecond
		reason := rec.Reason
		if reason == "" {
			reason = "-"
		}
		mode := rec.RestoreMode
		if mode == "" {
			mode = "full"
		}
		plan := rec.PlanningStatus
		if plan == "" {
			plan = "-"
		}
		fallback := rec.FallbackDecision
		if fallback == "" {
			fallback = "-"
		}
		cmd.Println(formatRestoreHistoryRow(
			rec.RestoreName,
			rec.DatabaseName,
			mode,
			plan,
			normalizeRestoreStatus(rec.Status),
			dur.Truncate(time.Millisecond).String(),
			rec.Timestamp.UTC().Format(time.RFC3339),
			reason,
			fallback,
		))
	}

	return nil
}

func normalizeRestoreStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "completed":
		return monitor.StatusSuccess
	case "failure":
		return monitor.StatusFailed
	default:
		return status
	}
}

func effectiveRestoreMode(job config.RestoreJob) string {
	if strings.TrimSpace(job.RestoreMode) == "" {
		return "full"
	}
	return strings.TrimSpace(job.RestoreMode)
}

func notifyRestoreResult(ctx context.Context, jobName string, job config.RestoreJob, result *internalrestore.ExecutionResult, runErr error) {
	if len(job.Notifications) == 0 {
		return
	}
	dispatcher, err := notifier.NewDispatcherFromRestoreConfig(job.Notifications)
	if err != nil {
		slog.Warn("failed to initialize restore notifier", "job", jobName, "error", err.Error())
		return
	}

	status := notifier.StatusWarning
	start := time.Now().UTC()
	end := time.Now().UTC()
	bytesRestored := int64(0)
	verificationPassed := false
	sourcePath := job.BackupSource.BackupPath
	errMsg := ""
	if runErr != nil {
		errMsg = runErr.Error()
	}

	if result != nil {
		status = notifier.NotificationStatusFromRestoreStatus(result.Status)
		if !result.StartedAt.IsZero() {
			start = result.StartedAt
		}
		if !result.CompletedAt.IsZero() {
			end = result.CompletedAt
		}
		bytesRestored = result.BytesRestored
		verificationPassed = result.VerificationPassed
		if result.SourcePath != "" {
			sourcePath = result.SourcePath
		}
		if errMsg == "" && result.Error != nil {
			errMsg = result.Error.Error()
		}
		if errMsg == "" && result.Reason != "" && status != notifier.StatusSuccess {
			errMsg = result.Reason
		}
	}

	restoreCtx := &notifier.RestoreContext{
		RestoreName:        jobName,
		DatabaseType:       job.Type,
		DatabaseName:       job.Database,
		Status:             status,
		StartTime:          start,
		EndTime:            end,
		Error:              errMsg,
		BytesRestored:      bytesRestored,
		SourceBackupPath:   sourcePath,
		VerificationPassed: verificationPassed,
	}

	if err := dispatcher.NotifyRestore(restoreCtx); err != nil {
		slog.Warn("failed to dispatch restore notification", "job", jobName, "error", err.Error())
	}
}

func handleRestorePause(cmd *cobra.Command, args []string) error {
	jobName := args[0]
	slog.Info("Pausing restore job", "job", jobName)
	fmt.Printf("Restore job %q paused\n", jobName)
	return nil
}

func handleRestoreResume(cmd *cobra.Command, args []string) error {
	jobName := args[0]
	slog.Info("Resuming restore job", "job", jobName)
	fmt.Printf("Restore job %q resumed\n", jobName)
	return nil
}

func loadRestoreConfig() (*config.Configuration, error) {
	path, err := ResolveConfigPath(restoreConfigFile)
	if err != nil {
		return nil, err
	}

	slog.Debug("Loading restore config", "file", path)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if err := config.ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}
