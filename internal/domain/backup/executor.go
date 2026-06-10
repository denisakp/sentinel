package backup

// Backup Executor (spec 038 Sub-PR K, FR-008). Owns the per-job backup
// critical path; every effect goes through the 9 injected ports (or a Job
// hook for adapter-backed steps a single port instance cannot express).
// Driving adapters (internal/cli/backup.go, internal/scheduler/) construct
// it via internal/cli/backup_factory.go and call Run.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

// Executor runs backup jobs end-to-end through injected ports.
type Executor struct {
	dumps     ports.DumpBuilder
	storage   ports.StorageBackend
	encrypt   ports.EncryptWriter
	hasher    ports.Hasher
	rec       ports.Recorder
	notif     ports.Dispatcher
	locks     ports.LockManager
	prober    ports.DBProber
	manifests ports.ManifestStore
}

// NewExecutor constructs an Executor over the 9 architectural ports
// (FR-008). Ports not exercised by a given configuration may be nil; the
// corresponding step degrades to a no-op (e.g. nil Dispatcher = no
// notifications configured, nil LockManager = locking handled by the
// runtime that invoked Run).
func NewExecutor(
	dumps ports.DumpBuilder,
	storage ports.StorageBackend,
	encrypt ports.EncryptWriter,
	hasher ports.Hasher,
	rec ports.Recorder,
	notif ports.Dispatcher,
	locks ports.LockManager,
	prober ports.DBProber,
	manifests ports.ManifestStore,
) *Executor {
	return &Executor{
		dumps:     dumps,
		storage:   storage,
		encrypt:   encrypt,
		hasher:    hasher,
		rec:       rec,
		notif:     notif,
		locks:     locks,
		prober:    prober,
		manifests: manifests,
	}
}

// Run executes the Job end-to-end: Validate → (Lock) → (Ping) → Dump →
// Pipeline (manifest / encryption / incremental artifacts) → Record →
// Notify. Connectivity-gate failures come back wrapped in *RetriableErr so
// the scheduler runtime can loop on IsRetriable.
func (e *Executor) Run(ctx context.Context, job Job) (RunResult, error) {
	res := RunResult{StartedAt: time.Now()}

	// 1. Validate.
	if err := ValidateDbType(job.Engine); err != nil {
		return res, fmt.Errorf("backup '%s': %w", job.Name, err)
	}
	if job.Options == nil {
		return res, fmt.Errorf("backup '%s': dump options are required", job.Name)
	}
	if e.dumps == nil {
		return res, fmt.Errorf("backup '%s': dump builder is not configured", job.Name)
	}

	// 2. Lock (optional — nil LockManager means the calling runtime already
	// serializes this job, which is today's scheduler behavior).
	if e.locks != nil {
		if _, err := e.locks.TryAcquire(job.Name, 0); err != nil {
			var held *ports.HeldError
			if errors.As(err, &held) {
				res.Skipped = true
				res.FinishedAt = time.Now()
				return res, nil
			}
			return res, fmt.Errorf("backup '%s': %w", job.Name, err)
		}
		defer func() { _ = e.locks.Release(job.Name) }()
	}

	// 3. Connectivity gate (optional).
	if job.PingBeforeDump && e.prober != nil {
		if err := e.prober.Ping(ctx, job.DBConn); err != nil {
			runErr := &RetriableErr{Err: fmt.Errorf("backup '%s': connectivity check failed: %w", job.Name, err)}
			res.FinishedAt = time.Now()
			e.record(ctx, job, &res, runErr, nil)
			e.notify(job, &res, runErr)
			return res, runErr
		}
	}

	// 4–5. Plan + dump. The full-vs-incremental decision is derived inside
	// the pipeline (it needs the produced artifact size, same as pre-carve).
	build, dumpErr := e.dumps.Build(ports.BuildContext{
		Context: ctx,
		JobID:   job.Name,
		Options: job.Options,
	})
	runErr := dumpErr
	res.Digest = build.Digest

	// 6. Post-dump pipeline (manifest, encryption, incremental artifacts).
	var sec *SecurityOutcome
	if runErr == nil {
		outcome, warnings, secErr := e.ApplyArtifactSecurity(ctx, job, build.Digest)
		res.Warnings = append(res.Warnings, warnings...)
		if secErr != nil {
			runErr = fmt.Errorf("backup '%s': security processing failed: %w", job.Name, secErr)
		} else {
			sec = outcome
		}
	}

	res.FinishedAt = time.Now()
	res.ArtifactPath, res.ArtifactSize = ResolveArtifactRef(job.StorageType, job.LocalPath, job.OutName, job.GCSBucket)
	if sec != nil {
		res.ManifestPath = sec.ManifestPath
		res.Encrypted = sec.Encrypted
		res.HashValue = sec.HashValue
	}

	// 8. Record history. 9. Notify. (Retention sweep — contract step 7 —
	// stays in the driving adapter; see progress.md Sub-PR K notes.)
	e.record(ctx, job, &res, runErr, sec)
	e.notify(job, &res, runErr)

	return res, runErr
}

// record persists the execution history row (+ security info). Relocated
// from internal/cli/backup.go::recordBackupExecution; persistence failures
// land on res.RecordErr / res.Warnings instead of stdout.
func (e *Executor) record(ctx context.Context, job Job, res *RunResult, runErr error, sec *SecurityOutcome) {
	status := "success"
	errorMessage := ""
	if runErr != nil {
		status = "failure"
		errorMessage = runErr.Error()
	}

	filePath, fileSize := ResolveArtifactRef(job.StorageType, job.LocalPath, job.OutName, job.GCSBucket)

	exec := &ports.Execution{
		BackupName:     job.Name,
		DatabaseType:   job.Engine,
		Timestamp:      res.StartedAt.UTC(),
		DurationMs:     res.FinishedAt.Sub(res.StartedAt).Milliseconds(),
		Status:         status,
		ErrorMessage:   errorMessage,
		StorageBackend: job.StorageType,
		FilePath:       filePath,
		FileSizeBytes:  fileSize,
	}

	meta := DeriveIncrementalContext(ctx, e.rec, job, fileSize)
	exec.BackupType = meta.BackupType
	exec.ChainID = meta.ChainID
	exec.ChainIndex = meta.ChainIndex
	exec.DeltaSizeBytes = meta.DeltaSizeBytes
	exec.FullBackupSizeBytes = meta.FullBackupSizeBytes
	res.Incremental = meta

	if sec != nil {
		if sec.BackupType != "" {
			exec.BackupType = sec.BackupType
		}
		if sec.ChainID != "" {
			exec.ChainID = sec.ChainID
		}
		exec.ChainIndex = sec.ChainIndex
		exec.DeltaSizeBytes = sec.DeltaSize
		exec.FullBackupSizeBytes = sec.FullSize
	}

	if e.rec == nil {
		return
	}
	if err := e.rec.RecordExecution(ctx, exec); err != nil {
		res.RecordErr = err
		return
	}

	if sec != nil && exec.ID != "" {
		if secErr := e.rec.RecordSecurityInfo(ctx, exec.ID, sec.HashAlgorithm, sec.HashValue, "", sec.ManifestPath, sec.Encrypted, sec.KeyHint); secErr != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Warning: failed to record security info for '%s': %v", job.Name, secErr))
		}
	}
}

// notify dispatches the per-job notification. Relocated from
// internal/cli/backup.go::notifyBackupResult (dispatch half); a nil
// Dispatcher means the job has no notification channels configured.
func (e *Executor) notify(job Job, res *RunResult, runErr error) {
	if e.notif == nil {
		return
	}

	status := ports.NotifyStatusSuccess
	errorMessage := ""
	if runErr != nil {
		status = ports.NotifyStatusFailure
		errorMessage = runErr.Error()
	}

	filePath, fileSize := LocalArtifactInfo(job.StorageType, job.LocalPath, job.OutName)
	bc := &ports.BackupContext{
		BackupName:   job.Name,
		DatabaseType: job.Engine,
		DatabaseName: job.Database,
		Status:       status,
		StartTime:    res.StartedAt,
		EndTime:      res.FinishedAt,
		Error:        errorMessage,
		FilePath:     filePath,
		FileSize:     fileSize,
	}

	if err := e.notif.Notify(bc); err != nil {
		res.NotifyErr = err
	}
}
