package mysql

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// Builder satisfies ports.DumpBuilder by wrapping Backup.
type Builder struct{}

func (Builder) Build(ctx ports.BuildContext) (ports.BuildResult, error) {
	args, ok := ctx.Options.(*MySqlDumpArgs)
	if !ok {
		return ports.BuildResult{}, fmt.Errorf("mysql.Builder: expected *MySqlDumpArgs, got %T", ctx.Options)
	}
	digest, err := Backup(args)
	if err != nil {
		return ports.BuildResult{}, err
	}
	return ports.BuildResult{Digest: digest}, nil
}
