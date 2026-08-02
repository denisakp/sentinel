package mariadb

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// Builder satisfies ports.DumpBuilder by wrapping Backup. It carries an
// injected ports.DBProber for the pre-dump connectivity check (spec 043).
type Builder struct{ prober ports.DBProber }

// NewBuilder returns a Builder wired with the given connectivity prober.
func NewBuilder(prober ports.DBProber) *Builder { return &Builder{prober: prober} }

func (b *Builder) Build(ctx ports.BuildContext) (ports.BuildResult, error) {
	args, ok := ctx.Options.(*MariaDBDumpArgs)
	if !ok {
		return ports.BuildResult{}, fmt.Errorf("mariadb.Builder: expected *MariaDBDumpArgs, got %T", ctx.Options)
	}
	digest, err := Backup(b.prober, args)
	if err != nil {
		return ports.BuildResult{}, err
	}
	return ports.BuildResult{Digest: digest}, nil
}
