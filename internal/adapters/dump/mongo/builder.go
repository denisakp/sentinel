package mongo

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/denisakp/sentinel/internal/ports"
)

// Builder satisfies ports.DumpBuilder by wrapping Backup. It carries an
// injected ports.DBProber for the pre-dump connectivity check.
// Cleanup is nil: Mongo TLS material is cleaned internally via defer in Backup().
type Builder struct{ prober ports.DBProber }

// NewBuilder returns a Builder wired with the given connectivity prober.
func NewBuilder(prober ports.DBProber) *Builder { return &Builder{prober: prober} }

func (b *Builder) Build(ctx ports.BuildContext) (ports.BuildResult, error) {
	args, ok := ctx.Options.(*DumpMongoArgs)
	if !ok {
		return ports.BuildResult{}, fmt.Errorf("mongo.Builder: expected *DumpMongoArgs, got %T", ctx.Options)
	}
	digest, err := Backup(b.prober, args)
	if err != nil {
		return ports.BuildResult{}, err
	}
	return ports.BuildResult{Digest: digest, LocalPath: mongoLocalPath(args)}, nil
}

// mongoLocalPath returns the on-disk artifact path for the domain pipeline.
// For the executor-owned remote path it is the staged archive
// inside RemoteStagingDir; otherwise it is the (local) dump destination Backup
// resolved into Storage.OutName.
func mongoLocalPath(args *DumpMongoArgs) string {
	if strings.TrimSpace(args.RemoteStagingDir) != "" {
		return filepath.Join(args.RemoteStagingDir, archiveFileName(args.Compress))
	}
	if args.Storage != nil {
		return args.Storage.OutName
	}
	return ""
}
