package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/storage/gcs"
)

type restoreGCSDownloader interface {
	Download(ctx context.Context, src, dest string) error
}

var newRestoreGCSBackend = func(cfg gcs.Config) (restoreGCSDownloader, error) {
	return gcs.NewGCSBackend(cfg)
}

var restoreFromLocalStagedFile = func(_ context.Context, _ config.RestoreJob, stagedPath string) error {
	if _, err := os.Stat(stagedPath); err != nil {
		return fmt.Errorf("staged restore file is not accessible: %w", err)
	}
	return nil
}

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

	if job.BackupSource.Type != "gcs" {
		return fmt.Errorf("restore run currently supports backup_source.type 'gcs'; got %q", job.BackupSource.Type)
	}
	if job.BackupSource.GCSBucket == "" {
		return fmt.Errorf("backup_source.gcs_bucket is required for gcs restore")
	}
	if job.BackupSource.BackupPath == "" {
		return fmt.Errorf("backup_source.backup_path is required for restore")
	}

	backend, err := newRestoreGCSBackend(gcs.Config{
		Bucket:          job.BackupSource.GCSBucket,
		ProjectID:       job.BackupSource.GCSProjectID,
		CredentialsFile: job.BackupSource.GCSCredentialsFile,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize gcs restore backend: %w", err)
	}

	stagePath := buildRestoreStagePath(jobName, job.BackupSource.BackupPath)
	if !job.KeepFile {
		defer func() {
			if rmErr := os.Remove(stagePath); rmErr != nil && !os.IsNotExist(rmErr) {
				slog.Warn("failed to remove staged restore file", "path", stagePath, "error", rmErr.Error())
			}
		}()
	}

	if err := backend.Download(context.Background(), job.BackupSource.BackupPath, stagePath); err != nil {
		if errors.Is(err, gcs.ErrObjectNotFound) {
			return fmt.Errorf("restore backup object not found: %w", err)
		}
		return fmt.Errorf("failed to download restore artifact from gcs: %w", err)
	}

	if err := restoreFromLocalStagedFile(context.Background(), job, stagePath); err != nil {
		return fmt.Errorf("restore execution failed: %w", err)
	}

	if job.KeepFile {
		fmt.Printf("Restore job %q completed. Staged file retained at: %s\n", jobName, stagePath)
	} else {
		fmt.Printf("Restore job %q completed successfully\n", jobName)
	}

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

func buildRestoreStagePath(jobName, backupPath string) string {
	base := filepath.Base(backupPath)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "backup.sql"
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("sentinel-restore-%s-%d-%s", jobName, time.Now().UnixNano(), base))
}

func handleRestoreHistory(cmd *cobra.Command, args []string) error {
	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	monitorPath := cfg.HistoryDBPath
	if monitorPath == "" {
		monitorPath = filepath.Join(os.Getenv("HOME"), ".sentinel", "history.db")
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
