package mongo

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// stagingDir manages a transient <backupPath>/.staging/<jobID>/ directory used
// for mongodump archive output when uploading to a remote storage backend.
type stagingDir struct {
	Root  string
	JobID string
}

// newStagingDir creates a fresh staging directory under backupPath. If jobID is
// empty, a unique mongo-<unix-nanos>-<6-hex> token is generated. Returns an
// error if the directory already exists (collision is fatal — FR-009).
func newStagingDir(backupPath, jobID string) (*stagingDir, error) {
	if strings.TrimSpace(backupPath) == "" {
		return nil, fmt.Errorf("staging: backup path is required")
	}
	if strings.TrimSpace(jobID) == "" {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, fmt.Errorf("staging: failed to generate job id: %w", err)
		}
		jobID = fmt.Sprintf("mongo-%d-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
	}

	root := filepath.Join(backupPath, ".staging", jobID)
	if _, err := os.Stat(root); err == nil {
		return nil, fmt.Errorf("staging: directory already exists: %s", root)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("staging: failed to stat %s: %w", root, err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("staging: failed to create %s: %w", root, err)
	}
	return &stagingDir{Root: root, JobID: jobID}, nil
}

// archiveFileName returns the canonical archive file name for a mongodump
// archive. When gzip is true, ".gz" is appended. Shared by stagingDir and the
// executor-owned staging path (spec 047) so both compute the same name.
func archiveFileName(gzip bool) string {
	name := "dump.archive"
	if gzip {
		name += ".gz"
	}
	return name
}

// ArchivePath returns the canonical archive file path inside the staging dir.
// When gzip is true, ".gz" is appended.
func (s *stagingDir) ArchivePath(gzip bool) string {
	return filepath.Join(s.Root, archiveFileName(gzip))
}

// Cleanup removes the staging directory tree. It is idempotent and best-effort:
// already-absent dirs return nil, other errors are logged to stderr but do not
// propagate so the primary backup error is preserved.
func (s *stagingDir) Cleanup() {
	if s == nil || s.Root == "" {
		return
	}
	err := os.RemoveAll(s.Root)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "staging: failed to remove %s: %v\n", s.Root, err)
	}
}
