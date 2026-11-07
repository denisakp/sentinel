package streamsink

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
)

// fakeBackend records what Upload was handed. Embedding the port leaves the
// methods Run never calls nil, so an unexpected call fails loudly.
type fakeBackend struct {
	ports.StorageBackend
	dest      string
	body      []byte
	srcExists bool
	err       error
}

func (f *fakeBackend) Upload(_ context.Context, src, dest string) error {
	f.dest = dest
	b, rerr := os.ReadFile(src)
	f.srcExists = rerr == nil
	f.body = b
	return f.err
}

func withBackend(t *testing.T, b ports.StorageBackend) {
	t.Helper()
	old := newBackend
	newBackend = func(*storage.BackendParams) (ports.StorageBackend, error) { return b, nil }
	t.Cleanup(func() { newBackend = old })
}

func TestRunRemoteStreamsAndUploadsUnderBaseName(t *testing.T) {
	payload := strings.Repeat("remote-dump-bytes\n", 5000)
	fb := &fakeBackend{}
	withBackend(t, fb)

	cmd := exec.Command("/bin/sh", "-c", "printf %s \"$PAYLOAD\"")
	cmd.Env = append(os.Environ(), "PAYLOAD="+payload)

	// The backend path prefix (bucket) must be stripped, as WriteBackup did.
	digest, err := Run(context.Background(), cmd, &storage.BackendParams{StorageType: "s3"}, "my-bucket/dump.sql")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := sha256.Sum256([]byte(payload))
	if digest != hex.EncodeToString(want[:]) {
		t.Errorf("digest = %s, want sha256 of the streamed bytes", digest)
	}
	if fb.dest != "dump.sql" {
		t.Errorf("uploaded under %q, want %q", fb.dest, "dump.sql")
	}
	if !fb.srcExists || string(fb.body) != payload {
		t.Errorf("backend did not receive the full dump from the staging file")
	}
}

func TestRunRemoteRemovesStagingAfterUpload(t *testing.T) {
	var staged string
	spy := &stagingSpy{onUpload: func(src string) { staged = src }}
	withBackend(t, spy)

	cmd := exec.Command("/bin/sh", "-c", "printf hello")
	if _, err := Run(context.Background(), cmd, &storage.BackendParams{StorageType: "gcs"}, "b/x.sql"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if staged == "" {
		t.Fatal("Upload was never called")
	}
	if _, err := os.Stat(filepath.Dir(staged)); !os.IsNotExist(err) {
		t.Errorf("staging dir %s was left behind", filepath.Dir(staged))
	}
}

type stagingSpy struct {
	ports.StorageBackend
	onUpload func(string)
}

func (s *stagingSpy) Upload(_ context.Context, src, _ string) error {
	s.onUpload(src)
	return nil
}

func TestRunRemoteUploadFailureIsClassified(t *testing.T) {
	withBackend(t, &fakeBackend{err: errors.New("boom")})

	cmd := exec.Command("/bin/sh", "-c", "printf hello")
	_, err := Run(context.Background(), cmd, &storage.BackendParams{StorageType: "s3"}, "b/x.sql")
	if !errors.Is(err, ErrUpload) {
		t.Fatalf("err = %v, want it to wrap ErrUpload", err)
	}
	if got := Describe(err, "pg_dump", nil).Error(); !strings.Contains(got, "failed to write backup to storage") {
		t.Errorf("Describe() = %q, want the storage-write message", got)
	}
}

func TestRunRemoteToolFailureSkipsUpload(t *testing.T) {
	fb := &fakeBackend{}
	withBackend(t, fb)

	cmd := exec.Command("/bin/sh", "-c", "printf partial; exit 2")
	_, err := Run(context.Background(), cmd, &storage.BackendParams{StorageType: "s3"}, "b/x.sql")
	if err == nil || errors.Is(err, ErrUpload) {
		t.Fatalf("err = %v, want a non-upload failure", err)
	}
	if fb.dest != "" {
		t.Errorf("Upload was called (%q) for a failed dump", fb.dest)
	}
	if got := Describe(err, "pg_dump", []byte("oops")).Error(); !strings.Contains(got, "failed to execute pg_dump command") {
		t.Errorf("Describe() = %q, want the execute message", got)
	}
}

func TestRunLocalWritesToFullPath(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "d.sql")
	cmd := exec.Command("/bin/sh", "-c", "printf local")
	if _, err := Run(context.Background(), cmd, &storage.BackendParams{}, dest); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "local" {
		t.Errorf("artifact = %q, want %q", b, "local")
	}
}
