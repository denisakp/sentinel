package restore

// Port-mock coverage for the domain Executor (T057). The behavior-parity
// flow tests live in internal/adapters/restore/runtime/executor_test.go
// (moved with the orchestration); these tests pin the port boundaries:
// RestoreBuilder dispatch, ChainAssembler invocation, planner rejection.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/chainassemblertesting"
)

type fakeRestoreOptions struct{ path string }

func (fakeRestoreOptions) IsRestoreOptions() {}

type fakeRestoreBuilder struct {
	calls []string
	err   error
}

func (f *fakeRestoreBuilder) Build(bc ports.RestoreBuildContext) (ports.RestoreBuildResult, error) {
	opts := bc.Options.(fakeRestoreOptions)
	f.calls = append(f.calls, opts.path)
	return ports.RestoreBuildResult{}, f.err
}

type restoreRecorder struct {
	ports.Recorder
	entries []*ports.RestoreExecution
}

func (r *restoreRecorder) RecordRestoreExecution(_ context.Context, entry *ports.RestoreExecution) error {
	r.entries = append(r.entries, entry)
	return nil
}

func fullManifest(id string) *ports.BackupManifest {
	return &ports.BackupManifest{BackupID: id}
}

func incrementalManifest(baseline string, required []string) *ports.BackupManifest {
	return &ports.BackupManifest{
		BackupID: "incr-tip",
		AdvancedRestore: &ports.AdvancedRestoreMetadata{
			Capabilities: []string{"incremental"},
			IncrementalLineage: &ports.IncrementalLineageMetadata{
				BaselineBackupID:   baseline,
				RequiredBackupIDs:  required,
				ExecutionSupported: true,
			},
		},
	}
}

func baseDomainJob(t *testing.T, mode string, m *ports.BackupManifest) Job {
	t.Helper()
	dir := t.TempDir()
	staged := filepath.Join(dir, "artifact.dump")
	if err := os.WriteFile(staged, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return Job{
		Name:        "job-a",
		Engine:      "postgres",
		Database:    "app",
		RestoreMode: mode,
		StagingDir:  dir,
		StageSource: func(context.Context) (*StagedArtifact, error) {
			return &StagedArtifact{Path: staged, ManifestPath: staged + ".manifest.json", SourcePath: "artifact.dump", SizeBytes: 7}, nil
		},
		BuildPlanRequest: func() (*PlanRequest, error) {
			req := &PlanRequest{RestoreMode: mode}
			if mode == "incremental" {
				req.IncrementalFromBackup = "base-001"
			}
			return req, nil
		},
		LoadPlanManifest: func(string) (*ports.BackupManifest, error) {
			if m == nil {
				return nil, ports.ErrNoManifest
			}
			return m, nil
		},
		ReadManifest: func(string) (*ports.BackupManifest, error) { return nil, ports.ErrNoManifest },
		Preflight: func(_ context.Context, a *StagedArtifact) (string, error) {
			return a.Path, nil
		},
		RestoreOptions: func(stagedPath string) (ports.RestoreOptions, error) {
			return fakeRestoreOptions{path: stagedPath}, nil
		},
	}
}

func TestRunFullSuccessDispatchesRestoreBuilder(t *testing.T) {
	builder := &fakeRestoreBuilder{}
	rec := &restoreRecorder{}
	e := NewExecutor(builder, nil, nil, nil, rec, nil, nil, nil, chainassemblertesting.NewMockAssembler())

	job := baseDomainJob(t, "full", fullManifest("base-001"))
	res, err := e.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Status != ports.StatusSuccess {
		t.Fatalf("Status = %q", res.Status)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("builder calls = %v, want 1", builder.calls)
	}
	if len(rec.entries) != 1 || rec.entries[0].Status != ports.StatusSuccess {
		t.Fatalf("recorded = %#v", rec.entries)
	}
}

func TestRunIncrementalPGInvokesChainAssembler(t *testing.T) {
	builder := &fakeRestoreBuilder{}
	rec := &restoreRecorder{}
	assembler := chainassemblertesting.NewMockAssembler()

	job := baseDomainJob(t, "incremental", incrementalManifest("base-001", []string{"incr-001"}))
	dir := job.StagingDir
	combined := filepath.Join(dir, "combined")
	if err := os.MkdirAll(combined, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	assembler.SeedResult(combined, nil)
	job.StageChain = func(_ context.Context, ids []string) ([]*StagedArtifact, error) {
		out := make([]*StagedArtifact, 0, len(ids))
		for _, id := range ids {
			p := filepath.Join(dir, id+".staged")
			if err := os.WriteFile(p, []byte(id), 0o600); err != nil {
				return nil, err
			}
			out = append(out, &StagedArtifact{Path: p, SourcePath: id, SizeBytes: int64(len(id))})
		}
		return out, nil
	}
	job.VerifyAfterRun = func(context.Context) (bool, error) { return true, nil }

	e := NewExecutor(builder, nil, nil, nil, rec, nil, nil, nil, assembler)
	res, err := e.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !res.VerificationPassed {
		t.Fatal("VerificationPassed = false")
	}
	calls := assembler.Calls()
	if len(calls) != 1 {
		t.Fatalf("assembler calls = %d, want 1", len(calls))
	}
	if len(calls[0].StagedSources) < 2 {
		t.Fatalf("staged sources = %v, want >= 2", calls[0].StagedSources)
	}
	if len(builder.calls) != 1 || builder.calls[0] != combined {
		t.Fatalf("builder calls = %v, want [%s]", builder.calls, combined)
	}
}

func TestRunPlannerRejectionSkipsEngine(t *testing.T) {
	builder := &fakeRestoreBuilder{}
	rec := &restoreRecorder{}
	e := NewExecutor(builder, nil, nil, nil, rec, nil, nil, nil, nil)

	// PITR for a non-postgres engine is rejected by the pure planner.
	job := baseDomainJob(t, "pitr", nil)
	job.Engine = "mysql"
	now := time.Now().UTC()
	job.BuildPlanRequest = func() (*PlanRequest, error) {
		return &PlanRequest{RestoreMode: "pitr", PITRTimestampUTC: &now}, nil
	}

	res, err := e.Run(context.Background(), job)
	if err == nil {
		t.Fatal("Run() error = nil, want planning rejection")
	}
	if res.PlanningStatus != string(PlanStatusRejected) {
		t.Fatalf("PlanningStatus = %q", res.PlanningStatus)
	}
	if len(builder.calls) != 0 {
		t.Fatalf("builder calls = %v, want none", builder.calls)
	}
}

func TestRunEngineFailureRecordsFailure(t *testing.T) {
	builder := &fakeRestoreBuilder{err: errors.New("engine boom")}
	rec := &restoreRecorder{}
	e := NewExecutor(builder, nil, nil, nil, rec, nil, nil, nil, nil)

	job := baseDomainJob(t, "full", nil)
	res, err := e.Run(context.Background(), job)
	if err == nil {
		t.Fatal("Run() error = nil, want engine failure")
	}
	if res.Status != ports.StatusFailed {
		t.Fatalf("Status = %q", res.Status)
	}
	if res.Reason != "restore_failed" {
		t.Fatalf("Reason = %q", res.Reason)
	}
	if len(rec.entries) != 1 || rec.entries[0].Status != ports.StatusFailed {
		t.Fatalf("recorded = %#v", rec.entries)
	}
}
