package mongo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
)

func installFakeMongodumpWithPayload(t *testing.T, payload string) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nprintf %%s %q\n", payload)
	if err := os.WriteFile(filepath.Join(dir, "mongodump"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake mongodump: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func expectedDigest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func TestBackup_LocalReturnsInlineDigest(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"small", "hi"},
		{"boundary-64", strings.Repeat("a", 64)},
		{"boundary-65", strings.Repeat("a", 65)},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installFakeMongodumpWithPayload(t, tc.payload)
			prober := stubConnectivity(t)

			out := t.TempDir()
			digest, err := Backup(prober, &DumpMongoArgs{
				Uri: "mongodb://stub",
				Storage: &storage.Params{
					StorageType: "local",
					LocalPath:   out,
					OutName:     "mongo.archive",
				},
			})
			if err != nil {
				t.Fatalf("Backup: %v", err)
			}
			if got, want := digest, expectedDigest(tc.payload); got != want {
				t.Fatalf("digest=%s want=%s", got, want)
			}
		})
	}
}

func TestBackup_RemoteReturnsEmptyDigest(t *testing.T) {
	installFakeMongodump(t, 0)
	prober := stubConnectivity(t)

	tmp := t.TempDir()
	fake := &fakeBackend{}
	orig := backupBackendFactory
	backupBackendFactory = func(*storage.Params) (ports.StorageBackend, error) { return fake, nil }
	t.Cleanup(func() { backupBackendFactory = orig })

	digest, err := Backup(prober, &DumpMongoArgs{
		Uri: "mongodb://stub",
		Storage: &storage.Params{
			StorageType: "s3",
			LocalPath:   tmp,
			OutName:     "mongo.archive",
		},
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if digest != "" {
		t.Fatalf("remote digest=%q want empty", digest)
	}
}
