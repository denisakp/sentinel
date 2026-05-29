package mongo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
)

// fakeBackend captures Upload invocations for assertion in tests.
type fakeBackend struct {
	uploadCalls []struct{ src, dest string }
	uploadErr   error
}

func (f *fakeBackend) Upload(_ context.Context, src, dest string) error {
	f.uploadCalls = append(f.uploadCalls, struct{ src, dest string }{src, dest})
	return f.uploadErr
}
func (f *fakeBackend) Download(context.Context, string, string) error { return nil }
func (f *fakeBackend) Delete(context.Context, string) error           { return nil }
func (f *fakeBackend) List(context.Context, string) ([]ports.StorageObject, error) {
	return nil, nil
}
func (f *fakeBackend) Exists(context.Context, string) (bool, error) { return false, nil }

// installFakeMongodump puts a shell stub on PATH so exec.Command("mongodump", ...) succeeds.
// The stub parses --archive=<path> and writes a small payload to that path.
func installFakeMongodump(t *testing.T, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	body := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  case "$a" in
    --archive=*) f="${a#--archive=}"; mkdir -p "$(dirname "$f")"; echo "fake-archive" > "$f" ;;
    --out=*) d="${a#--out=}"; mkdir -p "$d" ;;
  esac
done
exit %d
`, exitCode)
	path := filepath.Join(dir, "mongodump")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stubConnectivity replaces checkConnectivity for the duration of a test.
func stubConnectivity(t *testing.T) {
	t.Helper()
	orig := checkConnectivity
	checkConnectivity = func(string) error { return nil }
	t.Cleanup(func() { checkConnectivity = orig })
}

func TestBackup_RemoteUploadHappyPath(t *testing.T) {
	installFakeMongodump(t, 0)
	stubConnectivity(t)

	tmp := t.TempDir()
	fake := &fakeBackend{}
	orig := backupBackendFactory
	backupBackendFactory = func(*storage.Params) (ports.StorageBackend, error) { return fake, nil }
	t.Cleanup(func() { backupBackendFactory = orig })

	da := &DumpMongoArgs{
		Uri: "mongodb://stub",
		Storage: &storage.Params{
			StorageType: "s3",
			LocalPath:   tmp,
			OutName:     "mongo.archive",
		},
	}
	digest, err := Backup(da)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if digest != "" {
		t.Fatalf("remote branch digest = %q, want empty", digest)
	}
	if len(fake.uploadCalls) != 1 {
		t.Fatalf("expected 1 Upload call, got %d", len(fake.uploadCalls))
	}
	got := fake.uploadCalls[0]
	if !strings.HasSuffix(got.src, "/dump.archive") {
		t.Errorf("src=%q want suffix /dump.archive", got.src)
	}
	if !strings.Contains(got.src, filepath.Join(tmp, ".staging")) {
		t.Errorf("src=%q want inside %s/.staging", got.src, tmp)
	}
	// staging dir cleaned up
	if entries, _ := os.ReadDir(filepath.Join(tmp, ".staging")); len(entries) != 0 {
		t.Errorf("expected empty .staging, got %d entries", len(entries))
	}
}

func TestBackup_RemoteUploadError_CleansStaging(t *testing.T) {
	installFakeMongodump(t, 0)
	stubConnectivity(t)

	tmp := t.TempDir()
	fake := &fakeBackend{uploadErr: errors.New("upload boom")}
	orig := backupBackendFactory
	backupBackendFactory = func(*storage.Params) (ports.StorageBackend, error) { return fake, nil }
	t.Cleanup(func() { backupBackendFactory = orig })

	da := &DumpMongoArgs{
		Uri: "mongodb://stub",
		Storage: &storage.Params{
			StorageType: "s3",
			LocalPath:   tmp,
			OutName:     "mongo.archive",
		},
	}
	_, err := Backup(da)
	if err == nil || !strings.Contains(err.Error(), "upload boom") {
		t.Fatalf("expected upload error, got %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(tmp, ".staging")); len(entries) != 0 {
		t.Errorf("expected empty .staging after failure, got %d entries", len(entries))
	}
}

func TestBackup_MongodumpFail_CleansStaging(t *testing.T) {
	installFakeMongodump(t, 1) // non-zero exit
	stubConnectivity(t)

	tmp := t.TempDir()
	fake := &fakeBackend{}
	orig := backupBackendFactory
	backupBackendFactory = func(*storage.Params) (ports.StorageBackend, error) { return fake, nil }
	t.Cleanup(func() { backupBackendFactory = orig })

	da := &DumpMongoArgs{
		Uri: "mongodb://stub",
		Storage: &storage.Params{
			StorageType: "s3",
			LocalPath:   tmp,
			OutName:     "mongo.archive",
		},
	}
	_, err := Backup(da)
	if err == nil || !strings.Contains(err.Error(), "failed to run mongo_dump") {
		t.Fatalf("expected mongo_dump error, got %v", err)
	}
	if len(fake.uploadCalls) != 0 {
		t.Errorf("Upload must not be called when mongodump fails")
	}
	if entries, _ := os.ReadDir(filepath.Join(tmp, ".staging")); len(entries) != 0 {
		t.Errorf("expected empty .staging after mongodump fail, got %d entries", len(entries))
	}
}

// preserve original redaction test
func TestErrorRedaction(t *testing.T) {
	const secret = "hunter2"

	t.Run("non-empty stderr redacted", func(t *testing.T) {
		script := "printf 'MONGO_INITDB_ROOT_PASSWORD=hunter2\\nmongodb://u:hunter2@h/db\\n' 1>&2; exit 1"
		cmd := exec.Command("sh", "-c", script)
		var stdErr bytes.Buffer
		cmd.Stderr = &stdErr
		runErr := cmd.Run()
		if runErr == nil {
			t.Fatal("expected non-zero exit")
		}

		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		msg := strings.TrimSpace(redacted)
		if msg == "" {
			t.Fatal("expected non-empty msg")
		}
		wrapped := fmt.Errorf("failed to run mongo_dump: %w: %s", runErr, msg).Error()
		if strings.Contains(wrapped, secret) {
			t.Errorf("leaked: %q", wrapped)
		}
	})

	t.Run("empty stderr skipped", func(t *testing.T) {
		var stdErr bytes.Buffer // empty
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		msg := strings.TrimSpace(redacted)
		if msg != "" {
			t.Errorf("expected empty trimmed msg, got %q", msg)
		}
	})
}
