// Package ports — dump axis port definitions (spec 028 T028-T031, spec 035).
//
// EngineOptions is the marker interface every engine-specific *DumpArgs
// implements. Post-spec-035 concrete types live at:
//   - *internal/adapters/dump/pg.PgDumpArgs
//   - *internal/adapters/dump/mysql.MySqlDumpArgs
//   - *internal/adapters/dump/mariadb.MariaDBDumpArgs
//   - *internal/adapters/dump/mongo.DumpMongoArgs
//
// Plus DumpAll variants for pg/mysql/mariadb (no DumpAll for mongo).
package ports

import (
	"context"
	"log/slog"
)

// EngineOptions is the marker interface satisfied by every engine-specific
// *DumpArgs type. Adapters type-assert BuildContext.Options to recover the
// concrete shape.
type EngineOptions interface {
	// IsEngineOptions is a marker method satisfied by every dump engine's
	// args struct. Exported so types in foreign packages can implement it.
	IsEngineOptions()
}

// DumpCleanup is the post-dump cleanup handle returned (optionally) by
// DumpBuilder.Build inside BuildResult.Cleanup. nil for engines that have
// nothing to clean — including every engine today, because cleanup is
// performed internally via defer in each adapter's Backup() function. The
// field exists as the extension point for a future spec that splits
// argument building from command execution.
type DumpCleanup interface {
	Cleanup() error
}

// BuildContext carries the cross-cutting input shared by every engine's
// Build call. Engine-specific knobs travel inside Options as a
// *<Engine>DumpArgs value that the adapter type-asserts.
//
// Storage is intentionally not duplicated here: each *DumpArgs already
// embeds *storage.Params, and lifting storage.Params into ports would
// create an import cycle (adapters/storage imports ports). The adapter
// reads storage parameters from Options.(*<Engine>DumpArgs).Storage.
type BuildContext struct {
	Context context.Context // cancellation; nil → adapter may use context.Background()
	JobID   string          // for Mongo PEM filenames, lock-key derivation, log correlation
	Logger  *slog.Logger    // structured logger; nil → adapter falls back to slog.Default()
	Options EngineOptions   // engine-specific arg bag; non-nil; adapter type-asserts
}

// BuildResult is the engine-agnostic outcome of a dump.
type BuildResult struct {
	Digest       string      // sha256 hex of dump bytes when available; "" for remote-staged dumps
	BytesWritten int64       // size of produced artifact when known; 0 if not measured
	Cleanup      DumpCleanup // post-dump cleanup handle; nil today for every engine
	// LocalPath is the on-disk path of the produced artifact, always populated
	// (local storage: the final destination; remote storage: a staging file the
	// caller must upload then clean up). Enables the domain to hash/encrypt/
	// manifest the artifact uniformly before upload (spec 047).
	LocalPath string
}

// DumpBuilder is the unified port every dump engine adapter implements.
// Implementations live in internal/adapters/dump/<engine>/ as zero-field
// Builder structs that wrap each engine's existing Backup() function.
type DumpBuilder interface {
	Build(ctx BuildContext) (BuildResult, error)
}

// DumpJobSpec is the engine-agnostic, pure descriptor handed to a
// DumpArgsFactory (spec 040 / PRD 27). It carries only already-resolved values
// — no YAML tags, no config/adapter/storage dependency — so config no longer
// needs to import the dump adapter packages. Storage destination params are
// intentionally NOT here: the command layer sets the concrete *DumpArgs.Storage
// field on the value returned by the factory.
type DumpJobSpec struct {
	Engine               string // job type: "postgres" | "mysql" | "mariadb" | "mongodb"
	Host                 string
	Port                 string // already formatted by config (empty when port is 0)
	Username             string
	Password             string
	Database             string
	URI                  string // mongo
	AdditionalArgs       string
	PgOutFormat          string // pg
	Compress             bool   // pg + mongo
	CompressionAlgorithm string // pg
	CompressionLevel     int    // pg
}

// DumpArgsFactory translates a DumpJobSpec into the engine-specific dump-args
// value (which already satisfies EngineOptions). Implementations live in
// internal/adapters/dump/<engine>/ beside the engine's Builder, keeping the
// engine's argument surface a single source of truth (spec 040 / PRD 27).
type DumpArgsFactory interface {
	BuildDumpArgs(spec DumpJobSpec) (EngineOptions, error)
}
