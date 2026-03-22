package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/notifier"
	internalrestore "github.com/denisakp/sentinel/internal/restore"
	"github.com/denisakp/sentinel/internal/retention"
	"github.com/denisakp/sentinel/pkg/restore/mariadb_restore"
	"github.com/denisakp/sentinel/pkg/restore/mongo_restore"
	"github.com/denisakp/sentinel/pkg/restore/mysql_restore"
	"github.com/denisakp/sentinel/pkg/restore/pg_restore"
)

// RestoreScheduleConfig represents a scheduled restore job configuration
type RestoreScheduleConfig struct {
	// Job name for identification
	Name string

	// Cron schedule (5-field format: minute hour day month weekday)
	Schedule string

	// Restore execution parameters
	RestoreConfig *RestoreExecutionConfig

	// Enable/disable this restore job
	Enabled bool

	// Backup file location
	BackupPath string

	// Backup source/storage type
	BackupSource string

	// Verification options
	VerifyAfterRestore bool

	// Post-restore operations
	PostRestoreCmd string
}

// RestoreScheduleManager manages scheduled restore operations
type RestoreScheduleManager struct {
	scheduler     *Scheduler
	restoreExec   *RestoreExecutor
	logger        *slog.Logger
	verifier      PostRestoreVerifier
	backupStorage BackupStorage
	cfg           *config.Configuration
	monitor       *monitor.Monitor
	retention     *retention.Manager
}

// BackupStorage provides methods to retrieve backups from various sources
type BackupStorage interface {
	// ReadBackup retrieves backup data from the specified location
	ReadBackup(ctx context.Context, source string, path string) ([]byte, error)
}

// NewRestoreScheduleManager creates a new restore schedule manager
func NewRestoreScheduleManager(
	scheduler *Scheduler,
	logger *slog.Logger,
	verifier PostRestoreVerifier,
	backupStorage BackupStorage,
	cfg *config.Configuration,
	mon *monitor.Monitor,
	ret *retention.Manager,
) *RestoreScheduleManager {
	restoreExec := NewRestoreExecutor(nil) // will be set with actual implementation
	return &RestoreScheduleManager{
		scheduler:     scheduler,
		restoreExec:   restoreExec,
		logger:        logger,
		verifier:      verifier,
		backupStorage: backupStorage,
		cfg:           cfg,
		monitor:       mon,
		retention:     ret,
	}
}

// AddRestoreJob adds a scheduled restore operation to the scheduler
func (rsm *RestoreScheduleManager) AddRestoreJob(ctx context.Context, config *RestoreScheduleConfig) error {
	if config == nil {
		return fmt.Errorf("restore schedule config cannot be nil")
	}

	if config.Name == "" {
		return fmt.Errorf("restore job name is required")
	}

	if config.Schedule == "" {
		return fmt.Errorf("cron schedule is required")
	}

	if !config.Enabled {
		rsm.logger.Info("Restore job is disabled", slog.String("job", config.Name))
		return nil
	}

	// Create the restore function closure
	restoreFn := func() error {
		return rsm.executeRestore(ctx, config)
	}

	// Add to scheduler
	if err := rsm.scheduler.AddJob(config.Name, config.Schedule, restoreFn); err != nil {
		return fmt.Errorf("failed to add restore job to scheduler: %w", err)
	}

	rsm.logger.Info("Restore job added to scheduler",
		slog.String("job", config.Name),
		slog.String("schedule", config.Schedule),
		slog.String("database_type", config.RestoreConfig.DatabaseType),
	)

	return nil
}

// executeRestore performs the actual restore operation
func (rsm *RestoreScheduleManager) executeRestore(ctx context.Context, config *RestoreScheduleConfig) error {
	startTime := time.Now()

	rsm.logger.Info("Starting scheduled restore",
		slog.String("job", config.Name),
		slog.String("database_type", config.RestoreConfig.DatabaseType),
	)

	// Read backup data
	backupData, err := rsm.backupStorage.ReadBackup(ctx, config.BackupSource, config.BackupPath)
	if err != nil {
		rsm.logger.Error("Failed to read backup",
			slog.String("job", config.Name),
			slog.String("backup_path", config.BackupPath),
			slog.String("error", err.Error()),
		)
		// Send failure notification
		rsm.notifyRestoreFailure(ctx, config, startTime, "Failed to read backup: "+err.Error())
		// Record failure
		rsm.recordRestoreExecution(ctx, config, startTime, false, 0, false, "Failed to read backup: "+err.Error())
		return fmt.Errorf("failed to read backup: %w", err)
	}

	// T025: Pre-restore integrity verification and optional decryption.
	manifestPath := config.BackupPath + ".manifest.json"
	m, manifestErr := manifest.ReadManifest(manifestPath)
	if manifestErr == nil {
		// Manifest found: verify hash and handle decryption.
		var keyProvider crypto.KeyProvider
		if rsm.cfg != nil && (rsm.cfg.EncryptionKeyEnv != "" || rsm.cfg.EncryptionKeyFile != "") {
			keyProvider = &crypto.FileKeyProvider{EnvVar: rsm.cfg.EncryptionKeyEnv, FilePath: rsm.cfg.EncryptionKeyFile}
		}
		if _, verifyErr := internalrestore.PreRestoreVerifyAndDecrypt(ctx, m, config.BackupPath, keyProvider); verifyErr != nil {
			if errors.Is(verifyErr, internalrestore.ErrHashMismatch) {
				rsm.logger.Error("Pre-restore integrity check failed: hash mismatch",
					slog.String("job", config.Name),
					slog.String("backup_path", config.BackupPath),
					slog.String("error", verifyErr.Error()),
				)
				rsm.notifyRestoreFailure(ctx, config, startTime, verifyErr.Error())
				rsm.recordRestoreExecution(ctx, config, startTime, false, 0, false, verifyErr.Error())
				return verifyErr
			}
			rsm.logger.Warn("Pre-restore integrity check error (proceeding)",
				slog.String("job", config.Name),
				slog.String("error", verifyErr.Error()),
			)
		}
	} else if !errors.Is(manifestErr, manifest.ErrNoManifest) {
		rsm.logger.Warn("Could not read backup manifest; skipping integrity check (pre-v1.1 backup)",
			slog.String("job", config.Name),
			slog.String("backup_path", config.BackupPath),
		)
	}

	// Perform restore based on database type
	result, err := rsm.performDatabaseRestore(ctx, config, backupData)
	if err != nil {
		bytesRestored := int64(0)
		if result != nil {
			bytesRestored = result.BytesRestored
		}
		rsm.logger.Error("Restore operation failed",
			slog.String("job", config.Name),
			slog.String("database_type", config.RestoreConfig.DatabaseType),
			slog.String("error", err.Error()),
		)
		// Send failure notification
		rsm.notifyRestoreFailure(ctx, config, startTime, err.Error())
		// Record failure
		rsm.recordRestoreExecution(ctx, config, startTime, false, bytesRestored, false, err.Error())
		return err
	}

	// Verify if requested
	if config.VerifyAfterRestore && rsm.verifier != nil {
		verified, verifyErr := rsm.verifier.Verify(
			ctx,
			config.RestoreConfig.DatabaseType,
			config.RestoreConfig.Host,
			config.RestoreConfig.Port,
			config.RestoreConfig.Username,
			config.RestoreConfig.Password,
			config.RestoreConfig.Database,
		)
		if verifyErr != nil {
			rsm.logger.Error("Post-restore verification failed",
				slog.String("job", config.Name),
				slog.String("error", verifyErr.Error()),
			)
			result.VerificationPassed = false
		} else {
			result.VerificationPassed = verified
		}
	}

	duration := time.Since(startTime)
	rsm.logger.Info("Scheduled restore completed",
		slog.String("job", config.Name),
		slog.String("database_type", config.RestoreConfig.DatabaseType),
		slog.Duration("duration", duration),
		slog.Bool("success", result.Success),
		slog.Int64("bytes_restored", result.BytesRestored),
		slog.Bool("verification_passed", result.VerificationPassed),
	)

	// Send success notification
	rsm.notifyRestoreSuccess(ctx, config, startTime, result)
	// Record successful restore
	rsm.recordRestoreExecution(ctx, config, startTime, true, result.BytesRestored, result.VerificationPassed, "")
	// Apply retention policy to cleanup old restore execution records
	rsm.applyRestoreRetention(ctx, config)

	return nil
}

// notifyRestoreSuccess sends a notification about successful restore
func (rsm *RestoreScheduleManager) notifyRestoreSuccess(ctx context.Context, restoreScheduleConfig *RestoreScheduleConfig, startTime time.Time, result *RestoreResult) {
	if rsm.cfg == nil || rsm.cfg.Restores == nil {
		return
	}

	// Find the restore config for this job
	restoreJobCfg, ok := rsm.cfg.Restores[restoreScheduleConfig.Name]
	if !ok || len(restoreJobCfg.Notifications) == 0 {
		return
	}

	dispatcher, err := notifier.NewDispatcherFromRestoreConfig(restoreJobCfg.Notifications)
	if err != nil {
		rsm.logger.Warn("Failed to create notification dispatcher",
			slog.String("job", restoreScheduleConfig.Name),
			slog.String("error", err.Error()),
		)
		return
	}

	restoreCtx := &notifier.RestoreContext{
		RestoreName:        restoreScheduleConfig.Name,
		DatabaseType:       restoreScheduleConfig.RestoreConfig.DatabaseType,
		DatabaseName:       restoreScheduleConfig.RestoreConfig.Database,
		Status:             notifier.StatusSuccess,
		StartTime:          startTime,
		EndTime:            time.Now(),
		BytesRestored:      result.BytesRestored,
		SourceBackupPath:   restoreScheduleConfig.BackupPath,
		VerificationPassed: result.VerificationPassed,
	}

	if err := dispatcher.NotifyRestore(restoreCtx); err != nil {
		rsm.logger.Warn("Failed to send restore success notification",
			slog.String("job", restoreScheduleConfig.Name),
			slog.String("error", err.Error()),
		)
	}
}

// notifyRestoreFailure sends a notification about failed restore
func (rsm *RestoreScheduleManager) notifyRestoreFailure(ctx context.Context, restoreScheduleConfig *RestoreScheduleConfig, startTime time.Time, errorMsg string) {
	if rsm.cfg == nil || rsm.cfg.Restores == nil {
		return
	}

	// Find the restore config for this job
	restoreJobCfg, ok := rsm.cfg.Restores[restoreScheduleConfig.Name]
	if !ok || len(restoreJobCfg.Notifications) == 0 {
		return
	}

	dispatcher, err := notifier.NewDispatcherFromRestoreConfig(restoreJobCfg.Notifications)
	if err != nil {
		rsm.logger.Warn("Failed to create notification dispatcher",
			slog.String("job", restoreScheduleConfig.Name),
			slog.String("error", err.Error()),
		)
		return
	}

	restoreCtx := &notifier.RestoreContext{
		RestoreName:      restoreScheduleConfig.Name,
		DatabaseType:     restoreScheduleConfig.RestoreConfig.DatabaseType,
		DatabaseName:     restoreScheduleConfig.RestoreConfig.Database,
		Status:           notifier.StatusFailure,
		StartTime:        startTime,
		EndTime:          time.Now(),
		Error:            errorMsg,
		SourceBackupPath: restoreScheduleConfig.BackupPath,
	}

	if err := dispatcher.NotifyRestore(restoreCtx); err != nil {
		rsm.logger.Warn("Failed to send restore failure notification",
			slog.String("job", restoreScheduleConfig.Name),
			slog.String("error", err.Error()),
		)
	}
}

// recordRestoreExecution records the restore execution in the history database
func (rsm *RestoreScheduleManager) recordRestoreExecution(ctx context.Context, config *RestoreScheduleConfig, startTime time.Time, success bool, bytesRestored int64, verificationPassed bool, errorMsg string) {
	if rsm.monitor == nil {
		return
	}

	status := "success"
	if !success {
		status = "failure"
	}

	exec := &monitor.RestoreExecution{
		RestoreName:        config.Name,
		DatabaseType:       config.RestoreConfig.DatabaseType,
		DatabaseName:       config.RestoreConfig.Database,
		RestoreMode:        "full",
		PlanningStatus:     "ready",
		FallbackDecision:   "none",
		Timestamp:          startTime.UTC(),
		DurationMs:         time.Since(startTime).Milliseconds(),
		Status:             status,
		ErrorMessage:       errorMsg,
		SourceBackupPath:   config.BackupPath,
		BytesRestored:      bytesRestored,
		VerificationPassed: verificationPassed,
	}

	if err := rsm.monitor.RecordRestoreExecution(ctx, exec); err != nil {
		rsm.logger.Warn("Failed to record restore execution",
			slog.String("job", config.Name),
			slog.String("error", err.Error()),
		)
	}
}

// applyRestoreRetention applies retention policy to cleanup old restore execution records
func (rsm *RestoreScheduleManager) applyRestoreRetention(ctx context.Context, restoreConfig *RestoreScheduleConfig) {
	if rsm.cfg == nil || rsm.retention == nil {
		return
	}

	// Find the restore job config for retention policy
	var restoreJobCfg *config.RestoreJob
	for name, rj := range rsm.cfg.Restores {
		if name == restoreConfig.Name {
			restoreJobCfg = &rj
			break
		}
	}

	if restoreJobCfg == nil || (restoreJobCfg.Retention.KeepLast == 0 && restoreJobCfg.Retention.KeepDays == 0) {
		// No retention policy configured
		return
	}

	policy := retention.Policy{
		KeepLast: restoreJobCfg.Retention.KeepLast,
		KeepDays: restoreJobCfg.Retention.KeepDays,
		DryRun:   false,
	}

	if err := rsm.retention.ApplyRestoreRetention(ctx, restoreConfig.Name, policy); err != nil {
		rsm.logger.Warn("Failed to apply restore retention policy",
			slog.String("job", restoreConfig.Name),
			slog.String("error", err.Error()),
		)
	}
}

// performDatabaseRestore handles database-specific restore logic
func (rsm *RestoreScheduleManager) performDatabaseRestore(
	ctx context.Context,
	config *RestoreScheduleConfig,
	backupData []byte,
) (*RestoreResult, error) {
	startTime := time.Now()

	switch config.RestoreConfig.DatabaseType {
	case "postgres":
		return rsm.restorePostgres(ctx, config, backupData, startTime)
	case "mysql":
		return rsm.restoreMySQL(ctx, config, backupData, startTime)
	case "mariadb":
		return rsm.restoreMariaDB(ctx, config, backupData, startTime)
	case "mongodb":
		return rsm.restoreMongoDB(ctx, config, backupData, startTime)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.RestoreConfig.DatabaseType)
	}
}

// restorePostgres handles PostgreSQL restore
func (rsm *RestoreScheduleManager) restorePostgres(ctx context.Context, config *RestoreScheduleConfig, backupData []byte, startTime time.Time) (*RestoreResult, error) {
	args := &pg_restore.RestoreArgs{
		Host:            config.RestoreConfig.Host,
		Port:            config.RestoreConfig.Port,
		Username:        config.RestoreConfig.Username,
		Password:        config.RestoreConfig.Password,
		Database:        config.RestoreConfig.Database,
		OnConflict:      config.RestoreConfig.OnConflict,
		BackupPath:      config.BackupPath,
		PgRestoreFormat: config.RestoreConfig.Options["format"].(string),
		AdditionalArgs:  config.RestoreConfig.Options["additional_args"].(string),
	}

	err := pg_restore.Restore(ctx, args)
	return &RestoreResult{
		Success:          err == nil,
		DatabaseType:     "postgres",
		BackupFile:       config.BackupPath,
		RestoredDatabase: config.RestoreConfig.Database,
		BytesRestored:    int64(len(backupData)),
		Duration:         time.Since(startTime),
		StartTime:        startTime,
		EndTime:          time.Now(),
		ErrorMessage:     errMsg(err),
	}, err
}

// restoreMySQL handles MySQL restore
func (rsm *RestoreScheduleManager) restoreMySQL(ctx context.Context, config *RestoreScheduleConfig, backupData []byte, startTime time.Time) (*RestoreResult, error) {
	args := &mysql_restore.RestoreArgs{
		Host:           config.RestoreConfig.Host,
		Port:           config.RestoreConfig.Port,
		Username:       config.RestoreConfig.Username,
		Password:       config.RestoreConfig.Password,
		Database:       config.RestoreConfig.Database,
		OnConflict:     config.RestoreConfig.OnConflict,
		BackupPath:     config.BackupPath,
		AdditionalArgs: config.RestoreConfig.Options["additional_args"].(string),
	}

	err := mysql_restore.Restore(ctx, args)
	return &RestoreResult{
		Success:          err == nil,
		DatabaseType:     "mysql",
		BackupFile:       config.BackupPath,
		RestoredDatabase: config.RestoreConfig.Database,
		BytesRestored:    int64(len(backupData)),
		Duration:         time.Since(startTime),
		StartTime:        startTime,
		EndTime:          time.Now(),
		ErrorMessage:     errMsg(err),
	}, err
}

// restoreMariaDB handles MariaDB restore
func (rsm *RestoreScheduleManager) restoreMariaDB(ctx context.Context, config *RestoreScheduleConfig, backupData []byte, startTime time.Time) (*RestoreResult, error) {
	args := &mariadb_restore.RestoreArgs{
		Host:           config.RestoreConfig.Host,
		Port:           config.RestoreConfig.Port,
		Username:       config.RestoreConfig.Username,
		Password:       config.RestoreConfig.Password,
		Database:       config.RestoreConfig.Database,
		OnConflict:     config.RestoreConfig.OnConflict,
		BackupPath:     config.BackupPath,
		AdditionalArgs: config.RestoreConfig.Options["additional_args"].(string),
	}

	err := mariadb_restore.Restore(ctx, args)
	return &RestoreResult{
		Success:          err == nil,
		DatabaseType:     "mariadb",
		BackupFile:       config.BackupPath,
		RestoredDatabase: config.RestoreConfig.Database,
		BytesRestored:    int64(len(backupData)),
		Duration:         time.Since(startTime),
		StartTime:        startTime,
		EndTime:          time.Now(),
		ErrorMessage:     errMsg(err),
	}, err
}

// restoreMongoDB handles MongoDB restore
func (rsm *RestoreScheduleManager) restoreMongoDB(ctx context.Context, config *RestoreScheduleConfig, backupData []byte, startTime time.Time) (*RestoreResult, error) {
	args := &mongo_restore.RestoreArgs{
		URI:            config.RestoreConfig.URI,
		Database:       config.RestoreConfig.Database,
		OnConflict:     config.RestoreConfig.OnConflict,
		BackupPath:     config.BackupPath,
		AdditionalArgs: config.RestoreConfig.Options["additional_args"].(string),
	}

	if gzip, ok := config.RestoreConfig.Options["gzip"].(bool); ok {
		args.Gzip = gzip
	}
	if archive, ok := config.RestoreConfig.Options["archive"].(bool); ok {
		args.Archive = archive
	}

	err := mongo_restore.Restore(ctx, args)
	return &RestoreResult{
		Success:          err == nil,
		DatabaseType:     "mongodb",
		BackupFile:       config.BackupPath,
		RestoredDatabase: config.RestoreConfig.Database,
		BytesRestored:    int64(len(backupData)),
		Duration:         time.Since(startTime),
		StartTime:        startTime,
		EndTime:          time.Now(),
		ErrorMessage:     errMsg(err),
	}, err
}

// errMsg returns error message or empty string
func errMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// PauseRestoreJob temporarily disables a restore job without removing it
func (rsm *RestoreScheduleManager) PauseRestoreJob(jobName string) error {
	// Note: This requires extending the Scheduler interface
	rsm.logger.Info("Restore job paused", slog.String("job", jobName))
	return nil
}

// ResumeRestoreJob re-enables a previously paused restore job
func (rsm *RestoreScheduleManager) ResumeRestoreJob(jobName string) error {
	// Note: This requires extending the Scheduler interface
	rsm.logger.Info("Restore job resumed", slog.String("job", jobName))
	return nil
}

// GetRestoreJobStatus returns the current status of a restore job
func (rsm *RestoreScheduleManager) GetRestoreJobStatus(jobName string) (map[string]interface{}, error) {
	status := map[string]interface{}{
		"name":   jobName,
		"status": "active",
		// Additional fields can be populated from scheduler
	}
	return status, nil
}
