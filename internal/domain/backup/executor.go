package backup

// Backup Executor. Owns the per-job backup
// critical path; every effect goes through the 9 injected ports (or a Job
// hook for adapter-backed steps a single port instance cannot express).
// Driving adapters (internal/cli/backup.go, internal/scheduler/) construct
// it via internal/cli/backup_factory.go and call Run.

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// Locks reports the lock manager the Executor will serialize this job with, or
// nil when nothing will.
//
// It exists so the composition root can be tested. The backup executor spent a
// release constructed with a nil LockManager under a comment claiming the
// scheduler owned serialization, while the scheduler's lock helpers had no
// callers at all (#163). Nothing could observe that from outside the package,
// which is why it survived: the wiring was wrong in a way no test could reach.
func (e *Executor) Locks() ports.LockManager { return e.locks }

// NewExecutor constructs an Executor over the 9 architectural ports.
// Ports not exercised by a given configuration may be nil; the
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

	// Remote staging cleanup. When the factory redirected a remote
	// dump to a local staging dir, remove it on EVERY exit path — success or
	// failure — so no plaintext/ciphertext artifact is ever left behind.
	// Registered first so it runs last (after record() has stat'd the artifact).
	if job.StagingDir != "" {
		defer func() { _ = os.RemoveAll(job.StagingDir) }()
	}

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
			e.record(ctx, job, &res, runErr, nil, "")
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

	// 6. Post-dump pipeline (manifest, encryption, incremental artifacts). For
	// remote storage this runs on the staged local artifact (build.LocalPath)
	// so the artifact is hashed + encrypted BEFORE it leaves the host.
	var sec *SecurityOutcome
	if runErr == nil {
		outcome, warnings, secErr := e.ApplyArtifactSecurity(ctx, job, build.Digest, build.LocalPath)
		res.Warnings = append(res.Warnings, warnings...)
		if secErr != nil {
			runErr = fmt.Errorf("backup '%s': security processing failed: %w", job.Name, secErr)
		} else {
			sec = outcome
		}
	}

	// 7. Remote upload. The Executor owns the upload of the staged,
	// now-encrypted artifact + manifest sidecar. Fail-loud: if an encryption
	// key was configured but the artifact was NOT encrypted, refuse to upload
	// plaintext.
	if runErr == nil && e.isRemote(job) && build.LocalPath != "" {
		runErr = e.uploadStagedArtifact(ctx, job, build.LocalPath, sec, &res)
	}

	// 7.5. Post-upload verify (opt-in): re-download the
	// artifact from the injected StorageBackend and re-hash it against the
	// manifest hash, failing the backup on mismatch rather than letting a
	// storage-side corruption surface at the next restore. A no-op unless
	// both the job enables it AND a storage port was wired (the factory only
	// wires one for local storage when this is explicitly on; for staged
	// remote uploads the same backend used to upload is reused).
	if runErr == nil && job.VerifyAfterUpload && e.storage != nil {
		if verifyErr := e.verifyAfterUpload(ctx, job, sec); verifyErr != nil {
			runErr = verifyErr
		}
	}

	res.FinishedAt = time.Now()
	res.ArtifactPath, res.ArtifactSize = ResolveArtifactRef(job.StorageType, job.LocalPath, job.OutName, job.GCSBucket, build.LocalPath)
	if sec != nil {
		res.ManifestPath = sec.ManifestPath
		res.Encrypted = sec.Encrypted
		res.HashValue = sec.HashValue
	}

	// 8. Record history. 9. Notify. (Retention sweep — contract step 7 —
	// stays in the driving adapter; see progress.md Sub-PR K notes.)
	e.record(ctx, job, &res, runErr, sec, build.LocalPath)
	e.notify(job, &res, runErr)

	return res, runErr
}

// isRemote reports whether the job targets a non-local storage backend.
func (e *Executor) isRemote(job Job) bool {
	return job.StorageType != "" && job.StorageType != "local"
}

// uploadStagedArtifact uploads the staged (encrypted) artifact and its manifest
// sidecar to the injected remote StorageBackend. It fails loud when encryption
// was configured but not applied — never uploading plaintext. A nil backend
// (unit tests / non-stageable paths) is a no-op. Manifest-sidecar upload
// failures are non-fatal (surfaced through res.Warnings).
func (e *Executor) uploadStagedArtifact(ctx context.Context, job Job, stagedPath string, sec *SecurityOutcome, res *RunResult) error {
	if job.EncryptArtifact != nil && (sec == nil || !sec.Encrypted) {
		return fmt.Errorf("backup '%s': encryption key configured but artifact was not encrypted; refusing to upload plaintext", job.Name)
	}
	if e.storage == nil {
		return nil
	}

	if err := e.storage.Upload(ctx, stagedPath, job.OutName); err != nil {
		return fmt.Errorf("backup '%s': failed to upload artifact to %s: %w", job.Name, job.StorageType, err)
	}

	if sec != nil && sec.ManifestPath != "" {
		if err := e.storage.Upload(ctx, sec.ManifestPath, job.OutName+".manifest.json"); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Warning: failed to upload manifest sidecar for '%s': %v", job.Name, err))
		}
	}

	return nil
}

// verifyAfterUpload re-downloads job.OutName from e.storage into a temp file
// and re-hashes it against sec.HashValue via ports.ManifestStore.VerifyHash,
// returning an error tagged "verify_after_upload_failed" on mismatch (Q3:
// the job fails and the stored object is left in place — never deleted —
// for forensics; its path is included in the error). The temp file is always
// removed, on both the match and mismatch paths.
//
// When no baseline hash exists for this artifact (sec == nil ||
// sec.HashValue == "", e.g. a remote auto-discovery "single" dump-all job
// that uploads without staging and therefore has no manifest — see
// ApplyArtifactSecurity's remote/no-staging early return) there is nothing
// to compare against, so this degrades to a no-op rather than a false
// failure. Likewise a nil ManifestStore (never wired by the production
// factory) degrades to a no-op.
func (e *Executor) verifyAfterUpload(ctx context.Context, job Job, sec *SecurityOutcome) error {
	if sec == nil || sec.HashValue == "" || job.OutName == "" || e.manifests == nil {
		return nil
	}

	tmp, err := os.CreateTemp("", "sentinel-verify-*")
	if err != nil {
		return fmt.Errorf("backup '%s': verify_after_upload: failed to create temp file: %w", job.Name, err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := e.storage.Download(ctx, job.OutName, tmpPath); err != nil {
		return fmt.Errorf("backup '%s': verify_after_upload: re-download of %q failed: %w", job.Name, job.OutName, err)
	}

	algo := sec.HashAlgorithm
	if algo == "" {
		algo = "sha256"
	}
	if verifyErr := e.manifests.VerifyHash(tmpPath, algo, sec.HashValue); verifyErr != nil {
		return fmt.Errorf("backup '%s': verify_after_upload_failed: %w (stored object left in place: %s)", job.Name, verifyErr, job.OutName)
	}

	return nil
}

// record persists the execution history row (+ security info). Relocated
// from internal/cli/backup.go::recordBackupExecution; persistence failures
// land on res.RecordErr / res.Warnings instead of stdout.
func (e *Executor) record(ctx context.Context, job Job, res *RunResult, runErr error, sec *SecurityOutcome, stagedPath string) {
	status := "success"
	errorMessage := ""
	if runErr != nil {
		status = "failure"
		errorMessage = runErr.Error()
	}

	filePath, fileSize := ResolveArtifactRef(job.StorageType, job.LocalPath, job.OutName, job.GCSBucket, stagedPath)

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
