package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/notifier"
	internalrestore "github.com/denisakp/sentinel/internal/adapters/restore/runtime"
	"github.com/denisakp/sentinel/internal/config"
	domainrestore "github.com/denisakp/sentinel/internal/domain/restore"
	domainincr "github.com/denisakp/sentinel/internal/domain/restore/incremental"
	"github.com/denisakp/sentinel/internal/domain/schedule"
	"github.com/denisakp/sentinel/internal/ports"
)

func mapRestoreSourceError(job config.RestoreJob, err error) error {
	src := job.BackupSource
	switch {
	case errors.Is(err, ports.ErrLegacyEnvelope):
		return fmt.Errorf(LegacyEnvelopeRefusalMsg, job.Name, src.BackupPath)
	case errors.Is(err, ports.ErrChunkTooLarge), errors.Is(err, ports.ErrAuthTagFailed):
		return fmt.Errorf("%s: %w", friendlyDecryptMessage, err)
	case errors.Is(err, internalrestore.ErrUnsupportedRestoreSource):
		return fmt.Errorf("source type %q is not supported for restore; supported types: local, s3, gcs", src.Type)
	case errors.Is(err, internalrestore.ErrSourceObjectNotFound):
		return fmt.Errorf("backup %q not found in %s source: %w", src.BackupPath, src.Type, err)
	case errors.Is(err, internalrestore.ErrInsufficientStagingSpace):
		return fmt.Errorf("not enough disk space in staging directory %q: %w", job.StagingDir, err)
	default:
		return fmt.Errorf("restore execution failed: %w", err)
	}
}

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

	restoreDryRunCmd = &cobra.Command{
		Use:   "dry-run <job-name>",
		Short: "Simulate a restore without applying changes",
		Long:  `Test a restore job configuration without actually restoring data.`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestoreDryRun,
	}

	restoreRunCmd = &cobra.Command{
		Use:   "run [job-name]",
		Short: "Execute a restore job now (or --all for every enabled job)",
		Long:  `Run a configured restore job immediately (including staged backup download for supported sources). Use --all to run every enabled restore job concurrently up to max_concurrent_restores.`,
		Args:  cobra.MaximumNArgs(1),
		RunE:  handleRestoreRun,
	}

	restoreValidateChainCmd = &cobra.Command{
		Use:   "validate-chain <job-name>",
		Short: "Validate incremental restore chain",
		Long:  `Validate incremental restore lineage and planner readiness without executing restore operations.`,
		Args:  cobra.ExactArgs(1),
		RunE:  handleRestoreValidateChain,
	}

	restoreHistoryCmd = &cobra.Command{
		Use:   "history [job-name]",
		Short: "View restore execution history",
		Long:  `Display historical restore executions, optionally filtered by job name.`,
		RunE:  handleRestoreHistory,
	}

	// Global flags for restore commands.
	restoreConfigFile          string
	restoreLogLevel            string
	restoreAllowLegacyEnvelope bool

	// Run-only integrity escape hatch. Per-invocation, flag-only,
	// off by default — never env-defaulted or config-driven.
	restoreSkipHashVerify bool

	// Run-all flags.
	restoreRunAll   bool
	restoreParallel int

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
		restoreDryRunCmd,
		restoreValidateChainCmd,
		restoreRunCmd,
		restoreHistoryCmd,
	)

	restoreCmd.PersistentFlags().StringVar(&restoreConfigFile, "config", "", "Path to restore config file")
	restoreCmd.PersistentFlags().StringVar(&restoreLogLevel, "log-level", "info", "Log level: debug, info, warn, error")
	restoreCmd.PersistentFlags().BoolVar(&restoreAllowLegacyEnvelope, "allow-legacy-envelope", legacyEnvelopeEnvDefault(),
		"Decrypt artifacts produced before the v2 envelope fix. UNSAFE: pre-v2 streams used a flawed nonce scheme. Use only to recover plaintext for re-encryption.")

	restoreRunCmd.Flags().StringVar(&restoreGCSBucket, "gcs-bucket", "", "Google Cloud Storage bucket name")
	restoreRunCmd.Flags().StringVar(&restoreGCSProjectID, "gcs-project-id", "", "Google Cloud project ID (optional)")
	restoreRunCmd.Flags().StringVar(&restoreGCSCredentialsFile, "gcs-credentials-file", "", "Google Cloud service account key file")
	restoreRunCmd.Flags().BoolVar(&restoreKeepFile, "keep-file", false, "Keep staged restore artifact after run for debugging")
	restoreRunCmd.Flags().BoolVar(&restoreRunAll, "all", false, "Run all enabled restore jobs concurrently (up to max_concurrent_restores)")
	restoreRunCmd.Flags().IntVar(&restoreParallel, "parallel", 0, "Max concurrent restore jobs for --all (0 = use max_concurrent_restores)")
	// Registered on the run command only (not persistently) so it cannot leak
	// onto list/status/dry-run. Flag-only, off by default — no env default (an
	// env default would silently defeat integrity checking across every restore
	// on a host).
	restoreRunCmd.Flags().BoolVar(&restoreSkipHashVerify, "skip-hash-verify", false,
		"UNSAFE: proceed even if the artifact's SHA-256 does not match the manifest. "+
			"Downgrades the integrity abort to a logged WARNING. Use only for a manifest/artifact "+
			"you have independently verified, or last-copy disaster recovery.")
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Manage backup restoration and recovery",
	// Args plus RunE together, and both are needed.
	//
	// A command with no Run is not Runnable, and Cobra returns flag.ErrHelp for
	// those BEFORE it validates Args. Execute treats ErrHelp as success, so
	// `sentinel restore enable nightly` printed the entire help to stderr and
	// exited 0. A script calling one of the four removed subcommands would have
	// carried on as though it had worked, which is the failure mode this whole
	// batch of work exists to remove, reintroduced by the removal itself.
	//
	// With RunE set the command is Runnable, so NoArgs runs and rejects an unknown
	// subcommand by name with a non-zero exit. Bare `restore` still prints help
	// and exits 0, which is the conventional behaviour for a command group.
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
	Long: `Sentinel restore operations enable automated backup validation and disaster recovery testing.

Restore jobs default to DISABLED for safety. A job runs on its schedule only when its
configuration sets:

  restores:
    postgres_nightly:
      enabled: true

There is no command that enables, disables, pauses or resumes a job. Four such
subcommands existed and printed a success message without persisting anything, so an
operator had every reason to believe a job was scheduled when it was not. They were
removed rather than left lying (issue #137). Edit the configuration instead.

Examples:
  # List all restore jobs
  sentinel restore list

  # Test a restore without applying
  sentinel restore dry-run mysql_weekly_verify

  # Run a restore job immediately
  sentinel restore run postgres_nightly

  # View restore history
  sentinel restore history postgres_nightly`,
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

// jobOutcome is the result of running one restore job: OK plus the stdout to
// print (Msg) and the error to surface (Err). Used by both the single-job and
// the --all (run-all) paths so they share identical execution + result handling.
type jobOutcome struct {
	Name string
	OK   bool
	Msg  string
	Err  error
}

func handleRestoreRun(cmd *cobra.Command, args []string) error {
	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
	}

	if restoreRunAll {
		if len(args) > 0 {
			return fmt.Errorf("cannot combine --all with a job name")
		}
		return handleRestoreRunAll(ctx, cfg)
	}

	if len(args) == 0 {
		return fmt.Errorf("a restore job name is required (or use --all to run every enabled job)")
	}
	jobName := args[0]

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

	outcome := runOneRestoreJob(ctx, cfg, mon, jobName, job)
	if outcome.Msg != "" {
		fmt.Print(outcome.Msg)
	}
	return outcome.Err
}

// runOneRestoreJob executes a single restore job and returns its outcome without
// printing or returning early, so it can be composed into the run-all fan-out
// (failure is captured, never propagated to abort siblings).
func runOneRestoreJob(ctx context.Context, cfg *config.Configuration, mon *monitor.Monitor, jobName string, job config.RestoreJob) jobOutcome {
	result, err := runRestoreExecution(ctx, &internalrestore.ExecutionRequest{
		JobName:             jobName,
		Job:                 job,
		Config:              cfg,
		LockDir:             cfg.Scheduler.LockDir,
		Monitor:             mon,
		AllowLegacyEnvelope: restoreAllowLegacyEnvelope,
		SkipHashVerify:      restoreSkipHashVerify,
	})
	if err != nil {
		notifyRestoreResult(ctx, jobName, job, result, err)
		if result != nil && result.PlanningStatus == string(domainrestore.PlanStatusConfirmationRequired) {
			return jobOutcome{Name: jobName, OK: false, Err: fmt.Errorf("restore execution requires explicit fallback confirmation; set confirm_full_fallback: true for job %q", jobName)}
		}
		if result != nil && result.Status == ports.StatusSkipped {
			return jobOutcome{Name: jobName, OK: false, Err: fmt.Errorf("restore execution skipped: %s", result.Reason)}
		}
		return jobOutcome{Name: jobName, OK: false, Err: mapRestoreSourceError(job, err)}
	}

	notifyRestoreResult(ctx, jobName, job, result, nil)

	var b strings.Builder
	if result != nil && result.FallbackDecision == string(domainrestore.FallbackCandidateFullRestore) {
		fmt.Fprintf(&b, "WARNING: incremental restore fell back to full restore (reason=%s, fallback_backup_id=%s)\n", result.FallbackReason, result.FallbackBackupID)
	}
	if result != nil && result.StagedFileRetained {
		fmt.Fprintf(&b, "Restore job %q completed. Staged file retained at: %s\n", jobName, result.StagedFilePath)
	} else {
		fmt.Fprintf(&b, "Restore job %q completed successfully\n", jobName)
	}
	return jobOutcome{Name: jobName, OK: true, Msg: b.String()}
}

// effectiveRestoreConcurrency resolves the run-all concurrency: the --parallel
// override (if > 0), else max_concurrent_restores, else 1.
func effectiveRestoreConcurrency(cfg *config.Configuration, parallelFlag int) int {
	if parallelFlag > 0 {
		return parallelFlag
	}
	if cfg.MaxConcurrentRestores > 0 {
		return cfg.MaxConcurrentRestores
	}
	return 1
}

// handleRestoreRunAll runs every enabled restore job concurrently, bounded by
// the effective concurrency limit, isolating per-job failures and reporting a
// per-job aggregate.
func handleRestoreRunAll(ctx context.Context, cfg *config.Configuration) error {
	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize restore monitor: %w", err)
	}
	defer mon.Close()

	type namedJob struct {
		name string
		job  config.RestoreJob
	}
	var enabled []namedJob
	for name, job := range cfg.Restores {
		if job.Enabled == nil || *job.Enabled {
			enabled = append(enabled, namedJob{name: name, job: job})
		}
	}
	if len(enabled) == 0 {
		fmt.Println("No enabled restore jobs to run.")
		return nil
	}

	limit := effectiveRestoreConcurrency(cfg, restoreParallel)
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	outcomes := make([]jobOutcome, len(enabled))

	for i, e := range enabled {
		wg.Add(1)
		go func(i int, name string, job config.RestoreJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Contain any per-job panic so one bad job never aborts the batch.
			defer func() {
				if r := recover(); r != nil {
					outcomes[i] = jobOutcome{Name: name, OK: false, Err: fmt.Errorf("restore job %q panicked: %v", name, r)}
				}
			}()
			applyRestoreRunOverrides(&job)
			outcomes[i] = runOneRestoreJob(ctx, cfg, mon, name, job)
		}(i, e.name, e.job)
	}
	wg.Wait()

	failed := 0
	for _, o := range outcomes {
		status := "OK"
		if !o.OK {
			status = "FAILED"
			failed++
		}
		detail := strings.TrimSpace(o.Msg)
		if o.Err != nil {
			detail = o.Err.Error()
		}
		fmt.Printf("  %-30s %-7s %s\n", o.Name, status, detail)
	}
	fmt.Printf("Restore run-all: %d/%d succeeded (concurrency=%d)\n", len(outcomes)-failed, len(outcomes), limit)
	if failed > 0 {
		return fmt.Errorf("%d of %d restore jobs failed", failed, len(outcomes))
	}
	return nil
}

func handleRestoreValidateChain(cmd *cobra.Command, args []string) error {
	jobName := args[0]

	cfg, err := loadRestoreConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	job, ok := cfg.Restores[jobName]
	if !ok {
		return fmt.Errorf("restore job %q not found", jobName)
	}

	if effectiveRestoreMode(job) != "incremental" {
		return fmt.Errorf("restore job %q is not configured for incremental mode", jobName)
	}

	request, err := config.BuildAdvancedRestoreRequest(job)
	if err != nil {
		return fmt.Errorf("failed to build restore request: %w", err)
	}

	manifestPath := resolveRestoreManifestPath(job)
	plan, err := internalrestore.PlanAdvancedRestoreFromManifestPath(job, request, manifestPath)
	if err != nil {
		return fmt.Errorf("failed to plan incremental restore: %w", err)
	}
	if plan.Status != domainrestore.PlanStatusReady {
		return fmt.Errorf("chain validation failed: status=%s reason=%s", plan.Status, plan.ReasonCode)
	}

	artifacts := make([]domainincr.ChainArtifact, 0, len(plan.ResolvedBackupIDs))
	for i, backupID := range plan.ResolvedBackupIDs {
		artifacts = append(artifacts, domainincr.ChainArtifact{
			BackupID:         backupID,
			BaselineBackupID: plan.BaselineBackupID,
			ChainIndex:       i,
			ManifestPresent:  true,
			HashVerified:     true,
		})
	}

	resolved, err := domainincr.ResolveOrderedChain(artifacts, "")
	if err != nil {
		return fmt.Errorf("chain validation failed: %w", err)
	}

	cmd.Printf("Incremental chain is valid for restore job %q\n", jobName)
	cmd.Printf("  Baseline: %s\n", resolved.BaselineBackupID)
	cmd.Printf("  Target: %s\n", resolved.TargetBackupID)
	cmd.Printf("  Depth: %d\n", resolved.Depth)
	cmd.Printf("  Artifacts: %s\n", strings.Join(resolved.ArtifactIDs, ", "))

	return nil
}

func resolveRestoreManifestPath(job config.RestoreJob) string {
	path := strings.TrimSpace(job.BackupSource.BackupPath)
	if path == "" {
		return ""
	}
	if job.BackupSource.Type == "local" && job.BackupSource.LocalPath != "" && !filepath.IsAbs(path) {
		path = filepath.Join(job.BackupSource.LocalPath, path)
	}
	return path + ".manifest.json"
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

	var filter *ports.RestoreFilter
	if len(args) == 1 {
		filter = &ports.RestoreFilter{RestoreName: args[0]}
	}

	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}

	records, err := mon.ListRestoreExecutions(ctx, filter, 50, 0)
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
		if rec.FallbackReason != "" {
			fallback = rec.FallbackDecision + ":" + rec.FallbackReason
		}
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
		return ports.StatusSuccess
	case "failure":
		return ports.StatusFailed
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
	// Warn, but keep the channels that did resolve. Returning here silenced
	// every channel because one was misconfigured (#187).
	dispatcher, err := notifier.NewDispatcherFromRestoreConfig(job.Notifications)
	if err != nil {
		slog.Warn("some restore notification channels are unavailable",
			"job", jobName, "error", err.Error())
	}
	if dispatcher == nil || dispatcher.Len() == 0 {
		return
	}

	status := ports.NotifyStatusWarning
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
		if errMsg == "" && result.Reason != "" && status != ports.NotifyStatusSuccess {
			errMsg = result.Reason
		}
	}

	restoreCtx := &ports.RestoreContext{
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

	// Domain-level structural validation of schedulable restore jobs
	// (mirrors validateScheduledJobs in schedule.go).
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
			return nil, fmt.Errorf("restore job %q: %w", name, err)
		}
	}

	return cfg, nil
}
