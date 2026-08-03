package mongo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// OplogReplayArgs defines inputs for mongorestore oplog replay.
type OplogReplayArgs struct {
	// URI is the MongoDB connection URI for the target replica-set.
	URI string
	// ArchivePath is the path to the mongodump archive containing local.oplog.rs.
	ArchivePath string
}

// ReplayOplog applies oplog entries from a mongodump archive into the target
// MongoDB instance using mongorestore --oplogReplay --archive=<path>.
func ReplayOplog(ctx context.Context, args *OplogReplayArgs) error {
	if args == nil {
		return fmt.Errorf("oplog replay args are required")
	}
	if strings.TrimSpace(args.URI) == "" {
		return fmt.Errorf("uri is required for oplog replay")
	}
	if strings.TrimSpace(args.ArchivePath) == "" {
		return fmt.Errorf("archive_path is required for oplog replay")
	}

	if _, err := os.Stat(args.ArchivePath); err != nil {
		return fmt.Errorf("oplog archive not found at %s: %w", args.ArchivePath, err)
	}

	cmdArgs := []string{
		fmt.Sprintf("--uri=%s", args.URI),
		"--oplogReplay",
		fmt.Sprintf("--archive=%s", args.ArchivePath),
	}

	cmd := exec.CommandContext(ctx, "mongorestore", cmdArgs...)
	cmd.Env = os.Environ()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("mongorestore oplog replay failed: %w: %s", err, msg)
		}
		return fmt.Errorf("mongorestore oplog replay failed: %w", err)
	}

	return nil
}

// IsRestoreOptions marks *OplogReplayArgs as a ports.RestoreOptions.
func (*OplogReplayArgs) IsRestoreOptions() {}
