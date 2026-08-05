package cli

// `sentinel security reencrypt` — guided envelope re-encryption & key rotation
// (spec 054 / PRD 43). It re-wraps an encrypted backup into the current v2
// envelope (legacy-migration mode) or re-encrypts it under a new master key
// (key-rotation mode) using a write-new → verify → swap pipeline so a
// recoverable artifact is never lost (FR-005/006/018, SC-003).
//
// Pure CLI composition: it reuses encryptBackupFile (backup_factory.go),
// runtime.PreRestoreVerifyAndDecryptWithOptions (restore pre-flight),
// manifest_store, monitor, and the storage registry through existing entry
// points. No new port; no change to the domain backup/restore Executors
// (FR-015).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/restore/runtime"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

// EventReencrypt is the structured slog event name emitted once per successful
// re-encryption (FR-016, SC-010). It mirrors the log-event audit convention of
// crypto.EventLegacyEnvelopeDecrypt.
const EventReencrypt = "security.reencrypt"

// reencryptListLimit bounds the number of executions the `--all` sweep
// enumerates, mirroring verifyAllListLimit.
const reencryptListLimit = 100000

// keepOriginalSuffix names the sibling artifact written under --keep-original.
const keepOriginalSuffix = ".reencrypted"

// Re-encryption outcomes (data-model.md). Any value with the "skipped_" prefix
// is tallied in the report's "skipped" bucket.
const (
	outMigrated            = "migrated"
	outRotated             = "rotated"
	outWouldMigrate        = "would_migrate"
	outWouldRotate         = "would_rotate"
	outSkippedCurrent      = "skipped_already_current"
	outSkippedUnencrypted  = "skipped_unencrypted"
	outSkippedUnmigratable = "skipped_unmigratable"
	outSkippedLegacyRotate = "skipped_legacy"
	outFailed              = "failed"
)

// ReencryptLegacyCaveat is the mandatory confidentiality caveat banner printed
// before the report in legacy-migration mode (FR-004, SC-005). It MUST NEVER be
// printed in pure key-rotation mode (the v2 envelope is sound).
const ReencryptLegacyCaveat = "" +
	"WARNING: legacy -> v2 migration re-wraps the artifact's FORMAT so it no longer needs\n" +
	"--allow-legacy-envelope. It does NOT undo the confidentiality weakness of the legacy\n" +
	"envelope -- a pre-v2 ciphertext may already be compromised. The real remediation is to\n" +
	"re-run the backup FROM SOURCE where possible. See docs/runbooks/recover-legacy-envelope.md."

// Test seams over the effectful primitives, mirroring newVerifyBackend in
// backup_verify.go. Tests override these to inject re-encrypt / swap failures
// (T014) and to fake a backend.
var (
	reencryptEncryptFile = encryptBackupFile
	reencryptRenameFile  = os.Rename
	newReencryptBackend  = func(p *storage.BackendParams) (ports.StorageBackend, error) {
		return storage.NewBackend(p)
	}
)

// reencryptResult is the per-backup outcome surfaced in the text/json report.
type reencryptResult struct {
	BackupID     string `json:"id"`
	Job          string `json:"job"`
	Mode         string `json:"mode"`
	FromEnvelope int    `json:"from_envelope"`
	ToEnvelope   int    `json:"to_envelope"`
	Backend      string `json:"backend"`
	Outcome      string `json:"outcome"`
	Err          error  `json:"-"`
}

// MarshalJSON surfaces an operational Err as a string "error" field.
func (r reencryptResult) MarshalJSON() ([]byte, error) {
	type alias reencryptResult
	aux := struct {
		alias
		Error string `json:"error,omitempty"`
	}{alias: alias(r)}
	if r.Err != nil {
		aux.Error = r.Err.Error()
	}
	return json.Marshal(aux)
}

// reencryptSummary holds the report tally.
type reencryptSummary struct {
	Processed int `json:"processed"`
	Migrated  int `json:"migrated"`
	Rotated   int `json:"rotated"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

// pipelineOpts carries the resolved per-run knobs into reencryptOne.
type pipelineOpts struct {
	legacyMode   bool
	keepOriginal bool
	newKeyHint   string
}

var securityReencryptCmd = &cobra.Command{
	Use:   "reencrypt [backup-id]",
	Short: "Re-encrypt a backup into the current envelope, or rotate its master key",
	Long: "Re-wrap an encrypted backup into the current v2 envelope (legacy-migration mode) so it no\n" +
		"longer needs --allow-legacy-envelope, or re-encrypt it under a new master key (rotation mode).\n" +
		"Uses write-new -> verify -> swap so a recoverable artifact is never lost. Pass a single\n" +
		"<backup-id>, or --all to process every recorded successful backup in scope.",
	Args: cobra.MaximumNArgs(1),
	RunE: runSecurityReencrypt,
}

func runSecurityReencrypt(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	job, _ := cmd.Flags().GetString("job")
	sinceStr, _ := cmd.Flags().GetString("since")
	mode, _ := cmd.Flags().GetString("mode")
	newKeyEnv, _ := cmd.Flags().GetString("new-key-env")
	keepOriginal, _ := cmd.Flags().GetBool("keep-original")
	yes, _ := cmd.Flags().GetBool("yes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	outputFmt, _ := cmd.Flags().GetString("output")

	hasID := len(args) == 1
	var backupID string
	if hasID {
		backupID = args[0]
	}

	// --- Validation (exit 4; nothing is mutated) -------------------------
	// Exactly one of {<backup-id>, --all}.
	if hasID == all {
		reencryptPrintError(outputFmt, "provide exactly one of <backup-id> or --all")
		return fmt.Errorf("reencrypt: %w", ErrVerifyInternal)
	}
	if mode != "legacy" && mode != "rotate" {
		reencryptPrintError(outputFmt, fmt.Sprintf("invalid --mode %q (want 'legacy' or 'rotate')", mode))
		return fmt.Errorf("reencrypt: %w", ErrVerifyInternal)
	}
	if mode == "rotate" && newKeyEnv == "" {
		reencryptPrintError(outputFmt, "rotate mode requires --new-key-env (the env var holding the new master key)")
		return fmt.Errorf("reencrypt: %w", ErrVerifyInternal)
	}
	// A bulk (--all) mutation requires --yes unless it is a --dry-run (FR-017).
	if all && !yes && !dryRun {
		reencryptPrintError(outputFmt,
			"refusing to re-encrypt every backup without confirmation; re-run with --yes to proceed, or --dry-run to preview")
		return fmt.Errorf("reencrypt: %w", ErrVerifyInternal)
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	if cfgPath == "" {
		cfgPath = os.ExpandEnv("$HOME/.sentinel/config.yaml")
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		reencryptPrintError(outputFmt, fmt.Sprintf("failed to load config: %v", err))
		return fmt.Errorf("reencrypt: %w", ErrVerifyInternal)
	}
	if outputFmt == "" {
		outputFmt = cfg.LogFormat
	}
	if outputFmt == "" {
		outputFmt = "text"
	}

	// The old key must be resolvable to decrypt the source artifacts.
	if !encryptionConfigured(cfg) {
		reencryptPrintError(outputFmt,
			"no encryption key configured (set encryption_key_env or encryption_key_file); nothing to re-encrypt")
		return fmt.Errorf("reencrypt: %w", ErrVerifyInternal)
	}

	legacyMode := mode == "legacy"

	// Target config: same key for legacy migration; a shallow clone with
	// EncryptionKeyEnv overridden when a new key is supplied (rotation, or a
	// legacy migrate-and-rekey pass). Env-only — never a raw key value (FR-013).
	targetCfg := cfg
	if newKeyEnv != "" {
		clone := *cfg
		clone.EncryptionKeyEnv = newKeyEnv
		clone.EncryptionKeyFile = ""
		targetCfg = &clone
	}
	opts := pipelineOpts{
		legacyMode:   legacyMode,
		keepOriginal: keepOriginal,
		newKeyHint:   targetCfg.EncryptionKeyEnv,
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		reencryptPrintError(outputFmt, fmt.Sprintf("failed to open history db: %v", err))
		return fmt.Errorf("reencrypt: %w", ErrVerifyInternal)
	}
	defer mon.Close()

	ctx := context.Background()

	execs, err := resolveReencryptExecutions(ctx, mon, hasID, backupID, all, job, sinceStr)
	if err != nil {
		reencryptPrintError(outputFmt, err.Error())
		return fmt.Errorf("reencrypt: %w", ErrVerifyInternal)
	}

	// Confidentiality caveat: printed before the report in legacy mode only.
	if legacyMode && outputFmt != "json" {
		fmt.Fprintln(os.Stderr, ReencryptLegacyCaveat)
	}

	results := make([]reencryptResult, 0, len(execs))
	for i := range execs {
		results = append(results, processOneReencrypt(ctx, cfg, targetCfg, mon, &execs[i], mode, dryRun, opts))
	}

	if outputFmt == "json" {
		reencryptPrintJSON(results, legacyMode, dryRun)
	} else {
		reencryptPrintText(results, dryRun)
	}

	// Exit 5 if any backup failed; otherwise 0 (FR-012, SC-006).
	for _, r := range results {
		if r.Outcome == outFailed {
			return fmt.Errorf("reencrypt: %w", ErrVerifyIntegrityFailed)
		}
	}
	return nil
}

// resolveReencryptExecutions mirrors verify --all enumeration: a single id via
// GetExecution, or the whole repository via ListExecutions scoped by job and
// recency.
func resolveReencryptExecutions(ctx context.Context, mon *monitor.Monitor, hasID bool, backupID string, all bool, job, sinceStr string) ([]ports.Execution, error) {
	if hasID {
		exec, err := mon.GetExecution(ctx, backupID)
		if err != nil || exec == nil {
			return nil, fmt.Errorf("backup ID %q not found in history (run 'sentinel monitor list' to see available IDs)", backupID)
		}
		return []ports.Execution{*exec}, nil
	}

	filter := &ports.Filter{Status: ports.StatusSuccess}
	if job != "" {
		filter.BackupName = job
	}
	execs, err := mon.ListExecutions(ctx, filter, reencryptListLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions: %v", err)
	}

	var cutoff time.Time
	if sinceStr != "" {
		window, perr := parseSince(sinceStr)
		if perr != nil {
			return nil, perr
		}
		cutoff = time.Now().UTC().Add(-window)
	}
	if cutoff.IsZero() {
		return execs, nil
	}
	filtered := execs[:0]
	for i := range execs {
		if execs[i].Timestamp.After(cutoff) {
			filtered = append(filtered, execs[i])
		}
	}
	return filtered, nil
}

// processOneReencrypt resolves + classifies one backup, decides the action for
// the active mode, and (unless dry-run) runs the pipeline. It never returns an
// error; every outcome is captured in the result so an --all batch continues.
func processOneReencrypt(ctx context.Context, cfg, targetCfg *config.Configuration, rec ports.Recorder, exec *ports.Execution, mode string, dryRun bool, opts pipelineOpts) reencryptResult {
	res := reencryptResult{
		BackupID: exec.ID,
		Job:      exec.BackupName,
		Mode:     mode,
		Backend:  exec.StorageBackend,
	}

	ref, err := resolveArtifact(cfg, exec)
	if err != nil {
		res.Outcome = outFailed
		res.Err = err
		return res
	}

	m, mErr := loadReencryptManifest(ctx, ref)
	if mErr != nil && !errors.Is(mErr, ports.ErrNoManifest) {
		res.Outcome = outFailed
		res.Err = fmt.Errorf("read manifest: %w", mErr)
		return res
	}

	class := classifyManifest(m, mErr)
	res.FromEnvelope = envelopeVersionOf(m, class)
	res.ToEnvelope = res.FromEnvelope

	// Decide the action for the active mode.
	eligible := false
	switch class {
	case classUnmigratable:
		res.Outcome = outSkippedUnmigratable
	case classUnencrypted:
		res.Outcome = outSkippedUnencrypted
	case classCurrent:
		if opts.legacyMode {
			res.Outcome = outSkippedCurrent // idempotent (FR-007)
		} else {
			eligible = true // rotate a current artifact
		}
	case classLegacy:
		if opts.legacyMode {
			eligible = true // migrate legacy -> v2
		} else {
			res.Outcome = outSkippedLegacyRotate // rotate targets current-format
		}
	}

	if !eligible {
		return res
	}

	res.ToEnvelope = 2

	if dryRun {
		// Preview only — stop before any staging/mutation (I2, SC-004).
		if opts.legacyMode {
			res.Outcome = outWouldMigrate
		} else {
			res.Outcome = outWouldRotate
		}
		return res
	}

	if err := reencryptOne(ctx, cfg, targetCfg, rec, ref, m, opts); err != nil {
		res.Outcome = outFailed
		res.ToEnvelope = res.FromEnvelope
		res.Err = err
		return res
	}

	if opts.legacyMode {
		res.Outcome = outMigrated
	} else {
		res.Outcome = outRotated
	}

	// Audit: exactly one structured event per successful re-encryption
	// (FR-016, SC-010).
	slog.InfoContext(ctx, EventReencrypt,
		"event", EventReencrypt,
		"backup_id", exec.ID,
		"job", exec.BackupName,
		"mode", mode,
		"from_envelope", res.FromEnvelope,
		"to_envelope", res.ToEnvelope,
		"backend", exec.StorageBackend,
		"keep_original", opts.keepOriginal,
		"outcome", res.Outcome,
	)
	return res
}

// reencryptOne runs the safety-critical write-new -> verify -> swap pipeline for
// a single classified-eligible backup (contracts/reencrypt-pipeline.md steps
// 1-9). It returns nil on success; on any error the original artifact is left
// intact and restorable (SC-003).
func reencryptOne(ctx context.Context, cfg, targetCfg *config.Configuration, rec ports.Recorder, ref *artifactRef, m *ports.BackupManifest, opts pipelineOpts) error {
	// Scratch dir for the staged source (remote) and re-verify download. The
	// new-artifact temp lives in the destination directory for local backups so
	// the final swap is an atomic same-filesystem os.Rename.
	scratch, err := os.MkdirTemp("", "sentinel-reencrypt-*")
	if err != nil {
		return fmt.Errorf("create scratch dir: %w", err)
	}
	defer os.RemoveAll(scratch)

	// 1. Stage the source to a local, readable path. Local backups are read in
	//    place (no copy) so the original is untouched.
	sourcePath := ref.localArtifactPath
	if ref.remote {
		sourcePath = filepath.Join(scratch, "source")
		if err := ref.backend.Download(ctx, ref.object, sourcePath); err != nil {
			return fmt.Errorf("stage source: %w", err)
		}
	}

	// 2. Decrypt the source (old key; AllowLegacy in legacy mode) and stream the
	//    plaintext into a temp in the destination directory, hashing as we go.
	oldKP := &crypto.FileKeyProvider{EnvVar: cfg.EncryptionKeyEnv, FilePath: cfg.EncryptionKeyFile}
	plainReader, err := runtime.PreRestoreVerifyAndDecryptWithOptions(ctx, m, sourcePath, oldKP,
		ports.DecryptOptions{AllowLegacy: opts.legacyMode, BackupID: m.BackupID, Source: sourcePath})
	if err != nil {
		return fmt.Errorf("decrypt source: %w", err)
	}

	workDir := scratch
	if !ref.remote {
		workDir = filepath.Dir(ref.localArtifactPath)
	}
	tmpFile, err := os.CreateTemp(workDir, "sentinel-reencrypt-*.tmp")
	if err != nil {
		return fmt.Errorf("create work temp: %w", err)
	}
	artifactTemp := tmpFile.Name()
	manifestTemp := artifactTemp + ".manifest.json"
	defer func() {
		_ = os.Remove(artifactTemp)
		_ = os.Remove(manifestTemp)
	}()

	plainHasher := crypto.NewHashingWriter(tmpFile)
	if _, err := io.Copy(plainHasher, plainReader); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("stage plaintext: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close plaintext temp: %w", err)
	}
	plaintextHash := plainHasher.Sum()

	// 3. Re-encrypt in place (fresh v2 envelope, target key). artifactTemp now
	//    holds ciphertext; encHash is the SHA-256 of the encrypted bytes.
	_, encInfo, encHash, err := reencryptEncryptFile(targetCfg, artifactTemp, m.BackupID)
	if err != nil {
		return fmt.Errorf("re-encrypt: %w", err)
	}

	// 4. Build the new manifest (fresh EncryptionInfo, recomputed Hash).
	newM := *m
	newM.Encryption = encInfo
	newM.Hash = ports.HashInfo{Algorithm: "sha256", Value: encHash, PlaintextValue: plaintextHash}
	if fi, statErr := os.Stat(artifactTemp); statErr == nil {
		newM.SizeBytes = fi.Size()
	}

	// 5. LOCAL VERIFY before any storage mutation: hash-check + decrypt-back the
	//    new temp with the target key and confirm the plaintext round-trips.
	targetKP := &crypto.FileKeyProvider{EnvVar: targetCfg.EncryptionKeyEnv, FilePath: targetCfg.EncryptionKeyFile}
	verifyReader, err := runtime.PreRestoreVerifyAndDecryptWithOptions(ctx, &newM, artifactTemp, targetKP,
		ports.DecryptOptions{BackupID: m.BackupID, Source: artifactTemp})
	if err != nil {
		return fmt.Errorf("verify new artifact: %w", err)
	}
	roundTripHasher := crypto.NewHashingWriter(io.Discard)
	if _, err := io.Copy(roundTripHasher, verifyReader); err != nil {
		return fmt.Errorf("verify new artifact (decrypt-back): %w", err)
	}
	if roundTripHasher.Sum() != plaintextHash {
		return fmt.Errorf("verify new artifact: plaintext hash mismatch after re-encrypt")
	}

	// 6. Persist the new manifest to a temp beside the artifact.
	if err := manifest.WriteManifest(manifestTemp, &newM); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// 7. Swap at the original path (or a sibling under --keep-original). Nothing
	//    in storage has been mutated before this point (SC-003).
	newManifestRef, err := swapArtifact(ctx, ref, artifactTemp, manifestTemp, encHash, opts.keepOriginal, scratch)
	if err != nil {
		return err
	}

	// 8. Update the monitor row so future verify/restore see the new
	//    hash/manifest (FR-010). Under --keep-original the new artifact went to a
	//    sibling path and the original row still describes the untouched
	//    original, so the row is left alone (manual cutover).
	if !opts.keepOriginal {
		if err := rec.RecordSecurityInfo(ctx, ref.exec.ID, "sha256", encHash, plaintextHash, newManifestRef, true, opts.newKeyHint); err != nil {
			return fmt.Errorf("record security info: %w", err)
		}
	}

	return nil
}

// swapArtifact performs the path-preserving swap (research D3). It returns the
// new manifest reference (local path or remote object key) for the monitor row.
func swapArtifact(ctx context.Context, ref *artifactRef, artifactTemp, manifestTemp, encHash string, keepOriginal bool, scratch string) (string, error) {
	if ref.remote {
		destObject := ref.object
		if keepOriginal {
			destObject = ref.object + keepOriginalSuffix
		}
		destManifestObject := destObject + ".manifest.json"

		// Overwrite happens only after the local verify above. The verified
		// local temp is retained (in scratch/workDir, removed by the caller's
		// defer) until the stored object re-verifies, so an interrupted upload
		// is recoverable by re-running.
		if err := ref.backend.Upload(ctx, artifactTemp, destObject); err != nil {
			return "", fmt.Errorf("swap upload artifact: %w", err)
		}
		reDL := filepath.Join(scratch, "reverify")
		if err := ref.backend.Download(ctx, destObject, reDL); err != nil {
			return "", fmt.Errorf("swap re-verify download: %w", err)
		}
		reHash, err := verifyComputeFileHash(reDL)
		if err != nil {
			return "", fmt.Errorf("swap re-verify hash: %w", err)
		}
		if reHash != encHash {
			return "", fmt.Errorf("swap re-verify: stored object hash mismatch (stored=%s want=%s)", reHash, encHash)
		}
		if err := ref.backend.Upload(ctx, manifestTemp, destManifestObject); err != nil {
			return "", fmt.Errorf("swap upload manifest: %w", err)
		}
		return destObject, nil
	}

	// Local: atomic os.Rename over the original (or to a sibling).
	destArtifact := ref.localArtifactPath
	if keepOriginal {
		destArtifact = ref.localArtifactPath + keepOriginalSuffix
	}
	destManifest := destArtifact + ".manifest.json"

	if err := reencryptRenameFile(artifactTemp, destArtifact); err != nil {
		return "", fmt.Errorf("swap artifact: %w", err)
	}
	if err := reencryptRenameFile(manifestTemp, destManifest); err != nil {
		return "", fmt.Errorf("swap manifest: %w", err)
	}
	return destManifest, nil
}

// --- Resolution / classification ------------------------------------------

// artifactRef locates a backup's artifact + manifest on its own storage
// backend. Local backups reference on-disk paths directly; remote backups carry
// a constructed backend + object keys.
type artifactRef struct {
	exec              *ports.Execution
	remote            bool
	localArtifactPath string
	localManifestPath string
	backend           ports.StorageBackend
	object            string
	manifestObject    string
}

// resolveArtifact builds an artifactRef for exec, constructing the remote
// backend + object keys via the verify helpers when needed.
func resolveArtifact(cfg *config.Configuration, exec *ports.Execution) (*artifactRef, error) {
	if exec.FilePath == "" {
		return nil, fmt.Errorf("no artifact path recorded for backup %q", exec.ID)
	}
	ref := &artifactRef{exec: exec}

	if !isRemoteStorageBackend(exec.StorageBackend) {
		ref.localArtifactPath = exec.FilePath
		ref.localManifestPath = exec.FilePath + ".manifest.json"
		return ref, nil
	}

	var storageCfg config.StorageConfig
	if job, ok := cfg.Databases[exec.BackupName]; ok {
		storageCfg = job.Storage
	}
	params, object, err := verifyBackendParamsAndObject(exec.StorageBackend, exec.FilePath, storageCfg)
	if err != nil {
		return nil, err
	}
	backend, err := newReencryptBackend(params)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize %s backend: %w", exec.StorageBackend, err)
	}
	ref.remote = true
	ref.backend = backend
	ref.object = object
	ref.manifestObject = object + ".manifest.json"
	return ref, nil
}

// loadReencryptManifest reads the backup's manifest sidecar without reading any
// artifact bytes (so it is safe for dry-run classification). Returns
// ports.ErrNoManifest when the sidecar is absent (→ unmigratable).
func loadReencryptManifest(ctx context.Context, ref *artifactRef) (*ports.BackupManifest, error) {
	if !ref.remote {
		return manifest.ReadManifest(ref.localManifestPath)
	}

	exists, err := ref.backend.Exists(ctx, ref.manifestObject)
	if err != nil {
		return nil, fmt.Errorf("check manifest sidecar: %w", err)
	}
	if !exists {
		return nil, ports.ErrNoManifest
	}
	tmpDir, err := os.MkdirTemp("", "sentinel-reencrypt-manifest-*")
	if err != nil {
		return nil, fmt.Errorf("create manifest temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	local := filepath.Join(tmpDir, "manifest.json")
	if err := ref.backend.Download(ctx, ref.manifestObject, local); err != nil {
		return nil, fmt.Errorf("download manifest sidecar: %w", err)
	}
	return manifest.ReadManifest(local)
}

// reencryptClass is the classification of an in-scope backup (data-model.md).
type reencryptClass int

const (
	classCurrent reencryptClass = iota
	classLegacy
	classUnencrypted
	classUnmigratable
)

// classifyManifest maps a (manifest, read-error) pair to a class.
func classifyManifest(m *ports.BackupManifest, mErr error) reencryptClass {
	if errors.Is(mErr, ports.ErrNoManifest) {
		return classUnmigratable
	}
	if m == nil || m.Encryption == nil {
		return classUnencrypted
	}
	if m.Encryption.EnvelopeVersion >= 2 {
		return classCurrent
	}
	return classLegacy
}

// envelopeVersionOf reports the source envelope version for the report. A
// legacy artifact (no/old version marker) is surfaced as v1; unencrypted /
// unmigratable have no envelope (0).
func envelopeVersionOf(m *ports.BackupManifest, class reencryptClass) int {
	switch class {
	case classCurrent:
		return 2
	case classLegacy:
		if m != nil && m.Encryption != nil && m.Encryption.EnvelopeVersion > 0 {
			return m.Encryption.EnvelopeVersion
		}
		return 1
	default:
		return 0
	}
}

// --- Report ----------------------------------------------------------------

func summarizeReencrypt(results []reencryptResult) reencryptSummary {
	s := reencryptSummary{Processed: len(results)}
	for _, r := range results {
		switch r.Outcome {
		case outMigrated, outWouldMigrate:
			s.Migrated++
		case outRotated, outWouldRotate:
			s.Rotated++
		case outFailed:
			s.Failed++
		default: // skipped_*
			s.Skipped++
		}
	}
	return s
}

func envelopeTransition(r reencryptResult) string {
	if r.FromEnvelope == 0 {
		return "n/a"
	}
	return fmt.Sprintf("v%d->v%d", r.FromEnvelope, r.ToEnvelope)
}

func reencryptPrintText(results []reencryptResult, dryRun bool) {
	if dryRun {
		fmt.Println("DRY RUN — no artifacts or metadata were modified")
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tJOB\tMODE\tFROM->TO\tOUTCOME")
	for _, r := range results {
		outcome := r.Outcome
		if r.Err != nil {
			outcome = fmt.Sprintf("%s (%v)", r.Outcome, r.Err)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.BackupID, r.Job, r.Mode, envelopeTransition(r), outcome)
	}
	_ = w.Flush()

	s := summarizeReencrypt(results)
	fmt.Printf("%d processed · %d migrated · %d rotated · %d skipped · %d failed\n",
		s.Processed, s.Migrated, s.Rotated, s.Skipped, s.Failed)
}

func reencryptPrintJSON(results []reencryptResult, legacyMode, dryRun bool) {
	payload := map[string]interface{}{
		"results": results,
		"summary": summarizeReencrypt(results),
		"dry_run": dryRun,
	}
	if legacyMode {
		payload["caveat"] = ReencryptLegacyCaveat
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func reencryptPrintError(format, msg string) {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]interface{}{"result": "error", "error": msg})
		return
	}
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
}

func init() {
	SecurityCmd.AddCommand(securityReencryptCmd)
	securityReencryptCmd.Flags().String("config", "", "Path to sentinel YAML config")
	securityReencryptCmd.Flags().Bool("all", false, "Re-encrypt every recorded successful backup in scope; mutually exclusive with <backup-id>")
	securityReencryptCmd.Flags().String("job", "", "With --all: restrict to a single named backup job")
	securityReencryptCmd.Flags().String("since", "", "With --all: only process backups newer than this age (e.g. 30d, 4w, 720h)")
	securityReencryptCmd.Flags().String("mode", "legacy", "Re-encryption mode: 'legacy' (v1->v2 migration) or 'rotate' (re-key under --new-key-env)")
	securityReencryptCmd.Flags().String("new-key-env", "", "Env var holding the new master key (base64, 32 bytes); required in rotate mode (env-only)")
	securityReencryptCmd.Flags().Bool("keep-original", false, "Write the re-encrypted artifact to a sibling path and leave the original in place (manual cutover)")
	securityReencryptCmd.Flags().Bool("yes", false, "Confirm a bulk (--all) mutation; not needed for a single <backup-id> or --dry-run")
	securityReencryptCmd.Flags().Bool("dry-run", false, "Classify and report only; mutate nothing")
	securityReencryptCmd.Flags().String("output", "", "Output format: text or json")
}
