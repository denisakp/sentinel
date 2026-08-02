package runtime

// Restore execution entry point + the single domain restore.Executor
// construction site (spec 038 Sub-PR L, FR-011 / SC-006). ExecuteRestore
// keeps the pre-carve internal/restore.ExecuteRestore signature; the
// orchestration control flow now lives in
// internal/domain/restore.Executor.Run, fed through Job hooks built here.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/denisakp/sentinel/internal/adapters/compress"
	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/denisakp/sentinel/internal/adapters/lock"
	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	restoreregistry "github.com/denisakp/sentinel/internal/adapters/restore"
	chainassembler "github.com/denisakp/sentinel/internal/adapters/restore/chain_assembler"
	"github.com/denisakp/sentinel/internal/adapters/restore/incremental/mysqlbinlog"
	mariadbrestore "github.com/denisakp/sentinel/internal/adapters/restore/mariadb"
	mongorestore "github.com/denisakp/sentinel/internal/adapters/restore/mongo"
	mysqlrestore "github.com/denisakp/sentinel/internal/adapters/restore/mysql"
	pgrestore "github.com/denisakp/sentinel/internal/adapters/restore/pg"
	"github.com/denisakp/sentinel/internal/config"
	domainrestore "github.com/denisakp/sentinel/internal/domain/restore"
	"github.com/denisakp/sentinel/internal/ports"
)

// Error aliases over the relocated domain sentinels.
var (
	ErrRestoreLockConflict = domainrestore.ErrRestoreLockConflict
	ErrRestoreInterrupted  = domainrestore.ErrRestoreInterrupted
)

// Test seams, preserved from internal/restore/executor.go.
var (
	stageRestoreSource    = StageRestoreSource
	stageChainArtifacts   = StageChainArtifacts
	applyRestorePreflight = applyPreflight
	executeRestoreEngine  = executeEngineRestore
	runPostgresRestore    = pgrestore.Restore
	runPostgresPITR       = executePostgresPITR
	runMySQLBinlogReplay  = mysqlbinlog.Replay
	runMongoOplogReplay   = mongorestore.ReplayOplog
	assemblePostgresChain = chainassembler.NewAdapter().AssemblePostgresChain
)

// ExecutionRequest is the pre-carve restore execution input (verbatim from
// internal/restore/executor.go).
type ExecutionRequest struct {
	JobName           string
	Job               config.RestoreJob
	Config            *config.Configuration
	LockDir           string
	Monitor           ports.Recorder
	VerifyAfterRun    func(context.Context, config.RestoreJob) (bool, error)
	PostRestoreHook   func(context.Context, config.RestoreJob, string) error
	ConflictEvaluator func(context.Context, config.RestoreJob, string) error
	// AllowLegacyEnvelope opt-in for decrypting pre-v2 artifacts. Off by default.
	AllowLegacyEnvelope bool
	// SkipHashVerify downgrades a manifest hash mismatch from a hard abort to a
	// logged WARNING. Off by default; set only by the per-invocation
	// --skip-hash-verify restore flag (never env-defaulted).
	SkipHashVerify bool
}

// ExecutionResult preserves the pre-carve result shape (now the domain
// RunResult, relocated).
type ExecutionResult = domainrestore.RunResult

// stagedPathOptions carries the staged path through the RestoreBuilder port
// to the engine dispatch closure below.
type stagedPathOptions struct{ path string }

func (stagedPathOptions) IsRestoreOptions() {}

// restoreBuilderFunc adapts a closure to ports.RestoreBuilder.
type restoreBuilderFunc func(bc ports.RestoreBuildContext) (ports.RestoreBuildResult, error)

func (f restoreBuilderFunc) Build(bc ports.RestoreBuildContext) (ports.RestoreBuildResult, error) {
	return f(bc)
}

// chainAssemblerFunc adapts the assemblePostgresChain seam to
// ports.ChainAssembler.
type chainAssemblerFunc func(ctx context.Context, stagingDir string, stagedSources []string, toolsPath string) (string, error)

func (f chainAssemblerFunc) AssemblePostgresChain(ctx context.Context, stagingDir string, stagedSources []string, toolsPath string) (string, error) {
	return f(ctx, stagingDir, stagedSources, toolsPath)
}

// observingRestoreRecorder feeds the incremental metrics observer before
// persisting through the wrapped recorder (pre-carve recordRestoreExecution
// behavior, including the WARN-and-continue on persistence failure).
type observingRestoreRecorder struct {
	ports.Recorder
	inner   ports.Recorder
	jobName string
}

func (o *observingRestoreRecorder) RecordRestoreExecution(ctx context.Context, entry *ports.RestoreExecution) error {
	monitor.ObserveIncrementalRestore(o.jobName, entry)
	if o.inner == nil {
		return nil
	}
	if err := o.inner.RecordRestoreExecution(ctx, entry); err != nil {
		slog.Error("failed to record restore execution",
			"event", "monitor_record_restore_failed",
			"job", o.jobName,
			"error", err.Error(),
		)
	}
	return nil
}

// ExecuteRestore validates the request, constructs the domain Executor (the
// single construction site) and runs it.
func ExecuteRestore(ctx context.Context, req *ExecutionRequest) (*ExecutionResult, error) {
	if req == nil {
		return nil, fmt.Errorf("restore request is required")
	}
	job := req.Job
	job.Name = req.JobName
	if err := config.ValidateRestoreJob(&job); err != nil {
		return nil, err
	}

	exec, djob := NewRestoreExecutorFromConfig(req, job)
	return exec.Run(ctx, djob)
}

// NewRestoreExecutorFromConfig constructs the domain restore Executor + Job
// for one validated request. ALL restore call sites (CLI run, scheduler
// integration, scheduler executor) flow through here (FR-011).
func NewRestoreExecutorFromConfig(req *ExecutionRequest, job config.RestoreJob) (*domainrestore.Executor, domainrestore.Job) {
	var locks ports.LockManager
	if req.LockDir != "" {
		locks = lock.NewManager(req.LockDir)
	}

	rec := &observingRestoreRecorder{inner: req.Monitor, jobName: req.JobName}

	restores := restoreBuilderFunc(func(bc ports.RestoreBuildContext) (ports.RestoreBuildResult, error) {
		opts, ok := bc.Options.(stagedPathOptions)
		if !ok {
			return ports.RestoreBuildResult{}, fmt.Errorf("runtime: expected stagedPathOptions, got %T", bc.Options)
		}
		return ports.RestoreBuildResult{}, executeRestoreEngine(bc.Context, job, opts.path)
	})

	exec := domainrestore.NewExecutor(
		restores,
		nil, // ports.StorageBackend — staging goes through the StageSource/StageChain hooks (multi-backend dispatch)
		nil, // ports.DecryptReader — per-artifact decryption goes through the Preflight hook
		nil, // ports.Hasher — hash verification happens inside the Preflight hook
		rec,
		nil, // ports.Dispatcher — restore notifications stay caller-side (scheduler RestoreScheduleManager)
		locks,
		nil, // ports.DBProber — cascade gate handled by the ConflictEvaluator hook today
		chainAssemblerFunc(func(ctx context.Context, stagingDir string, sources []string, toolsPath string) (string, error) {
			return assemblePostgresChain(ctx, stagingDir, sources, toolsPath)
		}),
	)

	djob := domainrestore.Job{
		Name:               req.JobName,
		Engine:             job.Type,
		Database:           job.Database,
		RestoreMode:        job.RestoreMode,
		SourceType:         job.BackupSource.Type,
		ConflictStrategy:   effectiveConflictStrategy(job),
		TimeoutSeconds:     job.TimeoutSeconds,
		KeepFile:           job.KeepFile,
		StagingDir:         job.StagingDir,
		VerifyAfterRestore: job.VerifyAfterRestore,

		StageSource: func(ctx context.Context) (*domainrestore.StagedArtifact, error) {
			return stageRestoreSource(ctx, job)
		},
		StageChain: func(ctx context.Context, backupIDs []string) ([]*domainrestore.StagedArtifact, error) {
			return stageChainArtifacts(ctx, job, backupIDs)
		},
		BuildPlanRequest: func() (*domainrestore.PlanRequest, error) {
			request, err := config.BuildAdvancedRestoreRequest(job)
			if err != nil {
				return nil, err
			}
			return toPlanRequest(request), nil
		},
		LoadPlanManifest: manifest.LoadRestoreManifest,
		ReadManifest:     manifest.ReadManifest,
		Preflight: func(ctx context.Context, artifact *domainrestore.StagedArtifact) (string, error) {
			return applyRestorePreflight(ctx, req.Config, artifact, req.AllowLegacyEnvelope, req.SkipHashVerify)
		},
		RestoreOptions: func(stagedPath string) (ports.RestoreOptions, error) {
			return stagedPathOptions{path: stagedPath}, nil
		},
		BinlogReplay: func(ctx context.Context, sources []string) error {
			password, err := config.RestorePasswordFromEnv(job.PasswordEnv)
			if err != nil {
				return err
			}
			replayArgs, err := config.BuildMySQLBinlogReplayArgs(job, password, sources)
			if err != nil {
				return err
			}
			return runMySQLBinlogReplay(ctx, replayArgs)
		},
		OplogReplay: func(ctx context.Context, archivePath string) error {
			spec := config.BuildRestoreJobSpec(job, "", "", archivePath)
			opts, err := mongorestore.ArgsFactory{}.BuildRestoreArgs(spec, ports.OplogReplay)
			if err != nil {
				return err
			}
			return runMongoOplogReplay(ctx, opts.(*mongorestore.OplogReplayArgs))
		},
	}
	// Note: the engine-dispatch closure captures the pre-plan job. The only
	// mode-sensitive routing is postgres "pitr", which planning never
	// introduces (it can only downgrade incremental → full), so the captured
	// RestoreMode routes identically to the planned one.
	if req.ConflictEvaluator != nil {
		evaluator := req.ConflictEvaluator
		djob.ConflictEvaluator = func(ctx context.Context, stagedPath string) error {
			return evaluator(ctx, job, stagedPath)
		}
	}
	if req.PostRestoreHook != nil {
		hook := req.PostRestoreHook
		djob.PostRestoreHook = func(ctx context.Context, stagedPath string) error {
			return hook(ctx, job, stagedPath)
		}
	}
	if req.VerifyAfterRun != nil {
		verify := req.VerifyAfterRun
		djob.VerifyAfterRun = func(ctx context.Context) (bool, error) {
			return verify(ctx, job)
		}
	}

	return exec, djob
}

// executeEngineRestore dispatches to the engine adapter. Relocated verbatim
// from internal/restore/executor.go (test seams preserved); reached only
// through the domain Executor's ports.RestoreBuilder.
func executeEngineRestore(ctx context.Context, job config.RestoreJob, stagedPath string) error {
	password, err := config.RestorePasswordFromEnv(job.PasswordEnv)
	if err != nil {
		return err
	}

	if job.Type == "postgres" && job.RestoreMode == "pitr" {
		return runPostgresPITR(ctx, job, password, stagedPath)
	}

	spec := config.BuildRestoreJobSpec(job, password, stagedPath, "")
	factory, err := restoreregistry.NewArgsFactory(job.Type)
	if err != nil {
		return fmt.Errorf("unsupported restore type: %s", job.Type)
	}
	opts, err := factory.BuildRestoreArgs(spec, ports.PrimaryRestore)
	if err != nil {
		return err
	}

	switch job.Type {
	case "postgres":
		return runPostgresRestore(ctx, opts.(*pgrestore.RestoreArgs))
	case "mysql":
		return mysqlrestore.Restore(ctx, opts.(*mysqlrestore.RestoreArgs))
	case "mariadb":
		return mariadbrestore.Restore(ctx, opts.(*mariadbrestore.RestoreArgs))
	case "mongodb":
		return mongorestore.Restore(ctx, opts.(*mongorestore.RestoreArgs))
	default:
		return fmt.Errorf("unsupported restore type: %s", job.Type)
	}
}

// executePostgresPITR runs PostgreSQL restore orchestration for PITR
// requests. Relocated from internal/restore/postgres_pitr.go.
func executePostgresPITR(ctx context.Context, job config.RestoreJob, password, stagedPath string) error {
	spec := config.BuildRestoreJobSpec(job, password, stagedPath, "")
	opts, err := pgrestore.ArgsFactory{}.BuildRestoreArgs(spec, ports.PrimaryRestore)
	if err != nil {
		return fmt.Errorf("failed to build postgres PITR restore args: %w", err)
	}
	args := opts.(*pgrestore.RestoreArgs)
	if err := pgrestore.Restore(ctx, args); err != nil {
		return fmt.Errorf("failed to execute postgres PITR restore: %w", err)
	}
	return nil
}

// applyPreflight verifies integrity and decrypts the staged artifact in
// place. Relocated verbatim from internal/restore/executor.go.
func applyPreflight(ctx context.Context, cfg *config.Configuration, artifact *StagedArtifact, allowLegacyEnvelope, skipHashVerify bool) (string, error) {
	if artifact == nil {
		return "", fmt.Errorf("staged artifact is required")
	}
	if artifact.ManifestPath == "" {
		return artifact.Path, nil
	}

	m, err := manifest.ReadManifest(artifact.ManifestPath)
	if err != nil {
		if errors.Is(err, ports.ErrNoManifest) {
			return artifact.Path, nil
		}
		return "", err
	}

	var keyProvider ports.KeyProvider
	if cfg != nil && (cfg.EncryptionKeyEnv != "" || cfg.EncryptionKeyFile != "") {
		keyProvider = &crypto.FileKeyProvider{EnvVar: cfg.EncryptionKeyEnv, FilePath: cfg.EncryptionKeyFile}
	}

	reader, err := PreRestoreVerifyAndDecryptWithOptions(ctx, m, artifact.Path, keyProvider, ports.DecryptOptions{
		AllowLegacy:    allowLegacyEnvelope,
		SkipHashVerify: skipHashVerify,
		Source:         artifact.Path,
		BackupID:       m.BackupID,
	})
	if err != nil {
		if errors.Is(err, ErrHashMismatch) {
			return "", fmt.Errorf("integrity_check_failed: %w", err)
		}
		return "", err
	}

	// Decompress stage (spec 049 / PRD 33): inserted AFTER decrypt, driven by
	// the manifest (no operator flag). A manifest without a compression block
	// (legacy backups, native-compressed PG/Mongo dumps) skips this entirely,
	// so existing backups restore byte-for-byte unchanged.
	if m.Compression != nil && m.Compression.Algorithm != "" && m.Compression.Algorithm != "none" {
		dr, derr := compress.NewDecompressReader(reader, m.Compression.Algorithm)
		if derr != nil {
			return "", fmt.Errorf("failed to initialise decompressor for %q: %w", artifact.Path, derr)
		}
		defer dr.Close()
		reader = dr
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

func effectiveConflictStrategy(job config.RestoreJob) string {
	if job.ConflictStrategy == "" {
		return "error"
	}
	return job.ConflictStrategy
}
