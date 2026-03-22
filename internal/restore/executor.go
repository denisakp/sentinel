package restore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/lock"
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/monitor"
	mariadbrestore "github.com/denisakp/sentinel/pkg/restore/mariadb_restore"
	mongorestore "github.com/denisakp/sentinel/pkg/restore/mongo_restore"
	mysqlrestore "github.com/denisakp/sentinel/pkg/restore/mysql_restore"
	pgrestore "github.com/denisakp/sentinel/pkg/restore/pg_restore"
)

var (
	ErrRestoreLockConflict = errors.New("restore execution lock conflict")
	ErrRestoreInterrupted  = errors.New("restore interrupted")
)

var (
	stageRestoreSource    = StageRestoreSource
	applyRestorePreflight = applyPreflight
	executeRestoreEngine  = executeEngineRestore
	runPostgresRestore    = pgrestore.Restore
	runPostgresPITR       = executePostgresPITR
)

type ExecutionRequest struct {
	JobName           string
	Job               config.RestoreJob
	Config            *config.Configuration
	LockDir           string
	Monitor           *monitor.Monitor
	VerifyAfterRun    func(context.Context, config.RestoreJob) (bool, error)
	PostRestoreHook   func(context.Context, config.RestoreJob, string) error
	ConflictEvaluator func(context.Context, config.RestoreJob, string) error
}

type ExecutionResult struct {
	ExecutionID          string
	Status               string
	Reason               string
	RestoreMode          string
	PlanningStatus       string
	RequestedPITRTimeUTC *time.Time
	BaselineBackupID     string
	FallbackDecision     string
	RecoveryTimelineID   string
	StartedAt            time.Time
	CompletedAt          time.Time
	Duration             time.Duration
	StagedFilePath       string
	StagedFileRetained   bool
	SourcePath           string
	SourceType           string
	BytesRestored        int64
	VerificationPassed   bool
	ConflictStrategy     string
	TimeoutSeconds       int
	Error                error
}

func ExecuteRestore(ctx context.Context, req *ExecutionRequest) (*ExecutionResult, error) {
	if req == nil {
		return nil, fmt.Errorf("restore request is required")
	}
	job := req.Job
	job.Name = req.JobName
	if err := config.ValidateRestoreJob(&job); err != nil {
		return nil, err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	result := &ExecutionResult{
		Status:           monitor.StatusFailed,
		StartedAt:        time.Now().UTC(),
		SourceType:       job.BackupSource.Type,
		ConflictStrategy: effectiveConflictStrategy(job),
		TimeoutSeconds:   job.TimeoutSeconds,
	}

	if req.LockDir != "" {
		lockMgr := lock.NewManager(req.LockDir)
		if _, err := lockMgr.Acquire(req.JobName); err != nil {
			if errors.Is(err, lock.ErrLockExists) {
				result.Status = monitor.StatusSkipped
				result.Reason = "lock_conflict"
				result.CompletedAt = time.Now().UTC()
				result.Duration = result.CompletedAt.Sub(result.StartedAt)
				result.Error = ErrRestoreLockConflict
				recordRestoreExecution(ctx, req, job, result)
				return result, ErrRestoreLockConflict
			}
			return nil, fmt.Errorf("failed to acquire restore lock: %w", err)
		}
		defer func() {
			_ = lockMgr.Release(req.JobName)
		}()
	}

	if job.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(job.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	artifact, err := stageRestoreSource(ctx, job)
	if err != nil {
		result.Reason = classifyRestoreError(err)
		result.Error = err
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		recordRestoreExecution(ctx, req, job, result)
		return result, err
	}
	result.StagedFilePath = artifact.Path
	result.SourcePath = artifact.SourcePath
	result.BytesRestored = artifact.SizeBytes
	defer func() {
		if artifact == nil {
			return
		}
		if job.KeepFile {
			artifact.Retained = true
			result.StagedFileRetained = true
			return
		}
		_ = CleanupStagedArtifact(artifact)
	}()

	planningInput, err := config.BuildAdvancedRestoreRequest(job)
	if err != nil {
		result.Reason = classifyRestoreError(err)
		result.Error = err
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		recordRestoreExecution(ctx, req, job, result)
		return result, err
	}

	plan, err := PlanAdvancedRestoreFromManifestPath(job, planningInput, artifact.ManifestPath)
	if err != nil {
		result.Reason = classifyRestoreError(err)
		result.Error = err
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		recordRestoreExecution(ctx, req, job, result)
		return result, err
	}
	if plan.Status == PlanStatusRejected {
		err := fmt.Errorf("restore planning rejected: %s", plan.ReasonCode)
		result.Reason = plan.ReasonCode
		result.RestoreMode = string(plan.Mode)
		result.PlanningStatus = string(plan.Status)
		result.RequestedPITRTimeUTC = plan.ResolvedTargetTimeUTC
		result.BaselineBackupID = plan.BaselineBackupID
		result.FallbackDecision = string(plan.Fallback)
		result.Error = err
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		recordRestoreExecution(ctx, req, job, result)
		return result, err
	}
	if plan.Status == PlanStatusConfirmationRequired {
		err := fmt.Errorf("restore planning requires confirmation: %s", plan.ReasonCode)
		result.Reason = plan.ReasonCode
		result.RestoreMode = string(plan.Mode)
		result.PlanningStatus = string(plan.Status)
		result.RequestedPITRTimeUTC = plan.ResolvedTargetTimeUTC
		result.BaselineBackupID = plan.BaselineBackupID
		result.FallbackDecision = string(plan.Fallback)
		result.Error = err
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		recordRestoreExecution(ctx, req, job, result)
		return result, err
	}
	job.RestoreMode = string(plan.Mode)
	result.RestoreMode = string(plan.Mode)
	result.PlanningStatus = string(plan.Status)
	result.RequestedPITRTimeUTC = plan.ResolvedTargetTimeUTC
	result.BaselineBackupID = plan.BaselineBackupID
	result.FallbackDecision = string(plan.Fallback)
	result.RecoveryTimelineID = plan.RequestedTimeline

	stagedPath, err := applyRestorePreflight(ctx, req.Config, artifact)
	if err != nil {
		result.Reason = classifyRestoreError(err)
		result.Error = err
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		recordRestoreExecution(ctx, req, job, result)
		return result, err
	}
	result.StagedFilePath = stagedPath

	if req.ConflictEvaluator != nil {
		if err := req.ConflictEvaluator(ctx, job, stagedPath); err != nil {
			result.Reason = classifyRestoreError(err)
			result.Error = err
			result.CompletedAt = time.Now().UTC()
			result.Duration = result.CompletedAt.Sub(result.StartedAt)
			recordRestoreExecution(ctx, req, job, result)
			return result, err
		}
	}

	if err := executeRestoreEngine(ctx, job, stagedPath); err != nil {
		result.Reason = classifyRestoreError(err)
		result.Error = err
		if errors.Is(err, context.DeadlineExceeded) {
			result.Status = monitor.StatusTimeout
		}
		if errors.Is(err, context.Canceled) {
			result.Reason = "interrupted"
		}
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		recordRestoreExecution(ctx, req, job, result)
		return result, err
	}

	if req.PostRestoreHook != nil {
		if err := req.PostRestoreHook(ctx, job, stagedPath); err != nil {
			result.Reason = classifyRestoreError(err)
			result.Error = err
			result.CompletedAt = time.Now().UTC()
			result.Duration = result.CompletedAt.Sub(result.StartedAt)
			recordRestoreExecution(ctx, req, job, result)
			return result, err
		}
	}

	requiresVerification := job.VerifyAfterRestore || job.RestoreMode == "pitr" || job.RestoreMode == "incremental"
	if requiresVerification {
		if req.VerifyAfterRun == nil {
			err := fmt.Errorf("verification handler is required for restore mode %q", job.RestoreMode)
			result.Reason = classifyRestoreError(err)
			result.Error = err
			result.CompletedAt = time.Now().UTC()
			result.Duration = result.CompletedAt.Sub(result.StartedAt)
			recordRestoreExecution(ctx, req, job, result)
			return result, err
		}
		verified, err := req.VerifyAfterRun(ctx, job)
		if err != nil {
			result.Reason = classifyRestoreError(err)
			result.Error = err
			result.CompletedAt = time.Now().UTC()
			result.Duration = result.CompletedAt.Sub(result.StartedAt)
			recordRestoreExecution(ctx, req, job, result)
			return result, err
		}
		if !verified {
			err := fmt.Errorf("post-restore verification failed")
			result.Reason = classifyRestoreError(err)
			result.Error = err
			result.CompletedAt = time.Now().UTC()
			result.Duration = result.CompletedAt.Sub(result.StartedAt)
			recordRestoreExecution(ctx, req, job, result)
			return result, err
		}
		result.VerificationPassed = true
	}

	result.Status = monitor.StatusSuccess
	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)
	recordRestoreExecution(ctx, req, job, result)
	return result, nil
}

func applyPreflight(ctx context.Context, cfg *config.Configuration, artifact *StagedArtifact) (string, error) {
	if artifact == nil {
		return "", fmt.Errorf("staged artifact is required")
	}
	if artifact.ManifestPath == "" {
		return artifact.Path, nil
	}

	m, err := manifest.ReadManifest(artifact.ManifestPath)
	if err != nil {
		if errors.Is(err, manifest.ErrNoManifest) {
			return artifact.Path, nil
		}
		return "", err
	}

	var keyProvider crypto.KeyProvider
	if cfg != nil && (cfg.EncryptionKeyEnv != "" || cfg.EncryptionKeyFile != "") {
		keyProvider = &crypto.FileKeyProvider{EnvVar: cfg.EncryptionKeyEnv, FilePath: cfg.EncryptionKeyFile}
	}

	reader, err := PreRestoreVerifyAndDecrypt(ctx, m, artifact.Path, keyProvider)
	if err != nil {
		if errors.Is(err, ErrHashMismatch) {
			return "", fmt.Errorf("integrity_check_failed: %w", err)
		}
		return "", err
	}

	decryptedPath := artifact.Path + ".plaintext"
	file, err := os.OpenFile(decryptedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("failed to create decrypted staged file: %w", err)
	}
	if _, err := io.Copy(file, reader); err != nil {
		file.Close()
		return "", fmt.Errorf("failed to write decrypted staged file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to finalize decrypted staged file: %w", err)
	}
	artifact.Path = decryptedPath
	return decryptedPath, nil
}

func executeEngineRestore(ctx context.Context, job config.RestoreJob, stagedPath string) error {
	password, err := config.RestorePasswordFromEnv(job.PasswordEnv)
	if err != nil {
		return err
	}

	switch job.Type {
	case "postgres":
		if job.RestoreMode == "pitr" {
			return runPostgresPITR(ctx, job, password, stagedPath)
		}
		args, err := config.BuildPgRestoreArgs(job, password, stagedPath)
		if err != nil {
			return err
		}
		return runPostgresRestore(ctx, args)
	case "mysql":
		args, err := config.BuildMySQLRestoreArgs(job, password, stagedPath)
		if err != nil {
			return err
		}
		return mysqlrestore.Restore(ctx, args)
	case "mariadb":
		args, err := config.BuildMariaDBRestoreArgs(job, password, stagedPath)
		if err != nil {
			return err
		}
		return mariadbrestore.Restore(ctx, args)
	case "mongodb":
		args, err := config.BuildMongoRestoreArgs(job, stagedPath)
		if err != nil {
			return err
		}
		return mongorestore.Restore(ctx, args)
	default:
		return fmt.Errorf("unsupported restore type: %s", job.Type)
	}
}

func recordRestoreExecution(ctx context.Context, req *ExecutionRequest, job config.RestoreJob, result *ExecutionResult) {
	if req == nil || req.Monitor == nil || result == nil {
		return
	}
	entry := &monitor.RestoreExecution{
		ID:                   result.ExecutionID,
		RestoreName:          req.JobName,
		DatabaseType:         job.Type,
		DatabaseName:         job.Database,
		RestoreMode:          result.RestoreMode,
		PlanningStatus:       result.PlanningStatus,
		RequestedPITRTimeUTC: result.RequestedPITRTimeUTC,
		BaselineBackupID:     result.BaselineBackupID,
		FallbackDecision:     result.FallbackDecision,
		RecoveryTimelineID:   result.RecoveryTimelineID,
		SourceType:           job.BackupSource.Type,
		ConflictStrategy:     result.ConflictStrategy,
		Timestamp:            result.StartedAt,
		DurationMs:           result.Duration.Milliseconds(),
		Status:               result.Status,
		ErrorMessage:         errorMessage(result.Error),
		ErrorReason:          result.Reason,
		Reason:               result.Reason,
		SourceBackupPath:     result.SourcePath,
		StagedFilePath:       result.StagedFilePath,
		StagedFileRetained:   result.StagedFileRetained,
		BytesRestored:        result.BytesRestored,
		VerificationPassed:   result.VerificationPassed,
		TimeoutSeconds:       result.TimeoutSeconds,
		CreatedAt:            result.StartedAt,
		FinishedAt:           &result.CompletedAt,
	}
	_ = req.Monitor.RecordRestoreExecution(ctx, entry)
	result.ExecutionID = entry.ID
}

func classifyRestoreError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrInsufficientStagingSpace):
		return "insufficient_staging_space"
	case errors.Is(err, ErrRestoreLockConflict):
		return "lock_conflict"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled), errors.Is(err, ErrRestoreInterrupted):
		return "interrupted"
	default:
		return "restore_failed"
	}
}

func effectiveConflictStrategy(job config.RestoreJob) string {
	if job.ConflictStrategy == "" {
		return "error"
	}
	return job.ConflictStrategy
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
