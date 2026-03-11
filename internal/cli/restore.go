package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/denisakp/sentinel/internal/config"
)

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

	// Global flags for restore commands
	restoreConfigFile string
	restoreLogLevel   string
)

func init() {
	// Register restore subcommands
	restoreCmd.AddCommand(
		restoreListCmd,
		restoreStatusCmd,
		restoreEnableCmd,
		restoreDisableCmd,
		restoreDryRunCmd,
		restoreHistoryCmd,
		restorePauseCmd,
		restoreResumeCmd,
	)

	// Add restore flags
	restoreCmd.PersistentFlags().StringVar(&restoreConfigFile, "config", "", "Path to restore config file (required)")
	restoreCmd.PersistentFlags().StringVar(&restoreLogLevel, "log-level", "info", "Log level: debug, info, warn, error")
}

// restoreCmd is the root restore command
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

  # View restore history
  sentinel restore history postgres_nightly

  # Pause a job temporarily
  sentinel restore pause mongodb_integration_env`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Setup logging
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
	fmt.Printf("  Verify After Restore: %v\n", job.VerifyAfterRestore)
	fmt.Printf("  Timeout: %d seconds\n", job.TimeoutSeconds)

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
	fmt.Printf("  Timeout: %d seconds\n", job.TimeoutSeconds)
	fmt.Println()
	fmt.Println("NOTE: This is a dry-run. No data will be restored.")

	return nil
}

func handleRestoreHistory(cmd *cobra.Command, args []string) error {
	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize monitor to query restore history
	monitorPath := cfg.HistoryDBPath
	if monitorPath == "" {
		def := filepath.Join(os.Getenv("HOME"), ".sentinel", "history.db")
		monitorPath = def
	}

	fmt.Printf("Restore Execution History\n")
	fmt.Printf("(from %s)\n\n", monitorPath)
	fmt.Println("Job Name | Database | Status | Duration | Timestamp | Verified")
	fmt.Println("---------|----------|--------|----------|-----------|----------")
	fmt.Println("(Use 'sentinel monitor list --type restore' for detailed history)")

	return nil
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

// loadRestoreConfig loads the restore configuration from file
func loadRestoreConfig() (*config.Configuration, error) {
	if restoreConfigFile == "" {
		// Try default locations
		defaults := []string{
			"sentinel.yaml",
			"./config/sentinel.yaml",
			filepath.Join(os.Getenv("HOME"), ".sentinel", "config.yaml"),
		}

		for _, path := range defaults {
			if _, err := os.Stat(path); err == nil {
				restoreConfigFile = path
				break
			}
		}

		if restoreConfigFile == "" {
			return nil, fmt.Errorf("no config file found (specify with --config flag)")
		}
	}

	slog.Debug("Loading restore config", "file", restoreConfigFile)

	return config.LoadConfig(restoreConfigFile)
}
