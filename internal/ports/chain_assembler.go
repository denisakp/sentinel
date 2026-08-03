package ports

import "context"

// ChainAssembler combines a staged base + incremental sources into a single
// assembled artifact suitable for engine restore.
//
// Spec 038 — carved from internal/restore/incremental/assembler.go to
// preserve the "domain reaches adapters only through ports" rule (ADR 0001).
// Today only AssemblePostgresChain ships; sibling methods
// (AssembleMysqlChain, AssembleMongoChain) MAY join the interface later as
// additive expansions when those engines need cross-source assembly.
type ChainAssembler interface {
	// AssemblePostgresChain takes the staging directory, the ordered list of
	// staged source directories (base first, then incrementals), and the
	// optional toolsPath override (empty = use PATH). Returns the absolute
	// path to the assembled output directory, or an error.
	//
	// The implementation MUST clean up the output directory on error.
	AssemblePostgresChain(ctx context.Context, stagingDir string, stagedSources []string, toolsPath string) (string, error)
}
