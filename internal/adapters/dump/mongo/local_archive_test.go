package mongo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
)

// TestLocalMongoBackupIsASingleFileWithARealDigest is the regression guard for
// #191.
//
// A local Mongo job used --out, so mongodump wrote a DIRECTORY and produced
// nothing on stdout. The pipeline hashed that empty stdout and recorded
// sha256("") with a size of 0, pointing the manifest at a path the local backend
// cannot enumerate because it lists files. The job reported success and the
// history recorded a completed backup that verify could not check and restore
// could not read.
//
// The manifest did not merely lack a hash: it carried one that is wrong in a way
// that looks valid, so a repository-wide integrity sweep saw a well-formed record
// for an unrestorable backup.
func TestLocalMongoBackupIsASingleFileWithARealDigest(t *testing.T) {
	payload := "bson-ish archive bytes"
	installFakeMongodumpWithPayload(t, payload)
	prober := stubConnectivity(t)

	out := t.TempDir()
	digest, err := Backup(context.Background(), prober, &DumpMongoArgs{
		Uri:     "mongodb://stub",
		Storage: &storage.Params{StorageType: "local", LocalPath: out, OutName: "mongo.archive"},
	})
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	emptySum := sha256.Sum256(nil)
	if digest == hex.EncodeToString(emptySum[:]) {
		t.Fatal("the recorded digest is the sha256 of no bytes at all, which is what made a broken " +
			"local Mongo backup look like a valid one")
	}
	wantSum := sha256.Sum256([]byte(payload))
	if digest != hex.EncodeToString(wantSum[:]) {
		t.Errorf("digest = %s, want the hash of the archive's own bytes", digest)
	}

	// A single file, not a directory: the local backend lists files, so a
	// directory artifact is invisible to it.
	artifact := filepath.Join(out, "mongo.archive")
	info, err := os.Stat(artifact)
	if err != nil {
		// argsBuilder may resolve the name differently; find it rather than fail
		// on a path assumption.
		entries, readErr := os.ReadDir(out)
		if readErr != nil {
			t.Fatalf("stat %s: %v", artifact, err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
			if !e.IsDir() {
				info, _ = e.Info()
			}
		}
		if info == nil {
			t.Fatalf("no file artifact produced; %s contains: %s", out, strings.Join(names, ", "))
		}
	}
	if info.IsDir() {
		t.Error("the artifact is a directory; the local backend cannot enumerate it and restore " +
			"cannot read it")
	}
	if info.Size() == 0 {
		t.Error("the artifact is empty, so its recorded size of 0 would be accurate and useless")
	}
}
