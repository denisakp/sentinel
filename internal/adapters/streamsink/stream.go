// Package streamsink streams an engine dump from a subprocess to storage without
// holding it in memory.
//
// The dump adapters used to read the whole of the engine tool's stdout into a
// bytes.Buffer, hash that buffer, and hand the bytes to the storage sink. Peak
// memory therefore tracked the uncompressed dump size, so a 50 GB database
// needed comparable memory to back up and the process was killed by the OOM
// killer rather than failing with a useful error (#164). It also undercut the
// streaming pipeline downstream: compression and encryption stream, but a
// pipeline is only as streaming as its least streaming stage.
//
// This package lives beside the engine adapters rather than inside dump/,
// because ADR 0001 forbids the engine packages importing one another. It is the
// same arrangement as internal/adapters/mysqlargs.
//
// The name deliberately avoids a "dump" prefix: the depguard rule that enforces
// the axis denies the import path prefix internal/adapters/dump, which a package
// called dumpio would match even though it is not an engine adapter.
package streamsink

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/denisakp/sentinel/internal/ports"
)

// Sink says where a streamed dump should end up.
type Sink struct {
	// LocalPath is the file to write. For local storage this is the artifact's
	// final destination; for remote storage it is a staging file that Upload
	// consumes and the caller removes.
	LocalPath string

	// Backend, when non-nil, receives the finished file under RemoteName. Leave
	// it nil for local storage.
	Backend ports.StorageBackend

	// RemoteName is the object key to upload under. Ignored when Backend is nil.
	RemoteName string
}

// RunToSink runs cmd, streaming its standard output to the sink while hashing
// it, and returns the hex-encoded SHA-256 of the bytes produced.
//
// Nothing is buffered: memory is one copy buffer regardless of dump size. The
// digest covers exactly the bytes written, so it stays correct for a dump of any
// size without a second pass over the artifact.
//
// cmd.Stdout must not already be set; RunToSink owns it. stderr is left to the
// caller, which redacts it.
func RunToSink(ctx context.Context, cmd *exec.Cmd, sink Sink) (string, error) {
	if cmd.Stdout != nil {
		return "", fmt.Errorf("dumpio: cmd.Stdout is already set")
	}
	if sink.LocalPath == "" {
		return "", fmt.Errorf("dumpio: sink has no local path")
	}
	if dir := filepath.Dir(sink.LocalPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("dumpio: preparing %q: %w", dir, err)
		}
	}

	f, err := os.Create(sink.LocalPath)
	if err != nil {
		return "", fmt.Errorf("dumpio: creating %q: %w", sink.LocalPath, err)
	}

	hw := crypto.NewHashingWriter(f)
	cmd.Stdout = hw

	runErr := cmd.Run()
	closeErr := f.Close()

	if runErr != nil {
		// Leave nothing half-written behind: a truncated artifact that looks like
		// a backup is worse than no artifact.
		_ = os.Remove(sink.LocalPath)
		return "", runErr
	}
	if closeErr != nil {
		_ = os.Remove(sink.LocalPath)
		return "", fmt.Errorf("dumpio: closing %q: %w", sink.LocalPath, closeErr)
	}

	digest := hw.Sum()

	if sink.Backend != nil {
		if err := sink.Backend.Upload(ctx, sink.LocalPath, sink.RemoteName); err != nil {
			return "", fmt.Errorf("dumpio: uploading %q: %w", sink.RemoteName, err)
		}
	}

	return digest, nil
}

// ensure the writer contract we rely on does not drift
var _ io.Writer = (*crypto.HashingWriter)(nil)
