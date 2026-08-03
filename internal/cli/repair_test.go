package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/lock"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	storage "github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
	"github.com/denisakp/sentinel/internal/domain/retention"
	"github.com/spf13/cobra"
)

// ---- shared helpers ----

func newRepairTestMonitor(t *testing.T) *monitor.Monitor {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := monitor.NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })
	return mon
}

func seedRow(t *testing.T, mon *monitor.Monitor, exec ports.Execution) {
	t.Helper()
	if err := mon.RecordExecution(context.Background(), &exec); err != nil {
		t.Fatalf("RecordExecution: %v", err)
	}
}

func writeLockJSON(t *testing.T, dir, job string, jl ports.JobLock) string {
	t.Helper()
	body, _ := json.Marshal(jl)
	p := filepath.Join(dir, job+".lock")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	return p
}

func repairTestCfg(lockDir string) *config.Configuration {
	return &config.Configuration{
		Databases: map[string]config.BackupJob{
			"job1": {Name: "job1", Storage: config.StorageConfig{Type: "s3", S3Bucket: "b"}},
		},
		Scheduler: config.SchedulerConfig{StaleLockThreshold: 60, LockDir: lockDir},
	}
}

func withMockBackend(t *testing.T, mb *storagetesting.MockBackend) {
	t.Helper()
	orig := newRepairBackend
	newRepairBackend = func(_ *storage.BackendParams) (ports.StorageBackend, error) { return mb, nil }
	t.Cleanup(func() { newRepairBackend = orig })
}

func hasClass(findings []repairFinding, class string) bool {
	for _, f := range findings {
		if f.Class == class {
			return true
		}
	}
	return false
}

func findByPath(findings []repairFinding, class, path string) (repairFinding, bool) {
	for _, f := range findings {
		if f.Class == class && f.Path == path {
			return f, true
		}
	}
	return repairFinding{}, false
}

// ---- flag matrix ----

func TestResolveRepairMode(t *testing.T) {
	build := func(dry, fix, purge, yes bool) *cobra.Command {
		c := &cobra.Command{}
		c.Flags().Bool("dry-run", dry, "")
		c.Flags().Bool("fix", fix, "")
		c.Flags().Bool("purge-orphans", purge, "")
		c.Flags().Bool("yes", yes, "")
		return c
	}
	cases := []struct {
		name                              string
		dry, fix, purge                   bool
		wantReportOnly, wantRecoverable   bool
		wantPurge                         bool
	}{
		{"no-flags", false, false, false, true, false, false},
		{"dry-run", true, false, false, true, false, false},
		{"fix", false, true, false, false, true, false},
		{"purge-implies-fix", false, false, true, false, true, true},
		{"dry-run-overrides-fix", true, true, false, true, false, false},
		{"dry-run-overrides-purge", true, false, true, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := resolveRepairMode(build(tc.dry, tc.fix, tc.purge, false))
			if m.reportOnly != tc.wantReportOnly {
				t.Errorf("reportOnly=%v want %v", m.reportOnly, tc.wantReportOnly)
			}
			if m.doRecoverable() != tc.wantRecoverable {
				t.Errorf("doRecoverable=%v want %v", m.doRecoverable(), tc.wantRecoverable)
			}
			if m.doPurge() != tc.wantPurge {
				t.Errorf("doPurge=%v want %v", m.doPurge(), tc.wantPurge)
			}
		})
	}
}

// ---- class 1/2 : orphan_artifact / untracked / orphan_manifest ----

func TestClassifyStorageObjects(t *testing.T) {
	objs := []ports.StorageObject{
		{Path: "good.sql", SizeBytes: 10},                 // tracked → nothing
		{Path: "good.sql.manifest.json", SizeBytes: 1},    // sidecar for good.sql
		{Path: "orphan.sql", SizeBytes: 20},               // no row, no sidecar → orphan_artifact
		{Path: "untracked.sql", SizeBytes: 30},            // no row, HAS sidecar → untracked
		{Path: "untracked.sql.manifest.json", SizeBytes: 1},
		{Path: "lonely.sql.manifest.json", SizeBytes: 1},  // sidecar, artifact absent → orphan_manifest
	}
	tracked := map[string]struct{}{"good.sql": {}}
	repo := &repairRepo{key: "s3:b", label: "s3://b"}

	findings, cands := classifyStorageObjects(objs, tracked, storagetesting.NewMockBackend(), repo)

	if _, ok := findByPath(findings, driftOrphanArtifact, "orphan.sql"); !ok {
		t.Errorf("expected orphan_artifact for orphan.sql; got %+v", findings)
	}
	if _, ok := findByPath(findings, driftUntrackedArtifact, "untracked.sql"); !ok {
		t.Errorf("expected untracked_artifact for untracked.sql")
	}
	if _, ok := findByPath(findings, driftOrphanManifest, "lonely.sql.manifest.json"); !ok {
		t.Errorf("expected orphan_manifest for lonely.sql.manifest.json")
	}
	// Only the strict orphan is a purge candidate.
	if len(cands) != 1 || cands[0].objectPath != "orphan.sql" {
		t.Errorf("purge candidates = %+v, want [orphan.sql]", cands)
	}
}

// ---- class 3 : artifact_missing ----

func TestClassifyMissing(t *testing.T) {
	rows := []ports.Execution{
		{BackupName: "job1", Status: ports.StatusSuccess, FilePath: "present.sql"},
		{BackupName: "job1", Status: ports.StatusSuccess, FilePath: "gone.sql"},
		{BackupName: "job1", Status: ports.StatusFailed, FilePath: "failed.sql"}, // ignored
	}
	present := map[string]struct{}{"present.sql": {}}

	findings := classifyMissing(rows, present, "s3://b")
	if len(findings) != 1 || findings[0].Path != "gone.sql" {
		t.Fatalf("classifyMissing = %+v, want single gone.sql", findings)
	}

	// nil present set (degraded listing) → no guessing.
	if got := classifyMissing(rows, nil, "s3://b"); got != nil {
		t.Errorf("nil presentBases should yield no findings, got %+v", got)
	}
}

// ---- class 4 : stale_running lock guard ----

func TestClassifyStaleRunning(t *testing.T) {
	host, _ := os.Hostname()
	threshold := time.Minute

	if d, _ := classifyStaleRunning(nil, threshold, host); d != staleFinalize {
		t.Errorf("nil lock → want finalize, got %v", d)
	}
	// Live lock (this process pid).
	live := &ports.JobLock{PID: os.Getpid(), Hostname: host, StartTime: time.Now()}
	if d, _ := classifyStaleRunning(live, threshold, host); d != staleSkipLive {
		t.Errorf("live lock → want skipLive, got %v", d)
	}
	// Foreign host.
	foreign := &ports.JobLock{PID: 999999999, Hostname: "other-host", StartTime: time.Now().Add(-2 * time.Hour)}
	if d, _ := classifyStaleRunning(foreign, threshold, host); d != staleSkipForeign {
		t.Errorf("foreign lock → want skipForeign, got %v", d)
	}
	// Dead pid over threshold, same host → finalize.
	dead := &ports.JobLock{PID: 999999999, Hostname: host, StartTime: time.Now().Add(-2 * time.Hour)}
	if d, _ := classifyStaleRunning(dead, threshold, host); d != staleFinalize {
		t.Errorf("dead lock → want finalize, got %v", d)
	}
}

func TestReconcileStaleRunning_LockGuardAndModes(t *testing.T) {
	ctx := context.Background()
	host, _ := os.Hostname()
	lockDir := t.TempDir()
	cfg := repairTestCfg(lockDir)
	lm := lock.NewManager(lockDir)
	repos := []*repairRepo{{label: "s3://b", jobs: []string{"jobLive", "jobDead"}}}

	seed := func(mon *monitor.Monitor) {
		seedRow(t, mon, ports.Execution{ID: "live-run", BackupName: "jobLive", Status: ports.StatusRunning, FilePath: "a.sql"})
		seedRow(t, mon, ports.Execution{ID: "dead-run", BackupName: "jobDead", Status: ports.StatusRunning, FilePath: "b.sql"})
	}

	// jobLive holds a real live lock; jobDead has none.
	if _, err := lm.TryAcquire("jobLive", time.Minute); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	t.Cleanup(func() { _ = lm.Release("jobLive") })
	_ = host

	// report-only: nothing finalized.
	monRep := newRepairTestMonitor(t)
	seed(monRep)
	_ = reconcileStaleRunning(ctx, monRep, lm, cfg, repairMode{reportOnly: true}, repos)
	if e, _ := monRep.GetExecution(ctx, "dead-run"); e.Status != ports.StatusRunning {
		t.Errorf("report-only must not finalize; dead-run status=%s", e.Status)
	}

	// fix: dead-run finalized to interrupted, live-run untouched.
	monFix := newRepairTestMonitor(t)
	seed(monFix)
	findings := reconcileStaleRunning(ctx, monFix, lm, cfg, repairMode{fix: true}, repos)
	if e, _ := monFix.GetExecution(ctx, "dead-run"); e.Status != ports.StatusInterrupted {
		t.Errorf("dead-run should be interrupted, got %s", e.Status)
	}
	if e, _ := monFix.GetExecution(ctx, "live-run"); e.Status != ports.StatusRunning {
		t.Errorf("live-run should stay running (live lock), got %s", e.Status)
	}
	// The live one is reported as skipped(live lock), the dead one marked.
	var sawSkip, sawMark bool
	for _, f := range findings {
		if f.Job == "jobLive" && f.Action == "skipped (live lock)" {
			sawSkip = true
		}
		if f.Job == "jobDead" && f.Applied {
			sawMark = true
		}
	}
	if !sawSkip || !sawMark {
		t.Errorf("expected skip(live)+mark(dead); findings=%+v", findings)
	}
}

// ---- class 5 : stale_lock ----

func TestReconcileStaleLocks(t *testing.T) {
	host, _ := os.Hostname()
	lockDir := t.TempDir()
	cfg := repairTestCfg(lockDir)
	lm := lock.NewManager(lockDir)
	repos := []*repairRepo{{label: "s3://b", jobs: []string{"deadjob"}}}

	deadPath := writeLockJSON(t, lockDir, "deadjob", ports.JobLock{
		PID: 999999999, JobName: "deadjob", Hostname: host, StartTime: time.Now().Add(-2 * time.Hour),
	})
	// live lock held by this process
	if _, err := lm.TryAcquire("livejob", time.Minute); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	t.Cleanup(func() { _ = lm.Release("livejob") })
	foreignPath := writeLockJSON(t, lockDir, "foreignjob", ports.JobLock{
		PID: 999999999, JobName: "foreignjob", Hostname: "other-host", StartTime: time.Now().Add(-2 * time.Hour),
	})

	// report-only: reports the dead lock but removes nothing.
	rep := reconcileStaleLocks(lm, cfg, repairMode{reportOnly: true}, repos)
	if !hasClass(rep, driftStaleLock) {
		t.Errorf("expected stale_lock finding in report-only")
	}
	if _, err := os.Stat(deadPath); err != nil {
		t.Errorf("report-only removed the dead lock: %v", err)
	}

	// fix: removes only the dead lock; live + foreign untouched.
	fixFindings := reconcileStaleLocks(lm, cfg, repairMode{fix: true}, repos)
	if _, err := os.Stat(deadPath); !os.IsNotExist(err) {
		t.Errorf("dead lock should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(foreignPath); err != nil {
		t.Errorf("foreign lock must be preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lockDir, "livejob.lock")); err != nil {
		t.Errorf("live lock must be preserved: %v", err)
	}
	var applied bool
	for _, f := range fixFindings {
		if f.Class == driftStaleLock && f.Applied {
			applied = true
		}
	}
	if !applied {
		t.Errorf("expected an applied stale_lock removal; got %+v", fixFindings)
	}
}

// ---- class 6 : chain_broken ----

func TestDetectBrokenChains(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo := &repairRepo{label: "local", cfg: config.StorageConfig{Type: "local", LocalPath: dir}}

	writeManifest := func(name, backupID, chainID, baseline string, idx int) string {
		p := filepath.Join(dir, name+".manifest.json")
		m := ports.BackupManifest{
			BackupID: backupID,
			Hash:     ports.HashInfo{Algorithm: "sha256", Value: "deadbeef"},
			AdvancedRestore: &ports.AdvancedRestoreMetadata{
				IncrementalLineage: &ports.IncrementalLineageMetadata{
					Enabled: true, ChainID: chainID, ChainIndex: idx, BaselineBackupID: baseline,
				},
			},
		}
		body, _ := json.MarshalIndent(&m, "", "  ")
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		return p
	}

	// Intact chain: baseline idx0 + increment idx1, both manifests present.
	writeManifest("full.sql", "bk0", "chainOK", "bk0", 0)
	writeManifest("inc1.sql", "bk1", "chainOK", "bk0", 1)
	okRows := []ports.Execution{
		{ID: "bk0", BackupName: "job1", Status: ports.StatusSuccess, ChainID: "chainOK", ChainIndex: 0,
			FilePath: filepath.Join(dir, "full.sql"), ManifestPath: filepath.Join(dir, "full.sql.manifest.json")},
		{ID: "bk1", BackupName: "job1", Status: ports.StatusSuccess, ChainID: "chainOK", ChainIndex: 1,
			FilePath: filepath.Join(dir, "inc1.sql"), ManifestPath: filepath.Join(dir, "inc1.sql.manifest.json")},
	}
	if got := detectBrokenChains(ctx, okRows, repo); len(got) != 0 {
		t.Errorf("intact chain flagged broken: %+v", got)
	}

	// Broken chain: baseline present, idx1 missing, idx2 present (non-contiguous).
	writeManifest("full2.sql", "cb0", "chainBad", "cb0", 0)
	writeManifest("inc2.sql", "cb2", "chainBad", "cb0", 2)
	badRows := []ports.Execution{
		{ID: "cb0", BackupName: "job1", Status: ports.StatusSuccess, ChainID: "chainBad", ChainIndex: 0,
			FilePath: filepath.Join(dir, "full2.sql"), ManifestPath: filepath.Join(dir, "full2.sql.manifest.json")},
		{ID: "cb2", BackupName: "job1", Status: ports.StatusSuccess, ChainID: "chainBad", ChainIndex: 2,
			FilePath: filepath.Join(dir, "inc2.sql"), ManifestPath: filepath.Join(dir, "inc2.sql.manifest.json")},
	}
	got := detectBrokenChains(ctx, badRows, repo)
	if !hasClass(got, driftChainBroken) {
		t.Errorf("expected chain_broken for non-contiguous chain; got %+v", got)
	}
}

// ---- purge + active-baseline protection ----

func TestFilterProtectedOrphans(t *testing.T) {
	cands := []orphanCandidate{
		{objectPath: "base.sql", base: "base.sql"},
		{objectPath: "junk.sql", base: "junk.sql"},
	}
	// Active chain baseline is base.sql.
	records := []retention.BackupRecord{
		{FilePath: "base.sql", ChainID: "c1", BackupType: "full", ChainIndex: 0, Timestamp: time.Now()},
	}
	purge, protected := filterProtectedOrphans(cands, records)
	if len(protected) != 1 || protected[0].base != "base.sql" {
		t.Errorf("base.sql should be protected; protected=%+v", protected)
	}
	if len(purge) != 1 || purge[0].base != "junk.sql" {
		t.Errorf("junk.sql should be purgeable; purge=%+v", purge)
	}
}

func TestApplyOrphanPurge_ConfirmationAndProtection(t *testing.T) {
	ctx := context.Background()
	mb := storagetesting.NewMockBackend()
	mb.PutBytes("orphan.sql", []byte("x"))
	mb.PutBytes("base.sql", []byte("y"))

	cmd := &cobra.Command{}
	cmd.Flags().Bool("yes", true, "")

	newFindings := func() []repairFinding {
		return []repairFinding{
			{Class: driftOrphanArtifact, Path: "orphan.sql", Action: "report"},
			{Class: driftOrphanArtifact, Path: "base.sql", Action: "report"},
		}
	}
	cands := []orphanCandidate{
		{objectPath: "orphan.sql", base: "orphan.sql", backend: mb},
		{objectPath: "base.sql", base: "base.sql", backend: mb},
	}
	records := []retention.BackupRecord{
		{FilePath: "base.sql", ChainID: "c1", BackupType: "full", ChainIndex: 0, Timestamp: time.Now()},
	}

	// report-only: nothing deleted.
	_ = applyOrphanPurge(ctx, cmd, newFindings(), cands, records, repairMode{reportOnly: true})
	if _, ok := mb.GetBytes("orphan.sql"); !ok {
		t.Errorf("report-only must not delete")
	}

	// purge + yes: orphan deleted, protected baseline kept.
	out := applyOrphanPurge(ctx, cmd, newFindings(), cands, records, repairMode{purge: true, assumeYes: true})
	if _, ok := mb.GetBytes("orphan.sql"); ok {
		t.Errorf("orphan.sql should be deleted under --purge-orphans --yes")
	}
	if _, ok := mb.GetBytes("base.sql"); !ok {
		t.Errorf("active baseline base.sql must never be deleted")
	}
	if f, _ := findByPath(out, driftOrphanArtifact, "base.sql"); f.Action != "kept (protected active baseline)" {
		t.Errorf("base.sql action=%q want protected", f.Action)
	}
	if f, _ := findByPath(out, driftOrphanArtifact, "orphan.sql"); !f.Applied {
		t.Errorf("orphan.sql should be applied(deleted); action=%q", f.Action)
	}
}

func TestApplyOrphanPurge_NotConfirmed(t *testing.T) {
	ctx := context.Background()
	mb := storagetesting.NewMockBackend()
	mb.PutBytes("orphan.sql", []byte("x"))

	// Confirmation declined.
	orig := repairConfirm
	repairConfirm = func(_ io.Reader, _ io.Writer, _ string) bool { return false }
	defer func() { repairConfirm = orig }()

	cmd := &cobra.Command{}
	findings := []repairFinding{{Class: driftOrphanArtifact, Path: "orphan.sql", Action: "report"}}
	cands := []orphanCandidate{{objectPath: "orphan.sql", base: "orphan.sql", backend: mb}}

	out := applyOrphanPurge(ctx, cmd, findings, cands, nil, repairMode{purge: true})
	if _, ok := mb.GetBytes("orphan.sql"); !ok {
		t.Errorf("declined confirmation must not delete")
	}
	if f, _ := findByPath(out, driftOrphanArtifact, "orphan.sql"); f.Applied {
		t.Errorf("orphan.sql must not be applied when confirmation declined")
	}
}

// ---- exit-code contract ----

func TestRepairExitError(t *testing.T) {
	if err := repairExitError([]repairFinding{{Class: driftStaleLock}}); err != nil {
		t.Errorf("no manual-action classes → nil, got %v", err)
	}
	for _, cls := range []string{driftArtifactMissing, driftChainBroken} {
		err := repairExitError([]repairFinding{{Class: cls}})
		if Code(err) != 5 {
			t.Errorf("class %s should map to exit 5, got %d", cls, Code(err))
		}
	}
}

func TestSchemaRefusalMapping(t *testing.T) {
	cases := map[string]int{
		monitor.StatusForwardIncompatible: 2,
		monitor.StatusMissing:             3,
		monitor.StatusCorrupt:             4,
		monitor.StatusStalePending:        1,
	}
	for status, wantCode := range cases {
		if got := Code(schemaRefusalError(status)); got != wantCode {
			t.Errorf("status %s → exit %d, want %d", status, got, wantCode)
		}
	}
}

func TestRepair_RefusesCorruptSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	if err := os.WriteFile(dbPath, []byte("not a sqlite file"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	report, err := monitor.Diagnose(dbPath)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if report.Status == monitor.StatusCurrent {
		t.Fatalf("corrupt DB reported current")
	}
	if Code(schemaRefusalError(report.Status)) == 0 {
		t.Errorf("corrupt schema must map to a non-zero exit code")
	}
}

// ---- end-to-end runRepair: dry-run modifies nothing, fix applies safe set ----

func repairE2ESetup(t *testing.T) (*monitor.Monitor, *storagetesting.MockBackend, string, *config.Configuration) {
	t.Helper()
	mon := newRepairTestMonitor(t)
	lockDir := t.TempDir()
	cfg := repairTestCfg(lockDir)

	// monitor rows
	seedRow(t, mon, ports.Execution{ID: "ok1", BackupName: "job1", Status: ports.StatusSuccess, StorageBackend: "s3", FilePath: "s3://b/good.sql"})
	seedRow(t, mon, ports.Execution{ID: "gone1", BackupName: "job1", Status: ports.StatusSuccess, StorageBackend: "s3", FilePath: "s3://b/gone.sql"})
	seedRow(t, mon, ports.Execution{ID: "run1", BackupName: "job1", Status: ports.StatusRunning, StorageBackend: "s3", FilePath: "s3://b/partial.sql"})

	// storage
	mb := storagetesting.NewMockBackend()
	mb.PutBytes("good.sql", []byte("good"))                         // tracked
	mb.PutBytes("orphan.sql", []byte("orphan"))                     // orphan_artifact
	mb.PutBytes("lonely.sql.manifest.json", []byte(`{"x":1}`))      // orphan_manifest

	// stale lock (dead pid, old)
	host, _ := os.Hostname()
	writeLockJSON(t, lockDir, "stalejob", ports.JobLock{PID: 999999999, JobName: "stalejob", Hostname: host, StartTime: time.Now().Add(-2 * time.Hour)})

	return mon, mb, lockDir, cfg
}

func TestRunRepair_DryRunDetectsAllAndModifiesNothing(t *testing.T) {
	ctx := context.Background()
	mon, mb, lockDir, cfg := repairE2ESetup(t)
	withMockBackend(t, mb)
	lm := lock.NewManager(lockDir)

	cmd := &cobra.Command{}
	findings, err := runRepair(ctx, cmd, cfg, mon, lm, repairMode{reportOnly: true}, "")
	if err != nil {
		t.Fatalf("runRepair: %v", err)
	}

	for _, cls := range []string{driftOrphanArtifact, driftOrphanManifest, driftArtifactMissing, driftStaleRunning, driftStaleLock} {
		if !hasClass(findings, cls) {
			t.Errorf("dry-run missing class %s; findings=%+v", cls, findings)
		}
	}

	// Nothing modified.
	if _, ok := mb.GetBytes("orphan.sql"); !ok {
		t.Errorf("dry-run deleted orphan artifact")
	}
	if e, _ := mon.GetExecution(ctx, "run1"); e.Status != ports.StatusRunning {
		t.Errorf("dry-run finalized a running row: %s", e.Status)
	}
	if _, err := os.Stat(filepath.Join(lockDir, "stalejob.lock")); err != nil {
		t.Errorf("dry-run removed a stale lock: %v", err)
	}
	for _, f := range findings {
		if f.Applied {
			t.Errorf("dry-run applied a change: %+v", f)
		}
	}

	// Manual-action inconsistency remains → non-zero exit.
	if Code(repairExitError(findings)) != 5 {
		t.Errorf("expected exit 5 (artifact_missing present)")
	}
}

func TestRunRepair_FixAppliesSafeSetNotPurge(t *testing.T) {
	ctx := context.Background()
	mon, mb, lockDir, cfg := repairE2ESetup(t)
	withMockBackend(t, mb)
	lm := lock.NewManager(lockDir)

	cmd := &cobra.Command{}
	_, err := runRepair(ctx, cmd, cfg, mon, lm, repairMode{fix: true}, "")
	if err != nil {
		t.Fatalf("runRepair: %v", err)
	}

	if e, _ := mon.GetExecution(ctx, "run1"); e.Status != ports.StatusInterrupted {
		t.Errorf("fix should finalize run1, got %s", e.Status)
	}
	if _, err := os.Stat(filepath.Join(lockDir, "stalejob.lock")); !os.IsNotExist(err) {
		t.Errorf("fix should remove stale lock; stat err=%v", err)
	}
	// --fix must NOT delete orphan artifacts.
	if _, ok := mb.GetBytes("orphan.sql"); !ok {
		t.Errorf("--fix deleted an orphan artifact (should require --purge-orphans)")
	}
}

func TestRunRepair_PurgeDeletesOrphan(t *testing.T) {
	ctx := context.Background()
	mon, mb, lockDir, cfg := repairE2ESetup(t)
	withMockBackend(t, mb)
	lm := lock.NewManager(lockDir)

	cmd := &cobra.Command{}
	_, err := runRepair(ctx, cmd, cfg, mon, lm, repairMode{purge: true, assumeYes: true}, "")
	if err != nil {
		t.Fatalf("runRepair: %v", err)
	}
	if _, ok := mb.GetBytes("orphan.sql"); ok {
		t.Errorf("--purge-orphans --yes should delete orphan.sql")
	}
	// good.sql is tracked and must survive.
	if _, ok := mb.GetBytes("good.sql"); !ok {
		t.Errorf("tracked artifact good.sql must never be deleted")
	}
}
