package ports

// EngineOptions abstracts the engine-specific args struct passed to a dump
// builder. Current concrete implementations:
//   - *pkg/backup/pg_dump.PgDumpArgs
//   - *pkg/backup/mysql_dump.MySqlDumpArgs
//   - *pkg/backup/mariadb_dump.MariaDBDumpArgs
//   - *pkg/backup/mongo_dump.DumpMongoArgs
//
// The interface is intentionally a marker (single empty method) rather than
// the originally planned unified `Build(BuildContext) (BuildResult, error)`
// surface. Rationale, documented as a deliberate deviation from the spec's
// dump signature unification (FR-005 escape hatch):
//
//   - The four current `argsBuilder` functions have heterogeneous signatures
//     (mongo returns extra *MongoTLSMaterial; pg accepts an extra
//     backupPath; three are unexported).
//   - Wrapping them behind a uniform `Build(BuildContext)` adds a thin layer
//     of indirection while every external caller still uses the engine's
//     `Backup()` entry point directly.
//   - Relocating `*DumpArgs` types into ports cascades into relocating
//     `storage.Params` (cross-cutting field) and `MongoTLSMaterial` (with
//     its Close()/Register()/cleanup-ring machinery) — multiplying the
//     diff well beyond the spec's "mechanical wrapping" estimate.
//   - The downstream dump-adapter spec (034) will revisit unification when
//     the dump packages move into `internal/adapters/dump/`.
//
// In this slice the port establishes the *boundary* — every engine's args
// type satisfies `EngineOptions` — without imposing a unified Build
// signature that the downstream spec is in a better position to design.
type EngineOptions interface {
	// IsEngineOptions is a marker method satisfied by every dump engine's
	// args struct. Exported (capital I) so types in foreign packages can
	// implement it; unexported interface methods are only satisfiable by
	// types in the same package as the interface.
	IsEngineOptions()
}
