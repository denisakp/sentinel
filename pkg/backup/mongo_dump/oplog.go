package mongo_dump

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// OplogArchiveArgs defines oplog capture inputs.
type OplogArchiveArgs struct {
	// URI is the MongoDB connection URI (must target a replica-set member).
	URI string
	// OutputDir is the local directory where the archive will be written.
	OutputDir string
	// ArchiveName overrides the default archive filename.
	ArchiveName string
}

// OplogArchiveResult contains metadata for the generated oplog archive.
type OplogArchiveResult struct {
	// ArchivePath is the absolute path of the written mongodump archive.
	ArchivePath string
}

// ArchiveOplog captures the live MongoDB oplog (local.oplog.rs) to a mongodump
// archive file under OutputDir.  The resulting archive can be applied during
// incremental restore with mongorestore --oplogReplay --archive=<path>.
func ArchiveOplog(ctx context.Context, args *OplogArchiveArgs) (*OplogArchiveResult, error) {
	if args == nil {
		return nil, fmt.Errorf("oplog archive args are required")
	}
	if strings.TrimSpace(args.URI) == "" {
		return nil, fmt.Errorf("uri is required for oplog archival")
	}
	if strings.TrimSpace(args.OutputDir) == "" {
		return nil, fmt.Errorf("output_dir is required for oplog archival")
	}

	if err := os.MkdirAll(args.OutputDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create oplog output dir: %w", err)
	}

	archiveName := strings.TrimSpace(args.ArchiveName)
	if archiveName == "" {
		archiveName = fmt.Sprintf("oplog_%d.archive", time.Now().UTC().UnixNano())
	}
	archivePath := filepath.Join(args.OutputDir, archiveName)

	// mongodump --uri=<uri> --db=local --collection=oplog.rs --archive=<path>
	// This captures the live oplog into a mongodump-format archive that
	// mongorestore can apply with --oplogReplay.
	cmdArgs := []string{
		fmt.Sprintf("--uri=%s", args.URI),
		"--db=local",
		"--collection=oplog.rs",
		fmt.Sprintf("--archive=%s", archivePath),
		"--quiet",
	}

	cmd := exec.CommandContext(ctx, "mongodump", cmdArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("mongodump oplog capture failed: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("mongodump oplog capture failed: %w", err)
	}

	return &OplogArchiveResult{ArchivePath: archivePath}, nil
}
