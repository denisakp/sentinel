package mysql

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
)

// TestStreamedArtifactLandsUnderLocalPath guards a mistake made while fixing
// #164, not the original defect.
//
// The streaming branch was first placed before Storage.OutName had been
// finalised and joined to the backup path, so it streamed to a RELATIVE name.
// The artifact landed in the process's working directory instead of the
// configured output directory, and the only reason it was noticed is that two
// stray x.sql files turned up in the package directory and were nearly committed.
//
// An artifact written somewhere other than where the configuration says is a
// quiet kind of wrong: the run reports success, and the file is simply not where
// anyone will look for it.
func TestStreamedArtifactLandsUnderLocalPath(t *testing.T) {
	installFakeEngine(t, "mysqldump", "dump bytes")
	prober := stubConnectivity(t)

	out := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	if _, err := Backup(context.Background(), prober, &MySqlDumpArgs{
		Host: "127.0.0.1", Port: "3306", Username: "u", Database: "db",
		Storage: &storage.Params{StorageType: "local", LocalPath: out, OutName: "located.sql"},
	}); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(out, "located.sql")); err != nil {
		t.Errorf("the artifact is not under the configured local_path %s: %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "located.sql")); err == nil {
		t.Error("the artifact was written into the working directory instead of local_path")
		_ = os.Remove(filepath.Join(cwd, "located.sql"))
	}
}
