package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/dbprobertesting"
)

// fakeOptions satisfies the ports.EngineOptions marker.
type fakeOptions struct{}

func (fakeOptions) IsEngineOptions() {}

// fakeDumpBuilder writes a fixture artifact and returns a canned digest.
type fakeDumpBuilder struct {
	digest    string
	err       error
	writePath string
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
	return ports.BuildResult{Digest: f.digest}, nil
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
func (s *stubManifestStore) VerifyHash(string, string, string) error { return nil }
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
