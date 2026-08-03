package mongo

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertions wiring this adapter to the restore axis port
// surface declared in internal/ports/restore.go.
// OplogReplayArgs satisfies RestoreOptions alongside RestoreArgs because the
// mongo Builder.Build (and ArgsFactory OplogReplay phase) dispatches on either.
var (
	_ ports.RestoreBuilder     = (*Builder)(nil)
	_ ports.RestoreOptions     = (*RestoreArgs)(nil)
	_ ports.RestoreOptions     = (*OplogReplayArgs)(nil)
	_ ports.RestoreArgsFactory = ArgsFactory{}
)
