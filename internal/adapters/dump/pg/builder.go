package pg

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// Builder satisfies ports.DumpBuilder by wrapping Backup. It carries an
// injected ports.DBProber for the pre-dump connectivity check.
type Builder struct{ prober ports.DBProber }

// NewBuilder returns a Builder wired with the given connectivity prober.
func NewBuilder(prober ports.DBProber) *Builder { return &Builder{prober: prober} }

// Build executes a Postgres dump given a BuildContext whose Options must be
// a *PgDumpArgs. Returns BuildResult{Digest: <Backup result>} with a nil
// Cleanup — pg has no engine-specific post-dump cleanup.
func (b *Builder) Build(ctx ports.BuildContext) (ports.BuildResult, error) {
	args, ok := ctx.Options.(*PgDumpArgs)
	if !ok {
		return ports.BuildResult{}, fmt.Errorf("pg.Builder: expected *PgDumpArgs, got %T", ctx.Options)
	}
	digest, err := Backup(buildCtx(ctx), b.prober, args)
	if err != nil {
		return ports.BuildResult{}, err
	}
	// argsBuilder mutates Storage.OutName in place to the full on-disk path
	// pg_dump wrote to (FullPath(backupPath, outName)); surface it so the
	// domain can hash/encrypt/manifest the artifact before a remote upload.
	return ports.BuildResult{Digest: digest, LocalPath: args.Storage.OutName}, nil
}

// buildCtx returns the cancellation context carried by the port, falling back to
// a background context when the caller supplied none.
//
// Every builder used to drop ctx.Context entirely and start the dump with
// exec.Command, so nothing could ever cancel a running dump. scheduler.
// job_timeout_minutes was therefore unenforceable however it was wired: a
// deadline that reaches no subprocess stops nothing (#194).
func buildCtx(ctx ports.BuildContext) context.Context {
	if ctx.Context != nil {
		return ctx.Context
	}
	return context.Background()
}
