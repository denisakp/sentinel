package runtime

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/compress"
	"github.com/denisakp/sentinel/internal/adapters/crypto"
	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// writeCompressedBackup compresses plaintext into path with the given algorithm
// and returns the SHA-256 hex digest of the compressed (stored) bytes.
func writeCompressedBackup(t *testing.T, path string, plaintext []byte, algorithm string) string {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create compressed backup: %v", err)
	}
	hw := crypto.NewHashingWriter(out)
	cw, err := compress.NewCompressWriter(hw, algorithm, 0)
	if err != nil {
		t.Fatalf("NewCompressWriter(): %v", err)
	}
	if _, err := cw.Write(plaintext); err != nil {
		t.Fatalf("compress write: %v", err)
	}
	if err := cw.Close(); err != nil {
		t.Fatalf("compress close: %v", err)
	}
	hash := hw.Sum()
	if err := out.Close(); err != nil {
		t.Fatalf("close compressed backup: %v", err)
	}
	return hash
}

// TestApplyPreflight_DecompressesFromManifest asserts restore auto-detects
// pipeline compression from the manifest (no operator flag) and decompresses
// the staged artifact back to the original plaintext dump.
func TestApplyPreflight_DecompressesFromManifest(t *testing.T) {
	for _, algorithm := range []string{compress.AlgorithmGzip, compress.AlgorithmZstd} {
		t.Run(algorithm, func(t *testing.T) {
			dir := t.TempDir()
			stagedPath := filepath.Join(dir, "backup.sql")
			plaintext := bytes.Repeat([]byte("-- restore me, decompressed --\n"), 200)

			hash := writeCompressedBackup(t, stagedPath, plaintext, algorithm)

			manifestPath := stagedPath + ".manifest.json"
			m := &ports.BackupManifest{
				BackupID:     "cmp-1",
				Database:     "db",
				DatabaseType: "postgres",
				CreatedAt:    time.Now().UTC(),
				Hash:         ports.HashInfo{Algorithm: "sha256", Value: hash},
				Compression:  &ports.CompressionInfo{Algorithm: algorithm, Level: 0},
			}
			if err := manifest.WriteManifest(manifestPath, m); err != nil {
				t.Fatalf("WriteManifest(): %v", err)
			}

			artifact := &StagedArtifact{Path: stagedPath, ManifestPath: manifestPath}
			outPath, err := applyPreflight(context.Background(), &config.Configuration{}, artifact, false, false)
			if err != nil {
				t.Fatalf("applyPreflight() error = %v", err)
			}

			got, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("read decompressed output: %v", err)
			}
			if !bytes.Equal(got, plaintext) {
				t.Fatalf("decompressed payload mismatch: got %d bytes, want %d", len(got), len(plaintext))
			}
		})
	}
}

// TestApplyPreflight_NoCompressionBlockUnchanged asserts a manifest WITHOUT a
// compression block leaves a plaintext staged artifact byte-for-byte unchanged
// (legacy backup backward-compat: no decompress attempted).
func TestApplyPreflight_NoCompressionBlockUnchanged(t *testing.T) {
	dir := t.TempDir()
	stagedPath := filepath.Join(dir, "backup.sql")
	plaintext := []byte("-- an uncompressed, unencrypted dump --\n")
	if err := os.WriteFile(stagedPath, plaintext, 0o600); err != nil {
		t.Fatalf("write staged artifact: %v", err)
	}

	// Hash of the plaintext (stored) bytes.
	hash := hashFile(t, stagedPath)

	manifestPath := stagedPath + ".manifest.json"
	m := &ports.BackupManifest{
		BackupID:     "plain-1",
		Database:     "db",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC(),
		Hash:         ports.HashInfo{Algorithm: "sha256", Value: hash},
	}
	if err := manifest.WriteManifest(manifestPath, m); err != nil {
		t.Fatalf("WriteManifest(): %v", err)
	}

	artifact := &StagedArtifact{Path: stagedPath, ManifestPath: manifestPath}
	outPath, err := applyPreflight(context.Background(), &config.Configuration{}, artifact, false, false)
	if err != nil {
		t.Fatalf("applyPreflight() error = %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("payload changed: got %q want %q", got, plaintext)
	}
}

// hashFile computes the SHA-256 hex digest of the file at path.
func hashFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read for hash: %v", err)
	}
	hw := crypto.NewHashingWriter(io.Discard)
	if _, err := hw.Write(data); err != nil {
		t.Fatalf("hash write: %v", err)
	}
	return hw.Sum()
}
