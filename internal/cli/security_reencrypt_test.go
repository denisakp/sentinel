package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/restore/runtime"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

// --- test harness ----------------------------------------------------------

const reencKeyEnv = "SENTINEL_TEST_REENCRYPT_KEY"
const reencKeyEnvB = "SENTINEL_TEST_REENCRYPT_KEY_B"

// reencFlags configures a runReencrypt invocation.
type reencFlags struct {
	all          bool
	job          string
	since        string
	mode         string
	newKeyEnv    string
	keepOriginal bool
	yes          bool
	dryRun       bool
	output       string
	args         []string
}

// runReencrypt invokes securityReencryptCmd.RunE with the given flags, capturing
// stdout + stderr. It returns (stdout, stderr, err).
func runReencrypt(t *testing.T, cfgPath string, f reencFlags) (string, string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "reencrypt-test"}
	cmd.Flags().String("config", cfgPath, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().String("job", "", "")
	cmd.Flags().String("since", "", "")
	cmd.Flags().String("mode", "legacy", "")
	cmd.Flags().String("new-key-env", "", "")
	cmd.Flags().Bool("keep-original", false, "")
	cmd.Flags().Bool("yes", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().String("output", "", "")

	if f.all {
		_ = cmd.Flags().Set("all", "true")
	}
	if f.job != "" {
		_ = cmd.Flags().Set("job", f.job)
	}
	if f.since != "" {
		_ = cmd.Flags().Set("since", f.since)
	}
	if f.mode != "" {
		_ = cmd.Flags().Set("mode", f.mode)
	}
	if f.newKeyEnv != "" {
		_ = cmd.Flags().Set("new-key-env", f.newKeyEnv)
	}
	if f.keepOriginal {
		_ = cmd.Flags().Set("keep-original", "true")
	}
	if f.yes {
		_ = cmd.Flags().Set("yes", "true")
	}
	if f.dryRun {
		_ = cmd.Flags().Set("dry-run", "true")
	}
	if f.output != "" {
		_ = cmd.Flags().Set("output", f.output)
	}

	var err error
	stdout, stderr := captureStdoutStderr(t, func() { err = securityReencryptCmd.RunE(cmd, f.args) })
	return stdout, stderr, err
}

// captureStdoutStderr redirects both os.Stdout and os.Stderr for the duration
// of fn and returns what was written to each.
func captureStdoutStderr(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr

	outCh, errCh := make(chan string, 1), make(chan string, 1)
	go func() { var b bytes.Buffer; _, _ = io.Copy(&b, rOut); outCh <- b.String() }()
	go func() { var b bytes.Buffer; _, _ = io.Copy(&b, rErr); errCh <- b.String() }()

	fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-outCh, <-errCh
}

// testMasterKey sets keyEnv to a fresh random base64 32-byte key for the test
// and returns the raw key bytes.
func testMasterKey(t *testing.T, keyEnv string) []byte {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte((i*7 + len(keyEnv)) % 251)
	}
	t.Setenv(keyEnv, base64.StdEncoding.EncodeToString(raw))
	return raw
}

// readRowSecurity reads the security columns for a backup row directly (they
// are persisted by RecordSecurityInfo but not projected by GetExecution).
func readRowSecurity(t *testing.T, historyPath, id string) (string, bool) {
	t.Helper()
	db, err := sql.Open("sqlite", historyPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var hashValue string
	var encrypted int
	if err := db.QueryRow(`SELECT hash_value, encrypted FROM backup_executions WHERE id = ?`, id).
		Scan(&hashValue, &encrypted); err != nil {
		t.Fatalf("read row security: %v", err)
	}
	return hashValue, encrypted == 1
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// writeReencCfg writes a minimal config referencing keyEnv and a local job.
func writeReencCfg(t *testing.T, dir, historyPath, keyEnv string) string {
	t.Helper()
	return writeYAML(t, dir, "history_db_path: "+historyPath+`
encryption_key_env: `+keyEnv+`
databases:
  job1:
    type: postgres
    storage:
      type: local
`)
}

// seedLocalExecution records a successful local backup row and returns its id.
func seedLocalExecution(t *testing.T, historyPath, job, artifactPath string, size int64) string {
	t.Helper()
	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	defer mon.Close()
	exec := &ports.Execution{
		BackupName:     job,
		DatabaseType:   "postgres",
		Timestamp:      time.Now().UTC(),
		Status:         ports.StatusSuccess,
		StorageBackend: "local",
		FilePath:       artifactPath,
		FileSizeBytes:  size,
	}
	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		t.Fatalf("record execution: %v", err)
	}
	return exec.ID
}

// writeV2Backup writes plaintext to path, encrypts it in place under keyEnv
// (fresh v2 envelope), and writes the manifest sidecar. Returns the manifest
// backup id (AAD).
func writeV2Backup(t *testing.T, path, keyEnv string, plaintext []byte, backupID string) {
	t.Helper()
	if err := os.WriteFile(path, plaintext, 0o600); err != nil {
		t.Fatalf("write plaintext: %v", err)
	}
	plainHash := sha256hex(plaintext)
	cfg := &config.Configuration{EncryptionKeyEnv: keyEnv}
	_, encInfo, encHash, err := encryptBackupFile(cfg, path, backupID)
	if err != nil {
		t.Fatalf("encrypt v2 backup: %v", err)
	}
	m := &ports.BackupManifest{
		BackupID:     backupID,
		Database:     "job1",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC(),
		Hash:         ports.HashInfo{Algorithm: "sha256", Value: encHash, PlaintextValue: plainHash},
		Encryption:   encInfo,
	}
	if err := manifest.WriteManifest(path+".manifest.json", m); err != nil {
		t.Fatalf("write v2 manifest: %v", err)
	}
}

// writeLegacyBackup writes a pre-v2 (legacy, header-stripped) encrypted artifact
// to path under keyEnv and its manifest (EnvelopeVersion absent). Mirrors the
// crypto package's writeLegacyV1 test helper.
func writeLegacyBackup(t *testing.T, path, keyEnv string, plaintext []byte, backupID string) {
	t.Helper()
	master := os.Getenv(keyEnv)
	masterKey, err := base64.StdEncoding.DecodeString(master)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	salt, err := crypto.GenerateSalt()
	if err != nil {
		t.Fatalf("salt: %v", err)
	}
	derived := crypto.DeriveKey(masterKey, salt)

	var buf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&buf, derived, backupID)
	if err != nil {
		t.Fatalf("enc writer: %v", err)
	}
	if _, err := enc.Write(plaintext); err != nil {
		t.Fatalf("enc write: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("enc flush: %v", err)
	}
	baseNonce := enc.BaseNonce()
	authTag := enc.LastAuthTag()

	full := buf.Bytes()
	if len(full) < 5 {
		t.Fatalf("encrypt produced %d bytes", len(full))
	}
	legacy := append([]byte{}, full[5:]...) // strip the 5-byte v2 header
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy artifact: %v", err)
	}

	m := &ports.BackupManifest{
		BackupID:     backupID,
		Database:     "job1",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC(),
		Hash:         ports.HashInfo{Algorithm: "sha256", Value: sha256hex(legacy), PlaintextValue: sha256hex(plaintext)},
		Encryption: &ports.EncryptionInfo{
			Algorithm:       "AES-256-GCM",
			KeyDerivation:   "PBKDF2-HMAC-SHA256",
			Iterations:      100_000,
			Salt:            base64.StdEncoding.EncodeToString(salt),
			IV:              hex.EncodeToString(baseNonce),
			AuthTag:         hex.EncodeToString(authTag),
			EnvelopeVersion: 0, // legacy: no v2 marker
		},
	}
	if err := manifest.WriteManifest(path+".manifest.json", m); err != nil {
		t.Fatalf("write legacy manifest: %v", err)
	}
}

// decryptArtifact decrypts the artifact at path under keyEnv using its manifest.
func decryptArtifact(t *testing.T, path, keyEnv string, allowLegacy bool) ([]byte, error) {
	t.Helper()
	m, err := manifest.ReadManifest(path + ".manifest.json")
	if err != nil {
		return nil, err
	}
	kp := &crypto.FileKeyProvider{EnvVar: keyEnv}
	r, err := runtime.PreRestoreVerifyAndDecryptWithOptions(context.Background(), m, path, kp,
		ports.DecryptOptions{AllowLegacy: allowLegacy, BackupID: m.BackupID, Source: path})
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// recordingHandler captures slog records for audit-event assertions.
type recordingHandler struct {
	mu   *sync.Mutex
	msgs *[]string
}

func (h recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.msgs = append(*h.msgs, r.Message)
	return nil
}
func (h recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordingHandler) WithGroup(string) slog.Handler      { return h }

// captureSlog installs a recording slog handler for the test and returns a
// function yielding the count of EventReencrypt events seen.
func captureSlog(t *testing.T) func() int {
	t.Helper()
	var mu sync.Mutex
	var msgs []string
	prev := slog.Default()
	slog.SetDefault(slog.New(recordingHandler{mu: &mu, msgs: &msgs}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, m := range msgs {
			if m == EventReencrypt {
				n++
			}
		}
		return n
	}
}

// --- T012 / SC-001: legacy -> migrated, verifies under default policy -------

func TestReencrypt_LegacyMigratesAndVerifies(t *testing.T) {
	keyEnv := reencKeyEnv
	testMasterKey(t, keyEnv)
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeReencCfg(t, dir, historyPath, keyEnv)

	plaintext := []byte("-- legacy dump payload\nSELECT 1;\n")
	artifact := filepath.Join(dir, "legacy.sql")
	writeLegacyBackup(t, artifact, keyEnv, plaintext, "job1-legacy")
	id := seedLocalExecution(t, historyPath, "job1", artifact, int64(0))

	events := captureSlog(t)
	_, _, err := runReencrypt(t, cfgPath, reencFlags{args: []string{id}, mode: "legacy", output: "text"})
	if err != nil {
		t.Fatalf("legacy migrate should succeed, got: %v (code %d)", err, Code(err))
	}

	// Manifest now advertises the current envelope.
	m, err := manifest.ReadManifest(artifact + ".manifest.json")
	if err != nil {
		t.Fatalf("read migrated manifest: %v", err)
	}
	if m.Encryption == nil || m.Encryption.EnvelopeVersion != 2 {
		t.Fatalf("migrated manifest EnvelopeVersion: want 2, got %+v", m.Encryption)
	}

	// Decrypts under the DEFAULT policy (no AllowLegacy) and round-trips.
	got, err := decryptArtifact(t, artifact, keyEnv, false)
	if err != nil {
		t.Fatalf("migrated artifact must decrypt under default policy: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch after migrate: got %q", got)
	}

	// Row hash updated to the new artifact hash (FR-010). GetExecution does not
	// project the security columns, so read them directly.
	rowHash, rowEncrypted := readRowSecurity(t, historyPath, id)
	if rowHash != m.Hash.Value {
		t.Fatalf("row hash not updated: row=%s manifest=%s", rowHash, m.Hash.Value)
	}
	if !rowEncrypted {
		t.Fatalf("row should be marked encrypted")
	}

	// Exactly one audit event for one success (SC-010).
	if n := events(); n != 1 {
		t.Fatalf("want exactly 1 security.reencrypt event, got %d", n)
	}
}

// --- T013: idempotency + skip-with-reason ----------------------------------

func TestReencrypt_IdempotentAndSkips(t *testing.T) {
	keyEnv := reencKeyEnv
	testMasterKey(t, keyEnv)
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeReencCfg(t, dir, historyPath, keyEnv)

	// current (v2), unencrypted (manifest w/o Encryption), unmigratable (no manifest)
	current := filepath.Join(dir, "current.sql")
	writeV2Backup(t, current, keyEnv, []byte("current payload"), "job1-current")
	curID := seedLocalExecution(t, historyPath, "job1", current, 0)

	plainArt := filepath.Join(dir, "plain.sql")
	if err := os.WriteFile(plainArt, []byte("plain payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	pm := &ports.BackupManifest{
		BackupID: "job1-plain", Database: "job1", DatabaseType: "postgres",
		Hash: ports.HashInfo{Algorithm: "sha256", Value: sha256hex([]byte("plain payload"))},
	}
	if err := manifest.WriteManifest(plainArt+".manifest.json", pm); err != nil {
		t.Fatal(err)
	}
	plainID := seedLocalExecution(t, historyPath, "job1", plainArt, 0)

	noManifest := filepath.Join(dir, "old.sql")
	if err := os.WriteFile(noManifest, []byte("ancient payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	noManID := seedLocalExecution(t, historyPath, "job1", noManifest, 0)

	beforeCurrent, _ := os.ReadFile(current)

	out, _, err := runReencrypt(t, cfgPath, reencFlags{all: true, yes: true, mode: "legacy", output: "json"})
	if err != nil {
		t.Fatalf("legacy sweep over non-legacy set should exit 0, got: %v (code %d)", err, Code(err))
	}

	var got struct {
		Results []reencryptResult `json:"results"`
		Summary reencryptSummary  `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json parse: %v\n%s", err, out)
	}
	byID := map[string]string{}
	for _, r := range got.Results {
		byID[r.BackupID] = r.Outcome
	}
	if byID[curID] != outSkippedCurrent {
		t.Errorf("current backup: want %s, got %s", outSkippedCurrent, byID[curID])
	}
	if byID[plainID] != outSkippedUnencrypted {
		t.Errorf("unencrypted backup: want %s, got %s", outSkippedUnencrypted, byID[plainID])
	}
	if byID[noManID] != outSkippedUnmigratable {
		t.Errorf("unmigratable backup: want %s, got %s", outSkippedUnmigratable, byID[noManID])
	}
	if got.Summary.Skipped != 3 || got.Summary.Migrated != 0 || got.Summary.Failed != 0 {
		t.Fatalf("summary: %+v", got.Summary)
	}

	// The current backup was not mutated (idempotent).
	afterCurrent, _ := os.ReadFile(current)
	if !bytes.Equal(beforeCurrent, afterCurrent) {
		t.Fatalf("current backup must be byte-identical after a legacy no-op run")
	}
}

// --- T014 / SC-003: safety on injected failures ----------------------------

func TestReencrypt_SafetyOnFailure(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(t *testing.T)
	}{
		{
			name: "fail_at_reencrypt",
			break_: func(t *testing.T) {
				prev := reencryptEncryptFile
				reencryptEncryptFile = func(_ *config.Configuration, _, _ string) (bool, *ports.EncryptionInfo, string, error) {
					return false, nil, "", errors.New("injected re-encrypt failure")
				}
				t.Cleanup(func() { reencryptEncryptFile = prev })
			},
		},
		{
			name: "fail_at_swap",
			break_: func(t *testing.T) {
				prev := reencryptRenameFile
				reencryptRenameFile = func(_, _ string) error {
					return errors.New("injected swap failure")
				}
				t.Cleanup(func() { reencryptRenameFile = prev })
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyEnv := reencKeyEnv
			testMasterKey(t, keyEnv)
			dir := t.TempDir()
			historyPath := filepath.Join(dir, "history.db")
			cfgPath := writeReencCfg(t, dir, historyPath, keyEnv)

			plaintext := []byte("-- irreplaceable legacy payload\n")
			artifact := filepath.Join(dir, "legacy.sql")
			writeLegacyBackup(t, artifact, keyEnv, plaintext, "job1-legacy")
			id := seedLocalExecution(t, historyPath, "job1", artifact, 0)

			before, _ := os.ReadFile(artifact)
			beforeManifest, _ := os.ReadFile(artifact + ".manifest.json")

			tc.break_(t)

			_, _, err := runReencrypt(t, cfgPath, reencFlags{args: []string{id}, mode: "legacy", output: "text"})
			if !errors.Is(err, ErrVerifyIntegrityFailed) {
				t.Fatalf("failed re-encrypt should return ErrVerifyIntegrityFailed, got: %v", err)
			}
			if Code(err) != 5 {
				t.Fatalf("failure exit code: want 5, got %d", Code(err))
			}

			// The original artifact + manifest are byte-for-byte intact.
			after, _ := os.ReadFile(artifact)
			afterManifest, _ := os.ReadFile(artifact + ".manifest.json")
			if !bytes.Equal(before, after) {
				t.Fatalf("original artifact must be untouched after failure")
			}
			if !bytes.Equal(beforeManifest, afterManifest) {
				t.Fatalf("original manifest must be untouched after failure")
			}

			// And still decryptable (as legacy, its original form).
			got, derr := decryptArtifact(t, artifact, keyEnv, true)
			if derr != nil {
				t.Fatalf("original must remain decryptable after failure: %v", derr)
			}
			if !bytes.Equal(got, plaintext) {
				t.Fatalf("original plaintext corrupted after failure")
			}

			// No stray temp files left behind in the backup dir.
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "sentinel-reencrypt-") {
					t.Fatalf("leftover temp file after failure: %s", e.Name())
				}
			}
		})
	}
}

// --- T017 / SC-002: key rotation, new key works / old fails, no caveat ------

func TestReencrypt_RotateKey(t *testing.T) {
	keyEnv := reencKeyEnv
	keyEnvB := reencKeyEnvB
	testMasterKey(t, keyEnv)
	testMasterKey(t, keyEnvB)
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeReencCfg(t, dir, historyPath, keyEnv)

	plaintext := []byte("-- v2 payload to re-key\n")
	artifact := filepath.Join(dir, "current.sql")
	writeV2Backup(t, artifact, keyEnv, plaintext, "job1-current")
	id := seedLocalExecution(t, historyPath, "job1", artifact, 0)

	beforeManifest, _ := manifest.ReadManifest(artifact + ".manifest.json")

	_, stderr, err := runReencrypt(t, cfgPath, reencFlags{
		args: []string{id}, mode: "rotate", newKeyEnv: keyEnvB, output: "text",
	})
	if err != nil {
		t.Fatalf("rotate should succeed, got: %v", err)
	}

	// No confidentiality caveat in rotate mode (US2 AS-3, SC-005 boundary).
	if strings.Contains(stderr, "confidentiality") || strings.Contains(stderr, "migration re-wraps") {
		t.Fatalf("rotate mode must NOT print the legacy caveat; stderr=\n%s", stderr)
	}

	// New key decrypts; old key fails.
	got, derr := decryptArtifact(t, artifact, keyEnvB, false)
	if derr != nil {
		t.Fatalf("re-keyed artifact must decrypt with the NEW key: %v", derr)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch after rotate")
	}
	if _, derr := decryptArtifact(t, artifact, keyEnv, false); derr == nil {
		t.Fatalf("re-keyed artifact must FAIL to decrypt with the OLD key")
	}

	// Fresh salt/IV (envelope re-generated).
	afterManifest, _ := manifest.ReadManifest(artifact + ".manifest.json")
	if afterManifest.Encryption.Salt == beforeManifest.Encryption.Salt {
		t.Fatalf("rotate should generate a fresh salt")
	}
	if afterManifest.Encryption.IV == beforeManifest.Encryption.IV {
		t.Fatalf("rotate should generate a fresh IV")
	}
	if afterManifest.Encryption.EnvelopeVersion != 2 {
		t.Fatalf("rotated manifest EnvelopeVersion should stay 2")
	}
}

// --- T020 / SC-004 / SC-009: dry-run no-op + --all gate + json shape --------

func TestReencrypt_DryRunAndGate(t *testing.T) {
	keyEnv := reencKeyEnv
	testMasterKey(t, keyEnv)
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeReencCfg(t, dir, historyPath, keyEnv)

	legacy := filepath.Join(dir, "legacy.sql")
	writeLegacyBackup(t, legacy, keyEnv, []byte("legacy payload"), "job1-legacy")
	seedLocalExecution(t, historyPath, "job1", legacy, 0)

	current := filepath.Join(dir, "current.sql")
	writeV2Backup(t, current, keyEnv, []byte("current payload"), "job1-current")
	seedLocalExecution(t, historyPath, "job1", current, 0)

	// Snapshot all files.
	snapshot := func() map[string][]byte {
		m := map[string][]byte{}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			m[e.Name()] = b
		}
		return m
	}
	before := snapshot()

	// --dry-run mutates nothing, and does not require --yes.
	out, _, err := runReencrypt(t, cfgPath, reencFlags{all: true, dryRun: true, mode: "legacy", output: "json"})
	if err != nil {
		t.Fatalf("dry-run should exit 0, got: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("json parse: %v\n%s", err, out)
	}
	for _, k := range []string{"results", "summary", "dry_run", "caveat"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("dry-run json missing %q:\n%s", k, out)
		}
	}
	after := snapshot()
	for name, b := range before {
		if !bytes.Equal(b, after[name]) {
			t.Fatalf("dry-run mutated %s", name)
		}
	}
	if len(before) != len(after) {
		t.Fatalf("dry-run changed the file set: before=%d after=%d", len(before), len(after))
	}

	// --all without --yes (and not dry-run) refuses and mutates nothing (SC-009).
	before2 := snapshot()
	_, _, err = runReencrypt(t, cfgPath, reencFlags{all: true, mode: "legacy", output: "text"})
	if !errors.Is(err, ErrVerifyInternal) || Code(err) != 4 {
		t.Fatalf("--all without --yes must be exit 4 (ErrVerifyInternal), got: %v (code %d)", err, Code(err))
	}
	after2 := snapshot()
	for name, b := range before2 {
		if !bytes.Equal(b, after2[name]) {
			t.Fatalf("unconfirmed --all mutated %s", name)
		}
	}
}

// --- validation matrix (exit 4) --------------------------------------------

func TestReencrypt_ValidationExit4(t *testing.T) {
	keyEnv := reencKeyEnv
	testMasterKey(t, keyEnv)
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeReencCfg(t, dir, historyPath, keyEnv)

	cases := []struct {
		name string
		f    reencFlags
	}{
		{"both id and --all", reencFlags{all: true, args: []string{"someid"}, mode: "legacy"}},
		{"neither id nor --all", reencFlags{mode: "legacy"}},
		{"rotate without new-key-env", reencFlags{args: []string{"someid"}, mode: "rotate"}},
		{"bad mode", reencFlags{args: []string{"someid"}, mode: "bogus"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runReencrypt(t, cfgPath, tc.f)
			if !errors.Is(err, ErrVerifyInternal) || Code(err) != 4 {
				t.Fatalf("%s: want exit 4 (ErrVerifyInternal), got: %v (code %d)", tc.name, err, Code(err))
			}
		})
	}
}

// --- T010 / SC-005: legacy caveat banner printed ---------------------------

func TestReencrypt_LegacyCaveatPrinted(t *testing.T) {
	keyEnv := reencKeyEnv
	testMasterKey(t, keyEnv)
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeReencCfg(t, dir, historyPath, keyEnv)

	legacy := filepath.Join(dir, "legacy.sql")
	writeLegacyBackup(t, legacy, keyEnv, []byte("legacy payload"), "job1-legacy")
	id := seedLocalExecution(t, historyPath, "job1", legacy, 0)

	_, stderr, err := runReencrypt(t, cfgPath, reencFlags{args: []string{id}, mode: "legacy", output: "text"})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !strings.Contains(stderr, "does NOT undo the confidentiality") {
		t.Fatalf("legacy mode must print the confidentiality caveat; stderr=\n%s", stderr)
	}
}
