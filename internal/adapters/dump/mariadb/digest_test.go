package mariadb

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

func installFakeEngine(t *testing.T, payload string) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nprintf %%s %q\n", payload)
	if err := os.WriteFile(filepath.Join(dir, "mariadb-dump"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake mariadb-dump: %v", err)
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

var digestCases = []struct {
	name    string
	payload string
}{
	{"small", "hi"},
	{"boundary-64", strings.Repeat("a", 64)},
	{"boundary-65", strings.Repeat("a", 65)},
	{"empty", ""},
}

func TestBackup_ReturnsInlineDigest(t *testing.T) {
	for _, tc := range digestCases {
		t.Run(tc.name, func(t *testing.T) {
			installFakeEngine(t, tc.payload)
			stubConnectivity(t)

			out := t.TempDir()
			digest, err := Backup(&MariaDBDumpArgs{
				Host: "127.0.0.1", Port: "3306", Username: "u", Database: "d",
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
	for _, tc := range digestCases {
		t.Run(tc.name, func(t *testing.T) {
			installFakeEngine(t, tc.payload)

			out := t.TempDir()
			digest, err := BackupAll(&MariaDBDumpAllArgs{
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
