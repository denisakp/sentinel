package ports

import (
	"context"
	"errors"
)

// DBProber probes a live database for connectivity + capability + safety
// information. It is the carve-out for real I/O previously embedded in
// internal/backup/sql/* and internal/restore/postgres_conflicts.go (spec 037).
//
// Implementations: internal/adapters/db_probe/Adapter.
type DBProber interface {
	// Ping returns nil if the prober can establish a connection to the
	// configured database, otherwise an error.
	Ping(ctx context.Context, conn DatabaseConfig) error

	// ListDatabases enumerates databases visible to the configured user.
	// Empty result is valid.
	ListDatabases(ctx context.Context, conn DatabaseConfig) ([]string, error)

	// AssessPostgresCascadeSafety inspects the target Postgres database for
	// objects whose presence would cause a CASCADE drop / restore conflict.
	// Non-PG engines MUST return ErrUnsupportedEngine.
	//
	// AllowCascade is consulted by the prober only for shaping the result's
	// BlockingReason — the caller still decides whether to proceed based on
	// the returned SafeToProceed flag.
	AssessPostgresCascadeSafety(ctx context.Context, conn DatabaseConfig, target CascadeTarget) (*CascadeSafetyResult, error)
}

// CascadeTarget carries the per-job options relevant to a cascade-safety
// assessment. Kept separate from DatabaseConfig so the connection descriptor
// remains reusable across Ping/ListDatabases/AssessPostgresCascadeSafety.
type CascadeTarget struct {
	// ConflictStrategy maps to RestoreJob.ConflictStrategy. Cascade-safety
	// only runs when ConflictStrategy == "replace".
	ConflictStrategy string

	// AllowCascade lets a caller pre-authorize CASCADE drops; when true and
	// dependents exist, SafeToProceed remains true.
	AllowCascade bool
}

// CascadeSafetyResult is returned by AssessPostgresCascadeSafety.
type CascadeSafetyResult struct {
	// SafeToProceed is true when no dependents exist OR when AllowCascade was
	// set in the request.
	SafeToProceed bool

	// DependentObjects lists objects (view/matview/index) that depend on user
	// tables and would be dropped by CASCADE.
	DependentObjects []string

	// BlockingReason is populated when SafeToProceed is false; empty otherwise.
	BlockingReason string
}

// ErrUnsupportedEngine is returned by engine-specific methods when called
// against an engine they do not support (e.g., AssessPostgresCascadeSafety
// on a MySQL connection).
var ErrUnsupportedEngine = errors.New("db_probe: unsupported engine for this operation")
