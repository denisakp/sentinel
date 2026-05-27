package pg_dump

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
)

// installFakePgDump installs a fake `pg_dump` (or pg_dumpall) on PATH that
// writes `payload` to stdout.
func installFakeEngine(t *testing.T, engineName, payload string) {
	t.Helper()
	dir := t.TempDir()
	// Single-quote-safe via base64 round-trip through shell.
	script := fmt.Sprintf("#!/bin/sh\nprintf %%s %q\n", payload)
	if err := os.WriteFile(filepath.Join(dir, engineName), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", engineName, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func stubConnectivity(t *testing.T) {
	t.Helper()
	orig := checkConnectivity
	checkConnectivity = func(string, string, string, string, string, string) (bool, error) { return true, nil }
	t.Cleanup(func() { checkConnectivity = orig })
}

func expectedDigest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func TestBackup_ReturnsInlineDigest(t *testing.T) {
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
			installFakeEngine(t, "pg_dump", tc.payload)
			stubConnectivity(t)

			out := t.TempDir()
			digest, err := Backup(&PgDumpArgs{
				Host: "127.0.0.1", Port: "5432", Username: "u", Database: "d",
				Storage: &storage.Params{StorageType: "local", LocalPath: out, OutName: "x.sql"},
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

func TestBackupAll_ReturnsInlineDigest(t *testing.T) {
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
			installFakeEngine(t, "pg_dumpall", tc.payload)

			out := t.TempDir()
			digest, err := BackupAll(&PgDumpAllArgs{
				Username: "u",
				Storage:  &storage.Params{StorageType: "local", LocalPath: out, OutName: "all.sql"},
			})
			if err != nil {
				t.Fatalf("BackupAll: %v", err)
			}
			if got, want := digest, expectedDigest(tc.payload); got != want {
				t.Fatalf("digest=%s want=%s", got, want)
			}
		})
	}
}
