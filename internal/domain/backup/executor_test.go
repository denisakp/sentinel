package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/dbprobertesting"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
)

// fakeOptions satisfies the ports.EngineOptions marker.
type fakeOptions struct{}

func (fakeOptions) IsEngineOptions() {}

// fakeDumpBuilder writes a fixture artifact and returns a canned digest.
type fakeDumpBuilder struct {
	digest    string
	err       error
	writePath string
	localPath string // reported as BuildResult.LocalPath (staged artifact path)
	calls     int
}

func (f *fakeDumpBuilder) Build(_ ports.BuildContext) (ports.BuildResult, error) {
	f.calls++
	if f.err != nil {
		return ports.BuildResult{}, f.err
	}
	if f.writePath != "" {
		if err := os.WriteFile(f.writePath, []byte("-- dump payload\n"), 0o644); err != nil {
			return ports.BuildResult{}, err
		}
	}
	return ports.BuildResult{Digest: f.digest, LocalPath: f.localPath}, nil
}

// stubRecorder records executions in memory. Embedding the nil interface
// provides the remaining ports.Recorder methods; only the ones Run touches
// are overridden.
type stubRecorder struct {
	ports.Recorder
	execs    []*ports.Execution
	secCalls int
}

func (s *stubRecorder) RecordExecution(_ context.Context, exec *ports.Execution) error {
	exec.ID = "exec-1"
	s.execs = append(s.execs, exec)
	return nil
}

func (s *stubRecorder) RecordSecurityInfo(_ context.Context, _, _, _, _, _ string, _ bool, _ string) error {
	s.secCalls++
	return nil
}

func (s *stubRecorder) ListExecutions(_ context.Context, _ *ports.Filter, _, _ int) ([]ports.Execution, error) {
	return nil, nil
}

// stubManifestStore captures manifest writes.
type stubManifestStore struct {
	written  map[string]*ports.BackupManifest
	writeErr error

	// verifyHashFn, when set, backs VerifyHash with a real check (used by the
	// verify_after_upload tests). Nil preserves the historical always-nil
	// stub behaviour relied on by the other tests in this file.
	verifyHashFn func(path, algorithm, expected string) error
}

func (s *stubManifestStore) Write(path string, m *ports.BackupManifest) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	if s.written == nil {
		s.written = map[string]*ports.BackupManifest{}
	}
	s.written[path] = m
	return nil
}

func (s *stubManifestStore) Read(string) (*ports.BackupManifest, error) {
	return nil, ports.ErrNoManifest
}
func (s *stubManifestStore) LoadForRestore(string) (*ports.BackupManifest, error) {
	return nil, ports.ErrNoManifest
}
func (s *stubManifestStore) VerifyHash(path, algorithm, expected string) error {
	if s.verifyHashFn != nil {
		return s.verifyHashFn(path, algorithm, expected)
	}
	return nil
}

// realVerifyHash computes the real SHA-256 of the file at path and compares
// it against expected — a faithful-enough stand-in for
// manifest_store.VerifyBackupHash without importing the concrete adapter
// from domain test code.
func realVerifyHash(path, _, expected string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != expected {
		return errors.New("hash mismatch: expected=" + expected + " computed=" + got)
	}
	return nil
}

// tamperingBackend wraps a MockBackend but returns corrupted bytes on
// Download, simulating storage-side corruption that occurred after a
// successful upload (e.g. a truncated multipart PUT).
type tamperingBackend struct {
	*storagetesting.MockBackend
}

func (t *tamperingBackend) Download(ctx context.Context, src, dest string) error {
	if err := t.MockBackend.Download(ctx, src, dest); err != nil {
		return err
	}
	return os.WriteFile(dest, []byte("corrupted-bytes-do-not-match-hash"), 0o644)
}

// countingBackend wraps a MockBackend and counts Download calls, used to
// assert that a disabled verify_after_upload performs no re-download.
type countingBackend struct {
	*storagetesting.MockBackend
	downloads int
}

func (c *countingBackend) Download(ctx context.Context, src, dest string) error {
	c.downloads++
	return c.MockBackend.Download(ctx, src, dest)
}
func (s *stubManifestStore) ValidateIncrementalLineage(*ports.BackupManifest) error {
	return nil
}

func testJob(t *testing.T) (Job, string) {
	t.Helper()
	dir := t.TempDir()
	artifact := filepath.Join(dir, "out.sql")
	return Job{
		Name:        "job-a",
		Engine:      "postgres",
		Database:    "app",
		Options:     fakeOptions{},
		StorageType: "local",
		OutName:     artifact,
	}, artifact
}

func TestExecutorRunSuccess(t *testing.T) {
	job, artifact := testJob(t)
	dumps := &fakeDumpBuilder{digest: "abc123", writePath: artifact}
	rec := &stubRecorder{}
	manifests := &stubManifestStore{}

	e := NewExecutor(dumps, nil, nil, nil, rec, nil, nil, nil, manifests)
	res, err := e.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Digest != "abc123" {
		t.Fatalf("Digest = %q, want abc123", res.Digest)
	}
	if res.ManifestPath != artifact+".manifest.json" {
		t.Fatalf("ManifestPath = %q", res.ManifestPath)
	}
	if len(rec.execs) != 1 || rec.execs[0].Status != "success" {
		t.Fatalf("recorded execs = %#v", rec.execs)
	}
	if rec.secCalls != 1 {
		t.Fatalf("security info calls = %d, want 1", rec.secCalls)
	}
	m, ok := manifests.written[res.ManifestPath]
	if !ok {
		t.Fatal("manifest not written")
	}
	if m.Hash.Value != "abc123" || m.Hash.PlaintextValue != "abc123" {
		t.Fatalf("manifest hash = %#v", m.Hash)
	}
	if dumps.calls != 1 {
		t.Fatalf("dump calls = %d, want 1", dumps.calls)
	}
}

// TestExecutorRemoteBackupIsHashedAndEncrypted is the RED confirmation test for
// the remote-artifact security bypass (fix/remote-artifact-security). A backup
// job targeting REMOTE storage (s3/gcs/azure/gdrive) with encryption configured
// MUST still be encrypted, hashed, and have its security info recorded — exactly
// like a local job. Today ApplyArtifactSecurity early-returns for non-local
// storage (LocalArtifactInfo → ""), so the encryption hook never runs, no hash
// is recorded, and the artifact is uploaded in plaintext. This test asserts the
// CORRECT behaviour and therefore fails until the pipeline is fixed.
func TestExecutorRemoteBackupIsHashedAndEncrypted(t *testing.T) {
	encryptCalled := false
	job := Job{
		Name:              "remote-job",
		Engine:            "postgres",
		Database:          "app",
		Options:           fakeOptions{},
		StorageType:       "s3", // remote backend
		OutName:           "backup.sql",
		EncryptionKeyHint: "SENTINEL_KEY",
		EncryptArtifact: func(_, _ string) (bool, *ports.EncryptionInfo, string, error) {
			encryptCalled = true
			return true, &ports.EncryptionInfo{}, "enc-hash", nil
		},
	}
	dumps := &fakeDumpBuilder{digest: "plain-hash"}
	rec := &stubRecorder{}

	e := NewExecutor(dumps, nil, nil, nil, rec, nil, nil, nil, &stubManifestStore{})
	res, err := e.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !encryptCalled {
		t.Error("BYPASS: encryption hook was NOT called for a remote backup — artifact uploaded in plaintext")
	}
	if rec.secCalls != 1 {
		t.Errorf("BYPASS: RecordSecurityInfo calls = %d, want 1 — no hash/checksum recorded for remote backup", rec.secCalls)
	}
	if res.HashValue == "" {
		t.Error("BYPASS: res.HashValue is empty for a remote backup — integrity hash not persisted")
	}
}

// TestExecutorRemoteBackupCleansStagingOnSuccess asserts the staging cleanup
// guarantee: after a successful remote backup Run, the staging dir
// (holding the plaintext/ciphertext artifact) is removed — nothing is left on
// disk once the artifact has been uploaded + recorded.
func TestExecutorRemoteBackupCleansStagingOnSuccess(t *testing.T) {
	stagingDir := t.TempDir()
	stagedArtifact := filepath.Join(stagingDir, "out.sql")

	job := Job{
		Name:        "remote-clean",
		Engine:      "postgres",
		Database:    "app",
		Options:     fakeOptions{},
		StorageType: "s3", // remote
		OutName:     "backup.sql",
		StagingDir:  stagingDir,
	}
	// The dump writes a staged artifact and reports its path as LocalPath.
	dumps := &fakeDumpBuilder{digest: "abc123", writePath: stagedArtifact, localPath: stagedArtifact}
	rec := &stubRecorder{}

	// nil StorageBackend → upload is a no-op; we only assert staging cleanup.
	e := NewExecutor(dumps, nil, nil, nil, rec, nil, nil, nil, &stubManifestStore{})
	if _, err := e.Run(context.Background(), job); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Fatalf("staging dir still exists after success (stat err = %v) — plaintext/ciphertext left behind", err)
	}
}

// TestExecutorRemoteBackupCleansStagingOnFailure asserts the staging dir is
// removed even when the run fails after staging — a forced dump error here — so
// a partially-produced artifact never lingers on disk.
func TestExecutorRemoteBackupCleansStagingOnFailure(t *testing.T) {
	stagingDir := t.TempDir()
	// Simulate an artifact that was partially written before the failure.
	if err := os.WriteFile(filepath.Join(stagingDir, "out.sql"), []byte("partial"), 0o644); err != nil {
		t.Fatalf("seed staged artifact: %v", err)
	}

	job := Job{
		Name:        "remote-fail",
		Engine:      "postgres",
		Database:    "app",
		Options:     fakeOptions{},
		StorageType: "s3", // remote
		OutName:     "backup.sql",
		StagingDir:  stagingDir,
	}
	dumps := &fakeDumpBuilder{err: errors.New("pg_dump: exit status 1")}
	rec := &stubRecorder{}

	e := NewExecutor(dumps, nil, nil, nil, rec, nil, nil, nil, &stubManifestStore{})
	if _, err := e.Run(context.Background(), job); err == nil {
		t.Fatal("Run() error = nil, want dump failure")
	}

	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Fatalf("staging dir still exists after failure (stat err = %v) — artifact left behind", err)
	}
}

func TestExecutorRunPingFailureIsRetriable(t *testing.T) {
	job, _ := testJob(t)
	job.PingBeforeDump = true
	prober := dbprobertesting.NewMockProber()
	prober.SetPingErr(errors.New("connection refused"))
	dumps := &fakeDumpBuilder{digest: "abc123"}
	rec := &stubRecorder{}

	e := NewExecutor(dumps, nil, nil, nil, rec, nil, nil, prober, &stubManifestStore{})
	_, err := e.Run(context.Background(), job)
	if err == nil {
		t.Fatal("Run() error = nil, want connectivity error")
	}
	if !IsRetriable(err) {
		t.Fatalf("IsRetriable(%v) = false, want true", err)
	}
	if dumps.calls != 0 {
		t.Fatalf("dump calls = %d, want 0 (ping gate must run first)", dumps.calls)
	}
	if len(rec.execs) != 1 || rec.execs[0].Status != "failure" {
		t.Fatalf("recorded execs = %#v, want one failure row", rec.execs)
	}
}

func TestExecutorRunDumpFailureIsNotRetriable(t *testing.T) {
	job, _ := testJob(t)
	dumps := &fakeDumpBuilder{err: errors.New("pg_dump: exit status 1")}
	rec := &stubRecorder{}

	e := NewExecutor(dumps, nil, nil, nil, rec, nil, nil, nil, &stubManifestStore{})
	_, err := e.Run(context.Background(), job)
	if err == nil {
		t.Fatal("Run() error = nil, want dump error")
	}
	if IsRetriable(err) {
		t.Fatalf("IsRetriable(%v) = true, want false", err)
	}
	if len(rec.execs) != 1 || rec.execs[0].Status != "failure" {
		t.Fatalf("recorded execs = %#v, want one failure row", rec.execs)
	}
}

func TestExecutorRunRejectsInvalidEngine(t *testing.T) {
	job, _ := testJob(t)
	job.Engine = "oracle"
	e := NewExecutor(&fakeDumpBuilder{}, nil, nil, nil, nil, nil, nil, nil, nil)
	if _, err := e.Run(context.Background(), job); err == nil {
		t.Fatal("Run() error = nil, want invalid engine error")
	}
}

func TestExecutorRunRejectsNilOptions(t *testing.T) {
	job, _ := testJob(t)
	job.Options = nil
	e := NewExecutor(&fakeDumpBuilder{}, nil, nil, nil, nil, nil, nil, nil, nil)
	if _, err := e.Run(context.Background(), job); err == nil {
		t.Fatal("Run() error = nil, want options-required error")
	}
}

// verifyAfterUploadJob builds a staged-remote job (mirrors
// TestExecutorRemoteBackupCleansStagingOnSuccess) with VerifyAfterUpload
// enabled, plus a fakeDumpBuilder whose digest is the REAL SHA-256 of the
// fixture payload it writes — matching what a real dump adapter records
// (e.g. mongo_dump.go's sha256.Sum256(stdOut.Bytes())) so realVerifyHash has
// something genuine to compare against.
func verifyAfterUploadJob(t *testing.T) (Job, *fakeDumpBuilder, *stubRecorder) {
	t.Helper()
	stagingDir := t.TempDir()
	stagedArtifact := filepath.Join(stagingDir, "out.sql")
	payload := []byte("-- dump payload\n")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	job := Job{
		Name:              "verify-job",
		Engine:            "postgres",
		Database:          "app",
		Options:           fakeOptions{},
		StorageType:       "s3", // remote — staged upload path
		OutName:           "backup.sql",
		StagingDir:        stagingDir,
		VerifyAfterUpload: true,
	}
	dumps := &fakeDumpBuilder{digest: digest, writePath: stagedArtifact, localPath: stagedArtifact}
	rec := &stubRecorder{}
	return job, dumps, rec
}

// TestExecutorVerifyAfterUploadSuccess: a healthy remote backup re-downloads
// the uploaded artifact, re-hashes it, matches the manifest hash, and the
// job succeeds (exit criteria #1).
func TestExecutorVerifyAfterUploadSuccess(t *testing.T) {
	job, dumps, rec := verifyAfterUploadJob(t)
	backend := storagetesting.NewMockBackend()
	manifests := &stubManifestStore{verifyHashFn: realVerifyHash}

	e := NewExecutor(dumps, backend, nil, nil, rec, nil, nil, nil, manifests)
	if _, err := e.Run(context.Background(), job); err != nil {
		t.Fatalf("Run() error = %v, want success", err)
	}
	if len(rec.execs) != 1 || rec.execs[0].Status != "success" {
		t.Fatalf("recorded execs = %#v, want one success row", rec.execs)
	}
	if _, ok := backend.GetBytes(job.OutName); !ok {
		t.Fatal("artifact was not uploaded to the backend")
	}
}

// TestExecutorVerifyAfterUploadMismatchFails: an artifact corrupted in
// storage after upload (fault-injected via tamperingBackend) makes the job
// FAIL at backup time with reason verify_after_upload_failed — not silently
// succeed (exit criteria #2).
func TestExecutorVerifyAfterUploadMismatchFails(t *testing.T) {
	job, dumps, rec := verifyAfterUploadJob(t)
	backend := &tamperingBackend{MockBackend: storagetesting.NewMockBackend()}
	manifests := &stubManifestStore{verifyHashFn: realVerifyHash}

	e := NewExecutor(dumps, backend, nil, nil, rec, nil, nil, nil, manifests)
	_, err := e.Run(context.Background(), job)
	if err == nil {
		t.Fatal("Run() error = nil, want verify_after_upload_failed")
	}
	if !strings.Contains(err.Error(), "verify_after_upload_failed") {
		t.Fatalf("Run() error = %v, want it to contain verify_after_upload_failed", err)
	}
	if !strings.Contains(err.Error(), job.OutName) {
		t.Errorf("Run() error = %v, want it to surface the stored object path %q", err, job.OutName)
	}
	if len(rec.execs) != 1 || rec.execs[0].Status != "failure" {
		t.Fatalf("recorded execs = %#v, want one failure row", rec.execs)
	}
	// The corrupt object is left in place for forensics — not deleted.
	if _, ok := backend.GetBytes(job.OutName); !ok {
		t.Error("corrupt object was deleted; it must be left in place")
	}
}

// TestExecutorVerifyAfterUploadDisabledNoDownload: when the job does not
// enable verify_after_upload, the Executor performs no re-download even if a
// storage backend happens to be wired — zero added cost (exit criteria #4).
func TestExecutorVerifyAfterUploadDisabledNoDownload(t *testing.T) {
	job, dumps, rec := verifyAfterUploadJob(t)
	job.VerifyAfterUpload = false
	backend := &countingBackend{MockBackend: storagetesting.NewMockBackend()}
	manifests := &stubManifestStore{verifyHashFn: realVerifyHash}

	e := NewExecutor(dumps, backend, nil, nil, rec, nil, nil, nil, manifests)
	if _, err := e.Run(context.Background(), job); err != nil {
		t.Fatalf("Run() error = %v, want success", err)
	}
	if backend.downloads != 0 {
		t.Fatalf("Download call count = %d, want 0 (verify_after_upload disabled)", backend.downloads)
	}
}

// TestExecutorVerifyAfterUploadNilStorageNoDownload asserts the unset ⇒ nil
// storage port ⇒ no re-download contract even when the job itself requests
// verification (mirrors the factory's "keep storage nil unless enabled"
// wiring — this is the Executor-side half of that contract).
func TestExecutorVerifyAfterUploadNilStorageNoDownload(t *testing.T) {
	job, dumps, rec := verifyAfterUploadJob(t)
	manifests := &stubManifestStore{verifyHashFn: realVerifyHash}

	e := NewExecutor(dumps, nil, nil, nil, rec, nil, nil, nil, manifests)
	if _, err := e.Run(context.Background(), job); err != nil {
		t.Fatalf("Run() error = %v, want success (nil storage = no-op verify)", err)
	}
}
