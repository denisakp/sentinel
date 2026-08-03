package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

// localCompressionJob builds a local-storage job whose artifact already exists
// on disk, ready for ApplyArtifactSecurity.
func localCompressionJob(t *testing.T, content []byte) (Job, string) {
	t.Helper()
	dir := t.TempDir()
	artifact := filepath.Join(dir, "out.sql")
	if err := os.WriteFile(artifact, content, 0o644); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	return Job{
		Name:        "job-c",
		Engine:      "postgres",
		Database:    "app",
		Options:     fakeOptions{},
		StorageType: "local",
		OutName:     artifact,
	}, artifact
}

// TestApplyArtifactSecurity_CompressBeforeEncrypt asserts the pipeline ordering
// (dump → compress → hash → encrypt): the compress hook runs first, the encrypt
// hook observes the COMPRESSED bytes, and the manifest records the ciphertext
// digest as Hash.Value with the plaintext dump digest preserved.
func TestApplyArtifactSecurity_CompressBeforeEncrypt(t *testing.T) {
	job, artifact := localCompressionJob(t, []byte("-- plaintext dump payload\n"))

	var order []string
	compressedMarker := []byte("COMPRESSED-BYTES")

	job.CompressArtifact = func(path string) (bool, *ports.CompressionInfo, string, error) {
		order = append(order, "compress")
		if err := os.WriteFile(path, compressedMarker, 0o644); err != nil {
			return false, nil, "", err
		}
		return true, &ports.CompressionInfo{Algorithm: "zstd", Level: 3}, "comp-hash", nil
	}
	job.EncryptArtifact = func(path, _ string) (bool, *ports.EncryptionInfo, string, error) {
		order = append(order, "encrypt")
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, compressedMarker) {
			t.Errorf("encrypt hook saw %q, want compressed bytes %q", got, compressedMarker)
		}
		return true, &ports.EncryptionInfo{Algorithm: "AES-256-GCM"}, "enc-hash", nil
	}

	manifests := &stubManifestStore{}
	e := NewExecutor(nil, nil, nil, nil, &stubRecorder{}, nil, nil, nil, manifests)

	outcome, _, err := e.ApplyArtifactSecurity(context.Background(), job, "plain-digest", "")
	if err != nil {
		t.Fatalf("ApplyArtifactSecurity() error = %v", err)
	}

	if len(order) != 2 || order[0] != "compress" || order[1] != "encrypt" {
		t.Fatalf("stage order = %v, want [compress encrypt]", order)
	}
	if outcome.HashValue != "enc-hash" {
		t.Fatalf("outcome.HashValue = %q, want enc-hash", outcome.HashValue)
	}

	m := manifests.written[artifact+".manifest.json"]
	if m == nil {
		t.Fatal("manifest not written")
	}
	if m.Compression == nil || m.Compression.Algorithm != "zstd" || m.Compression.Level != 3 {
		t.Fatalf("manifest.Compression = %#v, want zstd/3", m.Compression)
	}
	if m.Hash.Value != "enc-hash" {
		t.Fatalf("manifest.Hash.Value = %q, want enc-hash", m.Hash.Value)
	}
	if m.Hash.PlaintextValue != "plain-digest" {
		t.Fatalf("manifest.Hash.PlaintextValue = %q, want plain-digest", m.Hash.PlaintextValue)
	}
	if m.Encryption == nil {
		t.Fatal("manifest.Encryption = nil, want encryption info")
	}
}

// TestApplyArtifactSecurity_CompressionOnlyManifest asserts compression without
// encryption records the compressed-bytes digest as the stored-artifact hash
// and stamps the manifest compression block.
func TestApplyArtifactSecurity_CompressionOnlyManifest(t *testing.T) {
	job, artifact := localCompressionJob(t, []byte("-- plaintext dump payload\n"))

	compressedMarker := []byte("GZIP-BYTES")
	job.CompressArtifact = func(path string) (bool, *ports.CompressionInfo, string, error) {
		if err := os.WriteFile(path, compressedMarker, 0o644); err != nil {
			return false, nil, "", err
		}
		return true, &ports.CompressionInfo{Algorithm: "gzip", Level: 6}, "comp-hash", nil
	}

	manifests := &stubManifestStore{}
	e := NewExecutor(nil, nil, nil, nil, &stubRecorder{}, nil, nil, nil, manifests)

	outcome, _, err := e.ApplyArtifactSecurity(context.Background(), job, "plain-digest", "")
	if err != nil {
		t.Fatalf("ApplyArtifactSecurity() error = %v", err)
	}
	if outcome.HashValue != "comp-hash" {
		t.Fatalf("outcome.HashValue = %q, want comp-hash (compressed bytes)", outcome.HashValue)
	}
	if outcome.Encrypted {
		t.Fatal("outcome.Encrypted = true, want false")
	}

	m := manifests.written[artifact+".manifest.json"]
	if m == nil {
		t.Fatal("manifest not written")
	}
	if m.Compression == nil || m.Compression.Algorithm != "gzip" || m.Compression.Level != 6 {
		t.Fatalf("manifest.Compression = %#v, want gzip/6", m.Compression)
	}
	if m.Hash.Value != "comp-hash" || m.Hash.PlaintextValue != "plain-digest" {
		t.Fatalf("manifest.Hash = %#v", m.Hash)
	}
	if m.Encryption != nil {
		t.Fatalf("manifest.Encryption = %#v, want nil", m.Encryption)
	}

	// The on-disk artifact holds the compressed bytes.
	got, _ := os.ReadFile(artifact)
	if !bytes.Equal(got, compressedMarker) {
		t.Fatalf("artifact bytes = %q, want compressed bytes", got)
	}
}

// TestApplyArtifactSecurity_NoCompressionByteForByte asserts that with no
// compression hook wired, the artifact bytes and manifest are unchanged from
// the pre-compression behaviour (opt-in default OFF; byte-for-byte parity).
func TestApplyArtifactSecurity_NoCompressionByteForByte(t *testing.T) {
	original := []byte("-- plaintext dump payload, do not touch\n")
	job, artifact := localCompressionJob(t, original)

	manifests := &stubManifestStore{}
	e := NewExecutor(nil, nil, nil, nil, &stubRecorder{}, nil, nil, nil, manifests)

	outcome, _, err := e.ApplyArtifactSecurity(context.Background(), job, "plain-digest", "")
	if err != nil {
		t.Fatalf("ApplyArtifactSecurity() error = %v", err)
	}

	got, _ := os.ReadFile(artifact)
	if !bytes.Equal(got, original) {
		t.Fatalf("artifact bytes changed: got %q want %q", got, original)
	}
	if outcome.HashValue != "plain-digest" {
		t.Fatalf("outcome.HashValue = %q, want plain-digest", outcome.HashValue)
	}

	m := manifests.written[artifact+".manifest.json"]
	if m == nil {
		t.Fatal("manifest not written")
	}
	if m.Compression != nil {
		t.Fatalf("manifest.Compression = %#v, want nil (no compression configured)", m.Compression)
	}
	if m.Hash.Value != "plain-digest" {
		t.Fatalf("manifest.Hash.Value = %q, want plain-digest", m.Hash.Value)
	}
}
