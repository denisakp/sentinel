package restore

// Restore Executor. Owns the restore critical path — control flow ported
// verbatim from the deleted internal/restore/executor.go::ExecuteRestore.
// Effects go through the 9 injected ports or through Job hooks for
// adapter-backed steps the ports cannot express (staging from configured
// sources, preflight decryption, engine arg construction, binlog/oplog
// replay). The single construction site is internal/adapters/restore/runtime.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	domainincr "github.com/denisakp/sentinel/internal/domain/restore/incremental"
	"github.com/denisakp/sentinel/internal/ports"
)

// Executor runs restore jobs end-to-end through injected ports + Job hooks.
type Executor struct {
	restores  ports.RestoreBuilder
	storage   ports.StorageBackend
	decrypt   ports.DecryptReader
	hasher    ports.Hasher
	rec       ports.Recorder
	notif     ports.Dispatcher
	locks     ports.LockManager
	prober    ports.DBProber
	assembler ports.ChainAssembler
}

// NewExecutor constructs an Executor over the 9 architectural ports.
// Ports not exercised by a given configuration may be nil; the corresponding
// step degrades to a no-op or is covered by a Job hook (see progress.md
// notes).
func NewExecutor(
	restores ports.RestoreBuilder,
	storage ports.StorageBackend,
	decrypt ports.DecryptReader,
	hasher ports.Hasher,
	rec ports.Recorder,
	notif ports.Dispatcher,
	locks ports.LockManager,
	prober ports.DBProber,
	assembler ports.ChainAssembler,
) *Executor {
	return &Executor{
		restores:  restores,
		storage:   storage,
		decrypt:   decrypt,
		hasher:    hasher,
		rec:       rec,
		notif:     notif,
		locks:     locks,
		prober:    prober,
		assembler: assembler,
	}
}

// Run executes the restore Job end-to-end: Validate → Lock → Stage → Plan →
// Preflight → (Chain stage + Assemble) → Engine restore → (Replay) →
// Post-hooks → Verify → Record. Behavior parity with the pre-carve
// ExecuteRestore is the binding contract.
func (e *Executor) Run(ctx context.Context, job Job) (*RunResult, error) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	result := &RunResult{
		Status:           ports.StatusFailed,
		StartedAt:        time.Now().UTC(),
		SourceType:       job.SourceType,
		ConflictStrategy: job.ConflictStrategy,
		TimeoutSeconds:   job.TimeoutSeconds,
	}

	fail := func(err error) (*RunResult, error) {
		result.Reason = classifyRestoreError(err)
		result.Error = err
		if errors.Is(err, context.DeadlineExceeded) {
			result.Status = ports.StatusTimeout
		}
		if errors.Is(err, context.Canceled) {
			result.Reason = "interrupted"
		}
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		e.record(ctx, job, result)
		return result, err
	}

	if e.locks != nil {
		if _, err := e.locks.TryAcquire(job.Name, time.Hour); err != nil {
			if errors.Is(err, ports.ErrLockHeld) {
				result.Status = ports.StatusSkipped
				result.Reason = "lock_conflict"
				result.CompletedAt = time.Now().UTC()
				result.Duration = result.CompletedAt.Sub(result.StartedAt)
				result.Error = ErrRestoreLockConflict
				e.record(ctx, job, result)
				return result, ErrRestoreLockConflict
			}
			return nil, fmt.Errorf("failed to acquire restore lock: %w", err)
		}
		defer func() {
			_ = e.locks.Release(job.Name)
		}()
	}

	if job.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(job.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	if job.StageSource == nil {
		return fail(fmt.Errorf("stage source hook is required"))
	}
	artifact, err := job.StageSource(ctx)
	if err != nil {
		return fail(err)
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
	var assembledArtifact *StagedArtifact
	defer func() {
		if assembledArtifact == nil {
			return
		}
		if job.KeepFile {
			assembledArtifact.Retained = true
			result.StagedFileRetained = true
			return
		}
		_ = os.RemoveAll(assembledArtifact.Path)
	}()

	if job.BuildPlanRequest == nil {
		return fail(fmt.Errorf("advanced restore request is required"))
	}
	planRequest, err := job.BuildPlanRequest()
	if err != nil {
		return fail(err)
	}
	plan, err := PlanFromManifestPath(job.Engine, planRequest, artifact.ManifestPath, job.LoadPlanManifest)
	if err != nil {
		return fail(err)
	}
	if plan.Status == PlanStatusRejected {
		err := fmt.Errorf("restore planning rejected: %s", plan.ReasonCode)
		result.Reason = plan.ReasonCode
		applyPlanToResult(result, plan)
		result.Error = err
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		e.record(ctx, job, result)
		return result, err
	}
	if plan.Status == PlanStatusConfirmationRequired {
		err := fmt.Errorf("fallback_required_confirmation: %s", plan.ReasonCode)
		result.Status = ports.StatusSkipped
		result.Reason = plan.ReasonCode
		applyPlanToResult(result, plan)
		result.Error = err
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		e.record(ctx, job, result)
		return result, err
	}
	job.RestoreMode = string(plan.Mode)
	applyPlanToResult(result, plan)
	result.RecoveryTimelineID = plan.RequestedTimeline
	result.ChainDepth = plan.ChainDepth

	stagedPath := artifact.Path
	if job.Preflight != nil {
		stagedPath, err = job.Preflight(ctx, artifact)
		if err != nil {
			return fail(err)
		}
	}
	result.StagedFilePath = stagedPath
	manifestPaths := make([]string, 0)
	if artifact.ManifestPath != "" {
		manifestPaths = append(manifestPaths, artifact.ManifestPath)
	}

	if job.RestoreMode == "incremental" && len(plan.ResolvedBackupIDs) > 0 {
		chainArtifacts := make([]domainincr.ChainArtifact, 0, len(plan.ResolvedBackupIDs))
		for i, backupID := range plan.ResolvedBackupIDs {
			chainArtifact := domainincr.ChainArtifact{
				BackupID:        backupID,
				ChainIndex:      i,
				ManifestPresent: true,
				HashVerified:    true,
			}
			if i > 0 {
				chainArtifact.BaselineBackupID = plan.BaselineBackupID
			}
			chainArtifacts = append(chainArtifacts, chainArtifact)
		}
		resolved, err := domainincr.ResolveOrderedChain(chainArtifacts, "")
		if err != nil {
			return fail(err)
		}
		result.ChainDepth = resolved.Depth

		if job.StageChain == nil {
			return fail(fmt.Errorf("stage chain hook is required for incremental restore"))
		}
		chainStaged, err := job.StageChain(ctx, resolved.ArtifactIDs)
		if err != nil {
			return fail(err)
		}
		defer func() {
			if job.KeepFile {
				for _, staged := range chainStaged {
					if staged == nil {
						continue
					}
					staged.Retained = true
				}
				result.StagedFileRetained = true
				return
			}
			_ = CleanupStagedArtifacts(chainStaged)
		}()

		paths := make([]string, 0, len(chainStaged))
		for _, staged := range chainStaged {
			if staged != nil && staged.Path != "" {
				paths = append(paths, staged.Path)
			}
			if staged != nil && staged.ManifestPath != "" {
				manifestPaths = append(manifestPaths, staged.ManifestPath)
			}
		}
		if (job.Engine == "mysql" || job.Engine == "mariadb") && len(paths) > 0 {
			stagedPath = paths[0]
			result.StagedFilePath = stagedPath
		}
		if job.Engine == "postgres" && len(paths) > 1 {
			if e.assembler == nil {
				return fail(fmt.Errorf("chain assembler is not configured"))
			}
			assemblyStart := time.Now().UTC()
			combinedPath, err := e.assembler.AssemblePostgresChain(ctx, job.StagingDir, paths, "")
			result.AssemblyDurationMs = time.Since(assemblyStart).Milliseconds()
			if err != nil {
				return fail(err)
			}
			assembledArtifact = &StagedArtifact{Path: combinedPath, SourcePath: combinedPath}
			stagedPath = combinedPath
			result.StagedFilePath = combinedPath
		}
	}

	if job.ConflictEvaluator != nil {
		if err := job.ConflictEvaluator(ctx, stagedPath); err != nil {
			return fail(err)
		}
	}

	if err := e.engineRestore(ctx, job, stagedPath); err != nil {
		return fail(err)
	}

	if job.RestoreMode == "incremental" && (job.Engine == "mysql" || job.Engine == "mariadb") {
		binlogSources, err := e.collectBinlogArtifacts(job, manifestPaths)
		if err != nil {
			return fail(err)
		}
		if len(binlogSources) > 0 {
			if job.BinlogReplay == nil {
				return fail(fmt.Errorf("binlog replay hook is required for incremental %s restore", job.Engine))
			}
			if replayErr := job.BinlogReplay(ctx, binlogSources); replayErr != nil {
				return fail(replayErr)
			}
		}
	}

	if job.RestoreMode == "incremental" && job.Engine == "mongodb" {
		oplogSources, err := e.collectOplogArtifacts(job, manifestPaths)
		if err != nil {
			return fail(err)
		}
		for _, archivePath := range oplogSources {
			if job.OplogReplay == nil {
				return fail(fmt.Errorf("oplog replay hook is required for incremental mongodb restore"))
			}
			if replayErr := job.OplogReplay(ctx, archivePath); replayErr != nil {
				return fail(replayErr)
			}
		}
	}

	if job.PostRestoreHook != nil {
		if err := job.PostRestoreHook(ctx, stagedPath); err != nil {
			return fail(err)
		}
	}

	requiresVerification := job.VerifyAfterRestore || job.RestoreMode == "pitr" || job.RestoreMode == "incremental"
	if requiresVerification {
		if job.VerifyAfterRun == nil {
			return fail(fmt.Errorf("verification handler is required for restore mode %q", job.RestoreMode))
		}
		verified, err := job.VerifyAfterRun(ctx)
		if err != nil {
			return fail(err)
		}
		if !verified {
			return fail(fmt.Errorf("post-restore verification failed"))
		}
		result.VerificationPassed = true
	}

	result.Status = ports.StatusSuccess
	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)
	e.record(ctx, job, result)
	return result, nil
}

// engineRestore dispatches the staged artifact to the engine adapter
// through ports.RestoreBuilder. The driving runtime supplies the options
// hook (engine arg construction, password resolution) and the builder
// (engine selection + PITR routing).
func (e *Executor) engineRestore(ctx context.Context, job Job, stagedPath string) error {
	if job.RestoreOptions == nil {
		return fmt.Errorf("restore options hook is required")
	}
	if e.restores == nil {
		return fmt.Errorf("restore builder is not configured")
	}

	opts, err := job.RestoreOptions(stagedPath)
	if err != nil {
		return err
	}
	_, err = e.restores.Build(ports.RestoreBuildContext{
		Context: ctx,
		JobID:   job.Name,
		Options: opts,
	})
	return err
}

func applyPlanToResult(result *RunResult, plan AdvancedRestorePlan) {
	result.RestoreMode = string(plan.Mode)
	result.PlanningStatus = string(plan.Status)
	result.RequestedPITRTimeUTC = plan.ResolvedTargetTimeUTC
	result.BaselineBackupID = plan.BaselineBackupID
	result.FallbackDecision = string(plan.Fallback)
	result.FallbackReason = plan.FallbackReason
	result.FallbackBackupID = plan.FallbackBackupID
}

// record persists the restore execution history row through the Recorder
// port. The driving runtime wraps the recorder to feed the incremental
// metrics observer first (pre-carve ObserveIncrementalRestore behavior).
func (e *Executor) record(ctx context.Context, job Job, result *RunResult) {
	entry := &ports.RestoreExecution{
		ID:                   result.ExecutionID,
		RestoreName:          job.Name,
		DatabaseType:         job.Engine,
		DatabaseName:         job.Database,
		RestoreMode:          result.RestoreMode,
		PlanningStatus:       result.PlanningStatus,
		RequestedPITRTimeUTC: result.RequestedPITRTimeUTC,
		BaselineBackupID:     result.BaselineBackupID,
		FallbackDecision:     result.FallbackDecision,
		FallbackReason:       result.FallbackReason,
		FallbackBackupID:     result.FallbackBackupID,
		RecoveryTimelineID:   result.RecoveryTimelineID,
		SourceType:           job.SourceType,
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
		ChainDepth:           result.ChainDepth,
		AssemblyDurationMs:   result.AssemblyDurationMs,
		CreatedAt:            result.StartedAt,
		FinishedAt:           &result.CompletedAt,
	}
	if e.rec == nil {
		return
	}
	_ = e.rec.RecordRestoreExecution(ctx, entry)
	result.ExecutionID = entry.ID
}

// CleanupStagedArtifact removes a staged artifact + its manifest sidecar.
func CleanupStagedArtifact(artifact *StagedArtifact) error {
	if artifact == nil {
		return nil
	}
	var cleanupErr error
	if artifact.ManifestPath != "" {
		if err := os.Remove(artifact.ManifestPath); err != nil && !os.IsNotExist(err) {
			cleanupErr = err
		}
	}
	if err := os.Remove(artifact.Path); err != nil && !os.IsNotExist(err) && cleanupErr == nil {
		cleanupErr = err
	}
	return cleanupErr
}

// CleanupStagedArtifacts removes every staged artifact, joining errors.
func CleanupStagedArtifacts(artifacts []*StagedArtifact) error {
	var cleanupErr error
	for _, artifact := range artifacts {
		if err := CleanupStagedArtifact(artifact); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func classifyRestoreError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrInsufficientStagingSpace):
		return "insufficient_staging_space"
	case strings.Contains(err.Error(), "broken_lineage_chain"):
		return "broken_lineage_chain"
	case strings.Contains(err.Error(), "timeline_mismatch"):
		return "timeline_mismatch"
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

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// collectBinlogArtifacts aggregates binlog artifact paths from staged chain
// manifests via the Job's ReadManifest hook.
func (e *Executor) collectBinlogArtifacts(job Job, manifestPaths []string) ([]string, error) {
	if len(manifestPaths) == 0 || job.ReadManifest == nil {
		return nil, nil
	}

	artifacts := make(map[string]struct{})
	for _, manifestPath := range manifestPaths {
		if strings.TrimSpace(manifestPath) == "" {
			continue
		}
		m, err := job.ReadManifest(manifestPath)
		if err != nil {
			if errors.Is(err, ports.ErrNoManifest) {
				continue
			}
			return nil, fmt.Errorf("failed to read chain manifest %s: %w", manifestPath, err)
		}
		if m == nil || m.AdvancedRestore == nil || m.AdvancedRestore.IncrementalLineage == nil {
			continue
		}
		for _, artifact := range m.AdvancedRestore.IncrementalLineage.BinlogArtifacts {
			if strings.TrimSpace(artifact) == "" {
				continue
			}
			artifacts[artifact] = struct{}{}
		}
	}

	out := make([]string, 0, len(artifacts))
	for artifact := range artifacts {
		out = append(out, artifact)
	}
	sort.Strings(out)
	return out, nil
}

// collectOplogArtifacts aggregates oplog archive paths from staged chain
// manifests via the Job's ReadManifest hook.
func (e *Executor) collectOplogArtifacts(job Job, manifestPaths []string) ([]string, error) {
	if len(manifestPaths) == 0 || job.ReadManifest == nil {
		return nil, nil
	}

	var artifacts []string
	seen := make(map[string]struct{})
	for _, manifestPath := range manifestPaths {
		if strings.TrimSpace(manifestPath) == "" {
			continue
		}
		m, err := job.ReadManifest(manifestPath)
		if err != nil {
			if errors.Is(err, ports.ErrNoManifest) {
				continue
			}
			return nil, fmt.Errorf("failed to read chain manifest %s: %w", manifestPath, err)
		}
		if m == nil || m.AdvancedRestore == nil || m.AdvancedRestore.IncrementalLineage == nil {
			continue
		}
		artifact := strings.TrimSpace(m.AdvancedRestore.IncrementalLineage.OplogArtifactPath)
		if artifact == "" {
			continue
		}
		if _, exists := seen[artifact]; exists {
			continue
		}
		seen[artifact] = struct{}{}
		artifacts = append(artifacts, artifact)
	}

	return artifacts, nil
}
