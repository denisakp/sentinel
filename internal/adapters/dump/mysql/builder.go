package mysql

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/utils"
)

// Builder satisfies ports.DumpBuilder by wrapping Backup. It carries an
// injected ports.DBProber for the pre-dump connectivity check.
type Builder struct{ prober ports.DBProber }

// NewBuilder returns a Builder wired with the given connectivity prober.
func NewBuilder(prober ports.DBProber) *Builder { return &Builder{prober: prober} }

func (b *Builder) Build(ctx ports.BuildContext) (ports.BuildResult, error) {
	args, ok := ctx.Options.(*MySqlDumpArgs)
	if !ok {
		return ports.BuildResult{}, fmt.Errorf("mysql.Builder: expected *MySqlDumpArgs, got %T", ctx.Options)
	}
	digest, err := Backup(b.prober, args)
	if err != nil {
		return ports.BuildResult{}, err
	}
	return ports.BuildResult{Digest: digest, LocalPath: localArtifactPath(args.Storage)}, nil
}

// localArtifactPath reconstructs the on-disk path Backup wrote to using the
// same helpers the adapter uses (Backup leaves Storage.OutName finalized but
// stores the full path only in a local variable). Returns "" when the storage
// handler cannot be resolved. Enables the domain to hash/encrypt/manifest the
// artifact before a remote upload.
func localArtifactPath(p *storage.Params) string {
	sh, err := storage.NewStorage(p)
	if err != nil {
		return ""
	}
	backupPath, err := sh.GetBackupPath(p.LocalPath)
	if err != nil {
		return ""
	}
	return utils.FullPath(backupPath, p.OutName)
}
