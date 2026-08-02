package pg

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertions wiring this adapter to the restore axis port
// surface declared in internal/ports/restore.go (spec 036 + spec 041).
var (
	_ ports.RestoreBuilder     = (*Builder)(nil)
	_ ports.RestoreOptions     = (*RestoreArgs)(nil)
	_ ports.RestoreArgsFactory = ArgsFactory{}
)
