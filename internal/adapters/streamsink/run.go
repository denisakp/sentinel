package streamsink

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/sanitize"
)

// ErrUpload marks a failure that happened after the dump was produced, while
// handing the finished artifact to remote storage. Callers use it to tell a
// failed engine tool from a failed upload.
var ErrUpload = errors.New("upload to storage failed")

// newBackend is the seam tests replace to avoid real cloud clients.
var newBackend = func(p *storage.BackendParams) (ports.StorageBackend, error) {
	return storage.NewBackend(p)
}

// ObjectKey returns the object name a remote backend stores a dump under.
//
// The legacy Storage.WriteBackup derived the key as the base name of the path
// it was given (s3 and gdrive through utils.FormatResourceValue, gcs through
// extractObjectPath, which also reduces a slashed path to its base name). The
// streaming path must produce the identical key, or a dump would land under a
// different name than the manifest and retention expect.
func ObjectKey(fullPath string) string {
	return filepath.Base(fullPath)
}

// Run executes cmd and streams its stdout to the destination named by p, hashing
// on the way, and returns the hex-encoded SHA-256 of the dump.
//
// Local storage writes straight to fullPath. Remote storage writes to a private
// staging file on disk and then uploads it under ObjectKey(fullPath), removing
// the staging file afterwards. Either way memory is one copy buffer regardless of
// dump size (#164): the dump is never materialised in a []byte.
func Run(ctx context.Context, cmd *exec.Cmd, p *storage.BackendParams, fullPath string) (string, error) {
	if IsLocal(p) {
		return RunToSink(ctx, cmd, Sink{LocalPath: fullPath})
	}

	backend, err := newBackend(p)
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "sentinel-dump-*")
	if err != nil {
		return "", fmt.Errorf("streamsink: creating staging dir: %w", err)
	}
	defer os.RemoveAll(dir)

	key := ObjectKey(fullPath)
	return RunToSink(ctx, cmd, Sink{
		LocalPath:  filepath.Join(dir, key),
		Backend:    backend,
		RemoteName: key,
	})
}

// Describe turns an error from Run into the message the adapters report: an
// upload failure reads "failed to write backup to storage", anything else is a
// failed engine tool and carries its redacted stderr.
func Describe(err error, tool string, stderr []byte) error {
	if errors.Is(err, ErrUpload) {
		return fmt.Errorf("failed to write backup to storage - %w", err)
	}
	redacted, _ := sanitize.RedactStderr(stderr)
	return fmt.Errorf("failed to execute %s command - %w, %s", tool, err, redacted)
}
