package mongo

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertions wiring this adapter to the restore axis port
// surface declared in internal/ports/restore.go (spec 036). OplogReplayArgs
// satisfies RestoreOptions alongside RestoreArgs because the mongo
// Builder.Build dispatches on either type.
var (
	_ ports.RestoreBuilder = (*Builder)(nil)
	_ ports.RestoreOptions = (*RestoreArgs)(nil)
	_ ports.RestoreOptions = (*OplogReplayArgs)(nil)
)
