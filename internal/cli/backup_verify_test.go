package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
	"github.com/spf13/cobra"
)

// swapVerifyBackend points newVerifyBackend at a fixed fake backend for the
// duration of a test, restoring the registry seam afterwards.
func swapVerifyBackend(t *testing.T, backend ports.StorageBackend) {
	t.Helper()
	prev := newVerifyBackend
	newVerifyBackend = func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return backend, nil
	}
	t.Cleanup(func() { newVerifyBackend = prev })
}

// seedRemoteExecution writes a config + history DB with one recorded s3 backup
// execution and returns the config path and the generated backup ID.
func seedRemoteExecution(t *testing.T, filePath string, size int64) (string, string) {
	t.Helper()
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeYAML(t, dir, "history_db_path: "+historyPath+`
databases:
  remote-job:
    type: postgres
    storage:
      type: s3
      s3_bucket: test-bucket
`)

	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	exec := &ports.Execution{
		BackupName:     "remote-job",
		DatabaseType:   "postgres",
		Timestamp:      time.Now().UTC(),
		Status:         "success",
		StorageBackend: "s3",
		FilePath:       filePath,
		FileSizeBytes:  size,
	}
	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		_ = mon.Close()
		t.Fatalf("record execution: %v", err)
	}
	_ = mon.Close()
	return cfgPath, exec.ID
}

// TestBackupVerifyRemoteFetchValidates asserts that verify DOWNLOADS a remote
// backup + its .manifest.json sidecar and validates the integrity hash, instead
// of failing to os.Open the remote key and reporting "no manifest".
func TestBackupVerifyRemoteFetchValidates(t *testing.T) {
	artifact := []byte("-- remote dump payload\n")
	sum := sha256.Sum256(artifact)
	hashValue := hex.EncodeToString(sum[:])

	manifestJSON, err := json.Marshal(ports.BackupManifest{
		BackupID: "remote-job",
		Hash: ports.HashInfo{
			Algorithm: "sha256",
			Value:     hashValue,
		},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	mb := storagetesting.NewMockBackend()
	mb.PutBytes("backup.sql", artifact)
	mb.PutBytes("backup.sql.manifest.json", manifestJSON)
	swapVerifyBackend(t, mb)

	cfgPath, backupID := seedRemoteExecution(t, "backup.sql", int64(len(artifact)))

	if err := runVerify(t, cfgPath, backupID); err != nil {
		t.Fatalf("verify of a valid remote backup should PASS, got: %v", err)
	}
}

// TestBackupVerifyRemoteFetchMissingManifestSkips asserts that a remote backup
// WITHOUT a manifest sidecar is tolerated: the artifact is downloaded, the
// sidecar is absent, and verify reports the existing "skipped" outcome rather
// than hard-failing.
func TestBackupVerifyRemoteFetchMissingManifestSkips(t *testing.T) {
	artifact := []byte("-- remote dump payload, no sidecar\n")

	mb := storagetesting.NewMockBackend()
	mb.PutBytes("backup.sql", artifact) // no .manifest.json seeded
	swapVerifyBackend(t, mb)

	cfgPath, backupID := seedRemoteExecution(t, "backup.sql", int64(len(artifact)))

	err := runVerify(t, cfgPath, backupID)
	if !errors.Is(err, ErrVerifySkipped) {
		t.Fatalf("remote backup with no manifest should be skipped, got: %v", err)
	}
}

// TestBackupVerifyRemoteFetchDetectsTampering asserts the integrity guarantee:
// when the downloaded remote artifact does not match the manifest hash, verify
// FAILS (does not silently pass).
func TestBackupVerifyRemoteFetchDetectsTampering(t *testing.T) {
	artifact := []byte("-- tampered remote payload\n")

	manifestJSON, err := json.Marshal(ports.BackupManifest{
		BackupID: "remote-job",
		Hash: ports.HashInfo{
			Algorithm: "sha256",
			Value:     "0000000000000000000000000000000000000000000000000000000000000000",
		},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	mb := storagetesting.NewMockBackend()
	mb.PutBytes("backup.sql", artifact)
	mb.PutBytes("backup.sql.manifest.json", manifestJSON)
	swapVerifyBackend(t, mb)

	cfgPath, backupID := seedRemoteExecution(t, "backup.sql", int64(len(artifact)))

	if err := runVerify(t, cfgPath, backupID); err == nil {
		t.Fatal("verify must FAIL on a hash mismatch for a remote backup, got nil")
	}
}

// ---------------------------------------------------------------------------
// backup verify --all sweep
// ---------------------------------------------------------------------------

// sweepFlags configures a runVerifyAll invocation.
type sweepFlags struct {
	all                   bool
	output                string
	since                 string
	job                   string
	ignoreMissingManifest bool
	args                  []string // positional args (a backup id for mutual-exclusion tests)
}

// runVerifyAll invokes backupVerifyCmd.RunE with the sweep flags set, capturing
// stdout. It returns the returned error and the captured stdout.
func runVerifyAll(t *testing.T, configPath string, f sweepFlags) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "verify-all-test"}
	cmd.Flags().String("config", configPath, "")
	cmd.Flags().String("output", "", "")
	cmd.Flags().String("format", "", "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().String("since", "", "")
	cmd.Flags().String("job", "", "")
	cmd.Flags().Bool("ignore-missing-manifest", false, "")
	cmd.Flags().Bool("allow-legacy-envelope", false, "")

	if f.all {
		_ = cmd.Flags().Set("all", "true")
	}
	if f.output != "" {
		_ = cmd.Flags().Set("output", f.output)
	}
	if f.since != "" {
		_ = cmd.Flags().Set("since", f.since)
	}
	if f.job != "" {
		_ = cmd.Flags().Set("job", f.job)
	}
	if f.ignoreMissingManifest {
		_ = cmd.Flags().Set("ignore-missing-manifest", "true")
	}

	var err error
	out := captureStdout(t, func() { err = backupVerifyCmd.RunE(cmd, f.args) })
	return out, err
}

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

// sweepSpec describes a backup to seed into a sweep repository.
type sweepSpec struct {
	file         string
	job          string
	seedArtifact bool   // put the artifact object in the backend
	seedManifest bool   // put the <file>.manifest.json sidecar
	manifestHash string // hash recorded in the manifest ("" ⇒ hash of the artifact bytes)
	olderThan    time.Duration
}

// seedSweepRepo writes a config + history DB with one recorded s3 execution per
// spec and seeds a MockBackend (swapped in via newVerifyBackend) with the
// requested artifacts + sidecars. It returns the config path and a map of
// file→backup id.
func seedSweepRepo(t *testing.T, specs []sweepSpec) (string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeYAML(t, dir, "history_db_path: "+historyPath+`
databases:
  sweep-job:
    type: postgres
    storage:
      type: s3
      s3_bucket: test-bucket
  other-job:
    type: postgres
    storage:
      type: s3
      s3_bucket: test-bucket
`)

	mb := storagetesting.NewMockBackend()
	swapVerifyBackend(t, mb)

	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	idByFile := make(map[string]string)
	for _, s := range specs {
		job := s.job
		if job == "" {
			job = "sweep-job"
		}
		payload := []byte("-- payload for " + s.file + "\n")
		if s.seedArtifact {
			mb.PutBytes(s.file, payload)
		}
		if s.seedManifest {
			hashVal := s.manifestHash
			if hashVal == "" {
				sum := sha256.Sum256(payload)
				hashVal = hex.EncodeToString(sum[:])
			}
			mj, mErr := json.Marshal(ports.BackupManifest{
				BackupID: s.file,
				Hash:     ports.HashInfo{Algorithm: "sha256", Value: hashVal},
			})
			if mErr != nil {
				_ = mon.Close()
				t.Fatalf("marshal manifest: %v", mErr)
			}
			mb.PutBytes(s.file+".manifest.json", mj)
		}
		ts := time.Now().UTC()
		if s.olderThan != 0 {
			ts = ts.Add(-s.olderThan)
		}
		exec := &ports.Execution{
			BackupName:     job,
			DatabaseType:   "postgres",
			Timestamp:      ts,
			Status:         "success",
			StorageBackend: "s3",
			FilePath:       s.file,
			FileSizeBytes:  int64(len(payload)),
		}
		if err := mon.RecordExecution(context.Background(), exec); err != nil {
			_ = mon.Close()
			t.Fatalf("record execution: %v", err)
		}
		idByFile[s.file] = exec.ID
	}
	_ = mon.Close()
	return cfgPath, idByFile
}

// sweepJSON is the machine-readable sweep output shape.
type sweepJSON struct {
	Results []struct {
		BackupID  string `json:"backup_id"`
		Job       string `json:"job"`
		Status    string `json:"status"`
		HashMatch bool   `json:"hash_match"`
		Error     string `json:"error"`
	} `json:"results"`
	Summary struct {
		Checked         int `json:"checked"`
		OK              int `json:"ok"`
		Corrupted       int `json:"corrupted"`
		MissingArtifact int `json:"missing_artifact"`
		MissingManifest int `json:"missing_manifest"`
		Errored         int `json:"errored"`
	} `json:"summary"`
}

// TestVerifyAll_AllOk asserts the happy path: every backup ok ⇒ exit 0, and the
// single-id path over a seeded backup is unchanged.
func TestVerifyAll_AllOk(t *testing.T) {
	cfgPath, ids := seedSweepRepo(t, []sweepSpec{
		{file: "a.sql", seedArtifact: true, seedManifest: true},
		{file: "b.sql", seedArtifact: true, seedManifest: true},
	})

	out, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "text"})
	if err != nil {
		t.Fatalf("all-ok sweep should return nil, got: %v", err)
	}
	if Code(err) != 0 {
		t.Fatalf("all-ok sweep exit code: want 0, got %d", Code(err))
	}
	if !strings.Contains(out, "2 checked · 2 ok") {
		t.Fatalf("summary line missing/wrong:\n%s", out)
	}

	// Single-id verify over the same seeded backup is unchanged (still PASS).
	if err := runVerify(t, cfgPath, ids["a.sql"]); err != nil {
		t.Fatalf("single-id verify of a valid backup should PASS, got: %v", err)
	}
}

// TestVerifyAll_FourStateMatrix asserts SC-004: corrupt/missing_artifact/
// missing_manifest are distinguished from ok, with correct summary counts, and
// the sweep exits with the integrity code.
func TestVerifyAll_FourStateMatrix(t *testing.T) {
	cfgPath, ids := seedSweepRepo(t, []sweepSpec{
		{file: "ok.sql", seedArtifact: true, seedManifest: true},
		{file: "corrupt.sql", seedArtifact: true, seedManifest: true, manifestHash: "0000000000000000000000000000000000000000000000000000000000000000"},
		{file: "gone.sql", seedArtifact: false, seedManifest: true},       // artifact absent
		{file: "nomanifest.sql", seedArtifact: true, seedManifest: false}, // sidecar absent
	})

	out, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "json"})
	if !errors.Is(err, ErrVerifyIntegrityFailed) {
		t.Fatalf("want ErrVerifyIntegrityFailed, got: %v", err)
	}
	if Code(err) != 5 {
		t.Fatalf("integrity exit code: want 5, got %d", Code(err))
	}

	var got sweepJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("sweep JSON parse: %v\noutput:\n%s", err, out)
	}

	statusByID := make(map[string]string)
	for _, r := range got.Results {
		statusByID[r.BackupID] = r.Status
	}
	want := map[string]string{
		ids["ok.sql"]:         verifyStatusOk,
		ids["corrupt.sql"]:    verifyStatusCorrupted,
		ids["gone.sql"]:       verifyStatusMissingArtifact,
		ids["nomanifest.sql"]: verifyStatusMissingManifest,
	}
	for id, wantStatus := range want {
		if statusByID[id] != wantStatus {
			t.Errorf("id %s: want status %q, got %q", id, wantStatus, statusByID[id])
		}
	}

	if got.Summary.Checked != 4 || got.Summary.OK != 1 || got.Summary.Corrupted != 1 ||
		got.Summary.MissingArtifact != 1 || got.Summary.MissingManifest != 1 {
		t.Fatalf("summary counts wrong: %+v", got.Summary)
	}
}

// TestVerifyAll_JSONShape asserts US3: --output json emits a results array + a
// summary object.
func TestVerifyAll_JSONShape(t *testing.T) {
	cfgPath, _ := seedSweepRepo(t, []sweepSpec{
		{file: "a.sql", seedArtifact: true, seedManifest: true},
	})
	out, _ := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "json"})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("json parse: %v\n%s", err, out)
	}
	if _, ok := raw["results"]; !ok {
		t.Fatalf("json missing 'results' array:\n%s", out)
	}
	if _, ok := raw["summary"]; !ok {
		t.Fatalf("json missing 'summary' object:\n%s", out)
	}
	var got sweepJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("typed parse: %v", err)
	}
	if len(got.Results) != 1 || got.Summary.Checked != 1 || got.Summary.OK != 1 {
		t.Fatalf("unexpected shape: results=%d summary=%+v", len(got.Results), got.Summary)
	}
}

// TestVerifyAll_OperationalVsIntegrity asserts SC-006: a backend that cannot be
// initialised is an OPERATIONAL error (ErrVerifyInternal, code 4), distinct
// from the integrity code (5).
func TestVerifyAll_OperationalVsIntegrity(t *testing.T) {
	cfgPath, _ := seedSweepRepo(t, []sweepSpec{
		{file: "a.sql", seedArtifact: true, seedManifest: true},
	})
	// Break the backend initialisation after seeding.
	prev := newVerifyBackend
	newVerifyBackend = func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return nil, errors.New("backend unreachable")
	}
	t.Cleanup(func() { newVerifyBackend = prev })

	_, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "text"})
	if !errors.Is(err, ErrVerifyInternal) {
		t.Fatalf("want ErrVerifyInternal (operational), got: %v", err)
	}
	if Code(err) != 4 {
		t.Fatalf("operational exit code: want 4, got %d", Code(err))
	}
}

// TestVerifyAll_IgnoreMissingManifest asserts A2: missing_manifest fails the
// sweep by default but is downgraded to exit 0 with --ignore-missing-manifest.
func TestVerifyAll_IgnoreMissingManifest(t *testing.T) {
	specs := []sweepSpec{
		{file: "ok.sql", seedArtifact: true, seedManifest: true},
		{file: "legacy.sql", seedArtifact: true, seedManifest: false}, // missing_manifest
	}

	cfgPath, _ := seedSweepRepo(t, specs)
	_, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "text"})
	if !errors.Is(err, ErrVerifyIntegrityFailed) {
		t.Fatalf("missing_manifest should fail by default, got: %v", err)
	}

	cfgPath2, _ := seedSweepRepo(t, specs)
	_, err = runVerifyAll(t, cfgPath2, sweepFlags{all: true, output: "text", ignoreMissingManifest: true})
	if err != nil {
		t.Fatalf("--ignore-missing-manifest should downgrade to exit 0, got: %v", err)
	}
}

// TestVerifyAll_Empty asserts the empty-repository edge: 0 checked ⇒ exit 0.
func TestVerifyAll_Empty(t *testing.T) {
	cfgPath, _ := seedSweepRepo(t, nil)
	out, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "text"})
	if err != nil {
		t.Fatalf("empty sweep should return nil, got: %v", err)
	}
	if !strings.Contains(out, "0 checked") {
		t.Fatalf("empty sweep should report '0 checked':\n%s", out)
	}
}

// TestVerifyAll_MutualExclusion asserts that both id+--all, or neither, is a
// usage error.
func TestVerifyAll_MutualExclusion(t *testing.T) {
	cfgPath, ids := seedSweepRepo(t, []sweepSpec{
		{file: "a.sql", seedArtifact: true, seedManifest: true},
	})

	// Both an id and --all.
	if _, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, args: []string{ids["a.sql"]}}); err == nil {
		t.Fatal("id + --all must be a usage error, got nil")
	}
	// Neither an id nor --all.
	if _, err := runVerifyAll(t, cfgPath, sweepFlags{all: false}); err == nil {
		t.Fatal("neither id nor --all must be a usage error, got nil")
	}
}

// TestVerifyAll_Since asserts that --since restricts the sweep to recent
// backups.
func TestVerifyAll_Since(t *testing.T) {
	cfgPath, _ := seedSweepRepo(t, []sweepSpec{
		{file: "recent.sql", seedArtifact: true, seedManifest: true, olderThan: 1 * time.Hour},
		{file: "old.sql", seedArtifact: true, seedManifest: true, olderThan: 60 * 24 * time.Hour},
	})

	out, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "json", since: "30d"})
	if err != nil {
		t.Fatalf("sweep --since 30d should return nil (only recent ok), got: %v", err)
	}
	var got sweepJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json parse: %v\n%s", err, out)
	}
	if got.Summary.Checked != 1 || got.Summary.OK != 1 {
		t.Fatalf("--since 30d should check exactly the recent backup, got %+v", got.Summary)
	}
}

// TestVerifyAll_Job asserts that --job restricts the sweep to a single job.
func TestVerifyAll_Job(t *testing.T) {
	cfgPath, _ := seedSweepRepo(t, []sweepSpec{
		{file: "a.sql", job: "sweep-job", seedArtifact: true, seedManifest: true},
		{file: "b.sql", job: "other-job", seedArtifact: true, seedManifest: true},
	})

	out, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "json", job: "sweep-job"})
	if err != nil {
		t.Fatalf("scoped sweep should return nil, got: %v", err)
	}
	var got sweepJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if got.Summary.Checked != 1 {
		t.Fatalf("--job sweep-job should check exactly 1 backup, got %d", got.Summary.Checked)
	}
	if len(got.Results) != 1 || got.Results[0].Job != "sweep-job" {
		t.Fatalf("--job scoping wrong: %+v", got.Results)
	}
}

// TestVerifyAll_ReadOnly asserts that the sweep writes NO new history rows.
func TestVerifyAll_ReadOnly(t *testing.T) {
	cfgPath, _ := seedSweepRepo(t, []sweepSpec{
		{file: "a.sql", seedArtifact: true, seedManifest: true},
		{file: "b.sql", seedArtifact: true, seedManifest: true},
	})

	countRows := func() int {
		cfg, err := readSweepConfig(t, cfgPath)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		mon, err := monitor.NewMonitor(cfg)
		if err != nil {
			t.Fatalf("open monitor: %v", err)
		}
		defer mon.Close()
		rows, err := mon.ListExecutions(context.Background(), &ports.Filter{}, 100000, 0)
		if err != nil {
			t.Fatalf("list executions: %v", err)
		}
		return len(rows)
	}

	before := countRows()
	if _, err := runVerifyAll(t, cfgPath, sweepFlags{all: true, output: "text"}); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	after := countRows()
	if before != after {
		t.Fatalf("sweep must not write history rows: before=%d after=%d", before, after)
	}
}

// readSweepConfig loads a config and returns its history DB path (helper for the
// read-only assertion).
func readSweepConfig(t *testing.T, cfgPath string) (string, error) {
	t.Helper()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "history_db_path:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "history_db_path:")), nil
		}
	}
	return "", errors.New("history_db_path not found")
}
