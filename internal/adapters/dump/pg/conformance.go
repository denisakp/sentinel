package pg

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time conformance assertions.
var (
	_ ports.DumpBuilder     = (*Builder)(nil)
	_ ports.EngineOptions   = (*PgDumpArgs)(nil)
	_ ports.DumpArgsFactory = ArgsFactory{}
)
