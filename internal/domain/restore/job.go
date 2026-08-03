package restore

// Job + result types for the restore Executor.
// Pure data: ports + sibling domain + stdlib only. Adapter-backed behavior
// (staging, preflight decrypt, engine arg construction, replay) reaches the
// Executor exclusively through function-valued hooks wired by the driving
// factory (internal/adapters/restore/runtime).

import (
	"context"
	"errors"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

// Sentinel errors (execution contract + relocated execution sentinels).
var (
	ErrRestoreLockConflict      = errors.New("restore execution lock conflict")
	ErrRestoreInterrupted       = errors.New("restore interrupted")
	ErrUnsupportedRestoreSource = errors.New("unsupported restore source")
	ErrSourceObjectNotFound     = errors.New("restore source object not found")
	ErrInsufficientStagingSpace = errors.New("insufficient staging space")
	ErrAmbiguousBackupID        = errors.New("ambiguous backup id")
	ErrHashMismatch             = errors.New("hash mismatch")

	ErrCascadeUnsafe    = errors.New("restore: postgres cascade safety gate failed")
	ErrChainBroken      = errors.New("restore: incremental chain integrity broken")
	ErrPITROutOfRange   = errors.New("restore: pitr timestamp outside chain coverage")
	ErrManifestNotFound = errors.New("restore: manifest not found at given ref")
)

// Mode enumerates the high-level restore modes.
type Mode string

const (
	ModeFull             Mode = "full"
	ModePITR             Mode = "pitr"
	ModeIncrementalChain Mode = "incremental"
)

// StagedArtifact describes one staged restore source on local disk.
// Relocated from internal/restore/source.go (pure data).
type StagedArtifact struct {
	Path         string
	ManifestPath string
	SourcePath   string
	SizeBytes    int64
	Retained     bool
}

// PlanRequest is the pure mirror of the driving layer's normalized advanced
// restore input (config.AdvancedRestoreRequest minus binlog target fields
// the planner does not consult).
type PlanRequest struct {
	RestoreMode           string
	PITRTimestampUTC      *time.Time
	PITRInputValue        string
	PITRTargetTimeline    string
	IncrementalFromBackup string
	ConfirmFullFallback   bool
}

// Job is the pure-data descriptor of one restore execution, plus the
// factory-wired hooks for adapter-backed steps.
type Job struct {
	Name     string
	Engine   string // postgres | mysql | mariadb | mongodb
	Database string

	// RestoreMode is the requested mode; Run updates it to the planned mode.
	RestoreMode      string
	SourceType       string
	ConflictStrategy string
	TimeoutSeconds   int
	KeepFile         bool
	StagingDir       string

	// BuildPlanRequest normalizes the advanced-restore input. Invoked after
	// staging (pre-carve error ordering: staging failures surface first).
	BuildPlanRequest func() (*PlanRequest, error)

	// StageSource downloads the primary backup (+ optional manifest) into
	// the staging dir.
	StageSource func(ctx context.Context) (*StagedArtifact, error)

	// StageChain stages every resolved chain artifact.
	StageChain func(ctx context.Context, backupIDs []string) ([]*StagedArtifact, error)

	// LoadPlanManifest loads the restore manifest backing planning;
	// it MUST return ports.ErrNoManifest when the sidecar is absent.
	LoadPlanManifest func(manifestPath string) (*ports.BackupManifest, error)

	// ReadManifest reads a staged chain manifest (binlog/oplog collection);
	// MUST return ports.ErrNoManifest when absent.
	ReadManifest func(manifestPath string) (*ports.BackupManifest, error)

	// Preflight verifies integrity and decrypts the staged artifact in
	// place, returning the effective staged path.
	Preflight func(ctx context.Context, artifact *StagedArtifact) (string, error)

	// RestoreOptions builds the engine-specific restore arg bag for the
	// staged path; handed verbatim to ports.RestoreBuilder.Build.
	RestoreOptions func(stagedPath string) (ports.RestoreOptions, error)

	// BinlogReplay replays MySQL/MariaDB binlog artifacts post-restore.
	BinlogReplay func(ctx context.Context, sources []string) error

	// OplogReplay replays one MongoDB oplog archive post-restore.
	OplogReplay func(ctx context.Context, archivePath string) error

	// Driving-supplied lifecycle hooks (pre-carve ExecutionRequest fields).
	ConflictEvaluator func(ctx context.Context, stagedPath string) error
	PostRestoreHook   func(ctx context.Context, stagedPath string) error
	VerifyAfterRun    func(ctx context.Context) (bool, error)

	// VerifyAfterRestore forces post-run verification even for full mode.
	VerifyAfterRestore bool
}

// RunResult is the outcome of Executor.Run. Field set preserved verbatim
// from internal/restore/executor.go::ExecutionResult.
type RunResult struct {
	ExecutionID          string
	Status               string
	Reason               string
	RestoreMode          string
	PlanningStatus       string
	RequestedPITRTimeUTC *time.Time
	BaselineBackupID     string
	FallbackDecision     string
	FallbackReason       string
	FallbackBackupID     string
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
	ChainDepth           int
	AssemblyDurationMs   int64
	Error                error
}
