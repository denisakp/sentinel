package pg

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time conformance assertions (spec 035 FR-013).
var (
	_ ports.DumpBuilder   = (*Builder)(nil)
	_ ports.EngineOptions = (*PgDumpArgs)(nil)
)
