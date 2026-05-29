package pg

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// Builder satisfies ports.DumpBuilder by wrapping Backup.
type Builder struct{}

// Build executes a Postgres dump given a BuildContext whose Options must be
// a *PgDumpArgs. Returns BuildResult{Digest: <Backup result>} with a nil
// Cleanup — pg has no engine-specific post-dump cleanup.
func (Builder) Build(ctx ports.BuildContext) (ports.BuildResult, error) {
	args, ok := ctx.Options.(*PgDumpArgs)
	if !ok {
		return ports.BuildResult{}, fmt.Errorf("pg.Builder: expected *PgDumpArgs, got %T", ctx.Options)
	}
	digest, err := Backup(args)
	if err != nil {
		return ports.BuildResult{}, err
	}
	return ports.BuildResult{Digest: digest}, nil
}
