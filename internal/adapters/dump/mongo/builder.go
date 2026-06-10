package mongo

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// Builder satisfies ports.DumpBuilder by wrapping Backup. Cleanup is nil:
// Mongo TLS material is cleaned internally via defer in Backup().
type Builder struct{}

func (Builder) Build(ctx ports.BuildContext) (ports.BuildResult, error) {
	args, ok := ctx.Options.(*DumpMongoArgs)
	if !ok {
		return ports.BuildResult{}, fmt.Errorf("mongo.Builder: expected *DumpMongoArgs, got %T", ctx.Options)
	}
	digest, err := Backup(args)
	if err != nil {
		return ports.BuildResult{}, err
	}
	return ports.BuildResult{Digest: digest}, nil
}
