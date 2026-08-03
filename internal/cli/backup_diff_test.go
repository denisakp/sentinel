package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

// diffExec describes one backup to seed for a diff test: its monitor-row fields
// plus (optionally) a local manifest sidecar written next to a NON-EXISTENT
// artifact path. The artifact file is deliberately never created, so a passing
// test proves diff reads zero artifact bytes.
type diffExec struct {
	job           string
	fileBase      string // artifact basename (no file is written for it)
	durationMs    int64
	sizeBytes     int64
	backupType    string
	chainIndex    int
	writeManifest bool
	manifest      ports.BackupManifest // used when writeManifest is true
}

// seedDiffRepo writes a config + history DB with one recorded LOCAL execution
// per spec and (for those requesting it) a manifest sidecar at
// <dir>/<fileBase>.manifest.json. The backup artifact itself is never written.
// Returns the config path and a map fileBase→backup id.
func seedDiffRepo(t *testing.T, execs []diffExec) (string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeYAML(t, dir, "history_db_path: "+historyPath+`
databases:
  diff-job:
    type: postgres
    storage:
      type: local
`)

	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	idByFile := make(map[string]string)
	for _, e := range execs {
		job := e.job
		if job == "" {
			job = "diff-job"
		}
		artifactPath := filepath.Join(dir, e.fileBase)

		if e.writeManifest {
			m := e.manifest
			if m.BackupID == "" {
				m.BackupID = e.fileBase
			}
			if m.Hash.Algorithm == "" {
				m.Hash.Algorithm = "sha256"
			}
			if m.Hash.Value == "" {
				m.Hash.Value = "deadbeef" + e.fileBase
			}
			if err := manifest.WriteManifest(artifactPath+".manifest.json", &m); err != nil {
				_ = mon.Close()
				t.Fatalf("write manifest: %v", err)
			}
		}

		exec := &ports.Execution{
			BackupName:     job,
			DatabaseType:   "postgres",
			Timestamp:      time.Now().UTC(),
			Status:         "success",
			StorageBackend: "local",
			FilePath:       artifactPath, // no such file on disk (never opened)
			FileSizeBytes:  e.sizeBytes,
			DurationMs:     e.durationMs,
			BackupType:     e.backupType,
			ChainIndex:     e.chainIndex,
		}
		if err := mon.RecordExecution(context.Background(), exec); err != nil {
			_ = mon.Close()
			t.Fatalf("record execution: %v", err)
		}
		idByFile[e.fileBase] = exec.ID
	}
	_ = mon.Close()
	return cfgPath, idByFile
}

// runDiff invokes backupDiffCmd.RunE with the two ids and the given output
// format, capturing stdout. Mirrors runVerify/runVerifyAll.
func runDiff(t *testing.T, cfgPath, output, id1, id2 string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "diff-test"}
	cmd.Flags().String("config", cfgPath, "")
	cmd.Flags().String("output", output, "")
	cmd.Flags().String("format", "", "")

	var err error
	out := captureStdout(t, func() { err = backupDiffCmd.RunE(cmd, []string{id1, id2}) })
	return out, err
}

// diffJSON mirrors the machine-readable diff shape.
type diffJSON struct {
	ID1         string `json:"id1"`
	ID2         string `json:"id2"`
	Differences []struct {
		Field    string `json:"field"`
		Before   string `json:"before"`
		After    string `json:"after"`
		Delta    string `json:"delta"`
		Security bool   `json:"security"`
		Warn     bool   `json:"warn"`
	} `json:"differences"`
	SecurityRegressions []string `json:"security_regressions"`
	Warnings            []string `json:"warnings"`
}

func parseDiffJSON(t *testing.T, out string) diffJSON {
	t.Helper()
	var d diffJSON
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse diff JSON: %v\noutput:\n%s", err, out)
	}
	return d
}

func fieldByName(d diffJSON, name string) (int, bool) {
	for i, f := range d.Differences {
		if f.Field == name {
			return i, true
		}
	}
	return 0, false
}

// TestBackupDiff_EncryptionDisabledFlagged asserts the headline signal:
// encryption present → absent yields ENCRYPTION DISABLED and a non-zero exit.
func TestBackupDiff_EncryptionDisabledFlagged(t *testing.T) {
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "enc.sql", sizeBytes: 1000, durationMs: 100, writeManifest: true, manifest: ports.BackupManifest{
			Hash:       ports.HashInfo{Algorithm: "sha256", Value: "aaaa"},
			Encryption: &ports.EncryptionInfo{Algorithm: "aes-256-gcm", KeyDerivation: "pbkdf2", Iterations: 600000, EnvelopeVersion: 2},
		}},
		{fileBase: "plain.sql", sizeBytes: 1000, durationMs: 100, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "bbbb"},
		}},
	})

	out, err := runDiff(t, cfgPath, "json", ids["enc.sql"], ids["plain.sql"])
	if !errors.Is(err, ErrDiffSecurityRegression) {
		t.Fatalf("encryption on→off must be a security regression, got: %v", err)
	}
	if Code(err) == 0 {
		t.Fatalf("security regression must exit non-zero, got code 0")
	}

	d := parseDiffJSON(t, out)
	i, ok := fieldByName(d, "encryption")
	if !ok {
		t.Fatalf("expected an 'encryption' diff field:\n%s", out)
	}
	if !d.Differences[i].Security {
		t.Fatalf("encryption field must be flagged security=true")
	}
	if !containsStr(d.SecurityRegressions, "ENCRYPTION DISABLED") {
		t.Fatalf("expected ENCRYPTION DISABLED in %v", d.SecurityRegressions)
	}
}

// TestBackupDiff_HashAlgoChangeFlagged asserts a hash-algorithm change is a
// security regression with a non-zero exit.
func TestBackupDiff_HashAlgoChangeFlagged(t *testing.T) {
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "a.sql", sizeBytes: 500, durationMs: 50, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "1111"},
		}},
		{fileBase: "b.sql", sizeBytes: 500, durationMs: 50, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha1", Value: "2222"},
		}},
	})

	out, err := runDiff(t, cfgPath, "json", ids["a.sql"], ids["b.sql"])
	if !errors.Is(err, ErrDiffSecurityRegression) {
		t.Fatalf("hash-algo change must be a security regression, got: %v", err)
	}
	d := parseDiffJSON(t, out)
	if i, ok := fieldByName(d, "hash.algorithm"); !ok || !d.Differences[i].Security {
		t.Fatalf("hash.algorithm must be a flagged security field:\n%s", out)
	}
	if !containsStr(d.SecurityRegressions, "HASH ALGORITHM CHANGED") {
		t.Fatalf("expected HASH ALGORITHM CHANGED in %v", d.SecurityRegressions)
	}
}

// TestBackupDiff_EncryptionWeakened asserts a parameter downgrade (iterations
// decreased) is flagged ENCRYPTION WEAKENED with a non-zero exit.
func TestBackupDiff_EncryptionWeakened(t *testing.T) {
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "strong.sql", sizeBytes: 1, durationMs: 1, writeManifest: true, manifest: ports.BackupManifest{
			Hash:       ports.HashInfo{Algorithm: "sha256", Value: "x"},
			Encryption: &ports.EncryptionInfo{Algorithm: "aes-256-gcm", KeyDerivation: "pbkdf2", Iterations: 600000, EnvelopeVersion: 2},
		}},
		{fileBase: "weak.sql", sizeBytes: 1, durationMs: 1, writeManifest: true, manifest: ports.BackupManifest{
			Hash:       ports.HashInfo{Algorithm: "sha256", Value: "y"},
			Encryption: &ports.EncryptionInfo{Algorithm: "aes-256-gcm", KeyDerivation: "pbkdf2", Iterations: 100000, EnvelopeVersion: 2},
		}},
	})

	out, err := runDiff(t, cfgPath, "json", ids["strong.sql"], ids["weak.sql"])
	if !errors.Is(err, ErrDiffSecurityRegression) {
		t.Fatalf("iterations decrease must be a security regression, got: %v", err)
	}
	d := parseDiffJSON(t, out)
	if i, ok := fieldByName(d, "encryption.iterations"); !ok || !d.Differences[i].Security {
		t.Fatalf("encryption.iterations must be a flagged security field:\n%s", out)
	}
	if !containsStr(d.SecurityRegressions, "ENCRYPTION WEAKENED") {
		t.Fatalf("expected ENCRYPTION WEAKENED in %v", d.SecurityRegressions)
	}
}

// TestBackupDiff_SizeAndDurationDeltas asserts size/duration render with correct
// before/after/percent and DO NOT trip the exit code (no security regression).
func TestBackupDiff_SizeAndDurationDeltas(t *testing.T) {
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "small.sql", sizeBytes: 1000, durationMs: 100, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "same"},
		}},
		{fileBase: "big.sql", sizeBytes: 3000, durationMs: 300, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "same"},
		}},
	})

	out, err := runDiff(t, cfgPath, "json", ids["small.sql"], ids["big.sql"])
	if err != nil {
		t.Fatalf("size/duration swings alone must exit 0, got: %v", err)
	}
	d := parseDiffJSON(t, out)

	si, ok := fieldByName(d, "size_bytes")
	if !ok {
		t.Fatalf("expected size_bytes diff:\n%s", out)
	}
	if !strings.Contains(d.Differences[si].Delta, "+200%") {
		t.Fatalf("size delta want +200%%, got %q", d.Differences[si].Delta)
	}
	if d.Differences[si].Security {
		t.Fatalf("size_bytes must not be a security field")
	}

	di, ok := fieldByName(d, "duration_ms")
	if !ok {
		t.Fatalf("expected duration_ms diff:\n%s", out)
	}
	if d.Differences[di].Before != "100" || d.Differences[di].After != "300" {
		t.Fatalf("duration before/after want 100/300, got %s/%s", d.Differences[di].Before, d.Differences[di].After)
	}
	if !strings.Contains(d.Differences[di].Delta, "+200%") {
		t.Fatalf("duration delta want +200%%, got %q", d.Differences[di].Delta)
	}
	if len(d.SecurityRegressions) != 0 {
		t.Fatalf("no security regression expected, got %v", d.SecurityRegressions)
	}
}

// TestBackupDiff_IdenticalEmpty asserts identical metadata ⇒ empty diff + exit 0.
func TestBackupDiff_IdenticalEmpty(t *testing.T) {
	m := ports.BackupManifest{
		Hash:       ports.HashInfo{Algorithm: "sha256", Value: "identical"},
		Encryption: &ports.EncryptionInfo{Algorithm: "aes-256-gcm", KeyDerivation: "pbkdf2", Iterations: 600000, EnvelopeVersion: 2},
	}
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "one.sql", sizeBytes: 2048, durationMs: 120, backupType: "full", writeManifest: true, manifest: m},
		{fileBase: "two.sql", sizeBytes: 2048, durationMs: 120, backupType: "full", writeManifest: true, manifest: m},
	})

	out, err := runDiff(t, cfgPath, "json", ids["one.sql"], ids["two.sql"])
	if err != nil {
		t.Fatalf("identical backups must exit 0, got: %v", err)
	}
	d := parseDiffJSON(t, out)
	if len(d.Differences) != 0 {
		t.Fatalf("identical backups must yield an empty diff, got: %+v", d.Differences)
	}
	if len(d.SecurityRegressions) != 0 {
		t.Fatalf("identical backups must have no regressions, got: %v", d.SecurityRegressions)
	}

	// Text mode should say there are no differences.
	textOut, textErr := runDiff(t, cfgPath, "text", ids["one.sql"], ids["two.sql"])
	if textErr != nil {
		t.Fatalf("identical text diff must exit 0, got: %v", textErr)
	}
	if !strings.Contains(textOut, "No metadata differences") {
		t.Fatalf("text output should report no differences:\n%s", textOut)
	}
}

// TestBackupDiff_MissingManifestPartial asserts one side missing a manifest ⇒
// partial diff + warning, no crash, exit 0 (no security regression can be
// computed). It ALSO proves zero artifact bytes are read: neither artifact file
// exists on disk, yet the diff succeeds.
func TestBackupDiff_MissingManifestPartial(t *testing.T) {
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "hasman.sql", sizeBytes: 1000, durationMs: 100, backupType: "full", writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "hh"},
		}},
		{fileBase: "noman.sql", sizeBytes: 5000, durationMs: 500, backupType: "incremental", writeManifest: false},
	})

	out, err := runDiff(t, cfgPath, "json", ids["hasman.sql"], ids["noman.sql"])
	if err != nil {
		t.Fatalf("missing manifest must not fail the diff, got: %v", err)
	}
	d := parseDiffJSON(t, out)
	if len(d.Warnings) == 0 {
		t.Fatalf("expected a warning about the missing manifest:\n%s", out)
	}
	if len(d.SecurityRegressions) != 0 {
		t.Fatalf("no security regression can be computed with a missing manifest, got: %v", d.SecurityRegressions)
	}
	// Monitor-row fields still compared (size/duration/backup_type differ).
	if _, ok := fieldByName(d, "size_bytes"); !ok {
		t.Fatalf("partial diff must still compare size_bytes:\n%s", out)
	}
	if _, ok := fieldByName(d, "backup_type"); !ok {
		t.Fatalf("partial diff must still compare backup_type:\n%s", out)
	}
	// hash/encryption fields must be ABSENT (needs both manifests).
	if _, ok := fieldByName(d, "hash.value"); ok {
		t.Fatalf("hash.value must not appear when a manifest is missing")
	}
}

// TestBackupDiff_ZeroArtifactBytesRead is the explicit exit-criterion assertion:
// the artifact files are never created, so any attempt to open FilePath would
// fail. The diff succeeds, proving only the row + sidecar are read.
func TestBackupDiff_ZeroArtifactBytesRead(t *testing.T) {
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "ghost1.sql", sizeBytes: 10, durationMs: 10, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "g1"},
		}},
		{fileBase: "ghost2.sql", sizeBytes: 20, durationMs: 20, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "g2"},
		}},
	})

	if _, err := runDiff(t, cfgPath, "json", ids["ghost1.sql"], ids["ghost2.sql"]); err != nil {
		t.Fatalf("diff must succeed without any artifact bytes present, got: %v", err)
	}
}

// TestBackupDiff_BackupTypeAndChainDepth asserts the monitor-row-first
// full↔incremental signal and chain-depth reset render as diff fields.
func TestBackupDiff_BackupTypeAndChainDepth(t *testing.T) {
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "inc.sql", sizeBytes: 100, durationMs: 10, backupType: "incremental", chainIndex: 3, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "i"},
		}},
		{fileBase: "full.sql", sizeBytes: 100, durationMs: 10, backupType: "full", chainIndex: 0, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "f"},
		}},
	})

	out, err := runDiff(t, cfgPath, "json", ids["inc.sql"], ids["full.sql"])
	if err != nil {
		t.Fatalf("backup_type/chain_depth diff must exit 0, got: %v", err)
	}
	d := parseDiffJSON(t, out)
	bt, ok := fieldByName(d, "backup_type")
	if !ok || d.Differences[bt].Before != "incremental" || d.Differences[bt].After != "full" {
		t.Fatalf("backup_type diff wrong:\n%s", out)
	}
	cd, ok := fieldByName(d, "chain_depth")
	if !ok || d.Differences[cd].Delta != "reset" {
		t.Fatalf("chain_depth reset diff wrong:\n%s", out)
	}
}

// TestBackupDiff_NotFound asserts a missing id maps to ErrVerifyNotFound (exit 2).
func TestBackupDiff_NotFound(t *testing.T) {
	cfgPath, ids := seedDiffRepo(t, []diffExec{
		{fileBase: "real.sql", sizeBytes: 1, durationMs: 1, writeManifest: true, manifest: ports.BackupManifest{
			Hash: ports.HashInfo{Algorithm: "sha256", Value: "r"},
		}},
	})

	_, err := runDiff(t, cfgPath, "text", ids["real.sql"], "does-not-exist")
	if !errors.Is(err, ErrVerifyNotFound) {
		t.Fatalf("missing id must map to ErrVerifyNotFound, got: %v", err)
	}
	if Code(err) != 2 {
		t.Fatalf("not-found exit code want 2, got %d", Code(err))
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
