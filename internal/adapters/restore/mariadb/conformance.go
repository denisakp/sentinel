package mariadb

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertions wiring this adapter to the restore axis port
// surface declared in internal/ports/restore.go.
var (
	_ ports.RestoreBuilder     = (*Builder)(nil)
	_ ports.RestoreOptions     = (*RestoreArgs)(nil)
	_ ports.RestoreArgsFactory = ArgsFactory{}
)
