// Package ports — restore axis port definitions.
//
// RestoreOptions is the marker interface every engine-specific *RestoreArgs
// (and Mongo's *OplogReplayArgs) implements. Post-spec-036 concrete types
// live at:
//   - *internal/adapters/restore/pg.RestoreArgs
//   - *internal/adapters/restore/mysql.RestoreArgs
//   - *internal/adapters/restore/mariadb.RestoreArgs
//   - *internal/adapters/restore/mongo.RestoreArgs
//   - *internal/adapters/restore/mongo.OplogReplayArgs
package ports

import (
	"context"
	"log/slog"
)

// RestoreOptions is the marker interface satisfied by every engine-specific
// *RestoreArgs (and *OplogReplayArgs) type. Adapters type-assert
// RestoreBuildContext.Options to recover the concrete shape. Forked from
// EngineOptions on the dump axis so the restore axis evolves independently.
type RestoreOptions interface {
	IsRestoreOptions()
}

// RestoreBuildContext carries the cross-cutting input shared by every
// engine's Build call. Engine-specific knobs travel inside Options as a
// *<Engine>RestoreArgs (or *OplogReplayArgs) value that the adapter
// type-asserts.
type RestoreBuildContext struct {
	Context context.Context // cancellation; nil → adapter may use context.Background()
	JobID   string          // for lock-key derivation, log correlation
	Logger  *slog.Logger    // structured logger; nil → adapter falls back to slog.Default()
	Options RestoreOptions  // engine-specific arg bag; non-nil; adapter type-asserts
}

// RestoreBuildResult is the engine-agnostic outcome of a restore. Empty
// today; restore produces no Digest/BytesWritten artifact. Reserved for
// future fields (PITR target ack, conflict-strategy outcome, oplog cursor).
type RestoreBuildResult struct{}

// RestoreBuilder is the unified port every restore engine adapter
// implements. Implementations live in internal/adapters/restore/<engine>/
// as zero-field Builder structs that wrap each engine's existing Restore().
type RestoreBuilder interface {
	Build(ctx RestoreBuildContext) (RestoreBuildResult, error)
}

// RestorePhase selects which arg shape a RestoreArgsFactory produces.
// Mongo is the only engine with a second phase.
type RestorePhase int

const (
	// PrimaryRestore builds the engine's main *RestoreArgs.
	PrimaryRestore RestorePhase = iota
	// OplogReplay builds Mongo's *OplogReplayArgs; non-Mongo engines error.
	OplogReplay
)

// RestoreJobSpec is the pure, engine-agnostic, carry-all descriptor handed to a
// RestoreArgsFactory. No YAML tags, no config/adapter
// dependency — so config no longer needs to import the restore adapter packages.
// Every factory input is already resolved here; factories are pure copiers.
type RestoreJobSpec struct {
	Engine         string // job type: "postgres" | "mysql" | "mariadb" | "mongodb"
	Host           string
	Port           int
	Username       string
	Password       string
	Database       string
	URI            string // mongo
	BackupPath     string // staged backup path (primary restore)
	ArchivePath    string // oplog archive path (mongo OplogReplay)
	OnConflict     string
	AllowCascade   bool // pg
	Gzip           bool // mongo
	Archive        bool // mongo
	AdditionalArgs string
}

// RestoreArgsFactory translates a RestoreJobSpec + phase into the engine-specific
// restore-args value (which already satisfies RestoreOptions). Implementations
// live in internal/adapters/restore/<engine>/ beside the engine's Builder.
type RestoreArgsFactory interface {
	BuildRestoreArgs(spec RestoreJobSpec, phase RestorePhase) (RestoreOptions, error)
}
