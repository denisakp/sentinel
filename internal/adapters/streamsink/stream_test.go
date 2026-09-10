package streamsink

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunToSinkStreamsWithoutBuffering is the regression guard for #164.
//
// The dump adapters read the whole of the engine tool's stdout into a
// bytes.Buffer, hashed that buffer, and handed the bytes to storage. Peak memory
// tracked the uncompressed dump size, so a large database was killed by the OOM
// killer rather than failing with a useful error.
//
// The artifact and the digest must come out identical to what buffering
// produced, or the fix would be a silent data change rather than a memory one.
func TestRunToSinkStreamsWithoutBuffering(t *testing.T) {
	payload := strings.Repeat("sentinel-dump-bytes\n", 5000)
	dest := filepath.Join(t.TempDir(), "dump.sql")

	cmd := exec.Command("/bin/sh", "-c", "printf %s \"$PAYLOAD\"")
	cmd.Env = append(os.Environ(), "PAYLOAD="+payload)

	digest, err := RunToSink(context.Background(), cmd, Sink{LocalPath: dest})
	if err != nil {
		t.Fatalf("RunToSink() error = %v", err)
	}

	want := sha256.Sum256([]byte(payload))
	if digest != hex.EncodeToString(want[:]) {
		t.Errorf("digest = %s, want the sha256 of the streamed bytes", digest)
	}

	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading the artifact: %v", err)
	}
	if string(written) != payload {
		t.Errorf("the artifact does not match what the command produced (%d bytes vs %d)",
			len(written), len(payload))
	}
}

// TestRunToSinkRemovesAPartialArtifactOnFailure: a truncated file that looks like
// a backup is worse than no file, because verify and restore would both accept
// its existence and only discover the truncation later.
func TestRunToSinkRemovesAPartialArtifactOnFailure(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dump.sql")

	// Write some bytes, then fail.
	cmd := exec.Command("/bin/sh", "-c", "printf 'half a dump'; exit 3")

	if _, err := RunToSink(context.Background(), cmd, Sink{LocalPath: dest}); err == nil {
		t.Fatal("RunToSink() succeeded for a command that exited non-zero")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("a partial artifact was left behind at %s", dest)
	}
}

// TestRunToSinkCreatesTheDestinationDirectory: the dump must not fail because the
// output directory does not exist yet, which is the normal state of a fresh
// backup target.
func TestRunToSinkCreatesTheDestinationDirectory(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "nested", "deeper", "dump.sql")

	cmd := exec.Command("/bin/sh", "-c", "printf hello")
	if _, err := RunToSink(context.Background(), cmd, Sink{LocalPath: dest}); err != nil {
		t.Fatalf("RunToSink() error = %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("the artifact was not created: %v", err)
	}
}

// TestRunToSinkRefusesToStealStdout: the caller owning cmd.Stdout would silently
// lose either the artifact or their own capture, so say so instead.
func TestRunToSinkRefusesToStealStdout(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "printf hello")
	cmd.Stdout = os.Stdout

	if _, err := RunToSink(context.Background(), cmd, Sink{LocalPath: filepath.Join(t.TempDir(), "x")}); err == nil {
		t.Error("RunToSink() accepted a command whose Stdout was already set")
	}
}
