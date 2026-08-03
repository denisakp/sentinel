package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/adapters/storage/gcs"
	"github.com/denisakp/sentinel/internal/adapters/storage/s3"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

// newVerifyBackend is a test seam over the storage registry, mirroring
// newRetentionDeleteBackend in retention_cleaner.go. It lets tests inject a
// fake backend for the remote-verify fetch path.
var newVerifyBackend = func(p *storage.BackendParams) (ports.StorageBackend, error) {
	return storage.NewBackend(p)
}

// verify status constants — the four states a backup can be classified into by
// verifyExecution.
const (
	verifyStatusOk              = "ok"
	verifyStatusCorrupted       = "corrupted"
	verifyStatusMissingArtifact = "missing_artifact"
	verifyStatusMissingManifest = "missing_manifest"
)

// verifyAllListLimit bounds the number of executions the `--all` sweep
// enumerates from the monitor, mirroring the retention fetch cap.
const verifyAllListLimit = 100000

// verifyOpts carries per-verify options shared by the single-id and the
// `--all` sweep callers of verifyExecution.
type verifyOpts struct {
	// ignoreMissingManifest downgrades a missing_manifest outcome from an
	// integrity failure to a warning at the sweep's exit-code stage. It does
	// not change per-execution classification, so verifyExecution ignores it;
	// handleVerifyAll reads it when mapping results to an exit code.
	ignoreMissingManifest bool
}

// verifyResult is the per-backup outcome of a single integrity verification.
// It feeds both the single-id renderer and the `--all` sweep report/JSON.
// Err is set only for genuine operational failures (backend init, temp dir,
// non-not-found download/read errors); for those Status is left empty. For the
// four classified states Err is nil.
type verifyResult struct {
	BackupID      string    `json:"backup_id"`
	Job           string    `json:"job"`
	Status        string    `json:"status"`
	HashMatch     bool      `json:"hash_match"`
	StoredHash    string    `json:"stored_hash,omitempty"`
	ComputedHash  string    `json:"computed_hash,omitempty"`
	HashAlgorithm string    `json:"hash_algorithm,omitempty"`
	SizeBytes     int64     `json:"size_bytes"`
	Timestamp     time.Time `json:"timestamp"`
	Path          string    `json:"path,omitempty"`
	// StorageBackend is the artifact's recorded storage backend. Carried for
	// the scheduled integrity audit trail; excluded from the
	// `verify --all` JSON so its output stays byte-for-byte identical.
	StorageBackend string `json:"-"`
	Err            error  `json:"-"`
}

// MarshalJSON serialises a verifyResult, surfacing any operational Err as a
// string "error" field (the error interface itself is not JSON-serialisable).
func (r verifyResult) MarshalJSON() ([]byte, error) {
	type alias verifyResult
	aux := struct {
		alias
		Error string `json:"error,omitempty"`
	}{alias: alias(r)}
	if r.Err != nil {
		aux.Error = r.Err.Error()
	}
	return json.Marshal(aux)
}

var backupVerifyCmd = &cobra.Command{
	Use:   "verify [backup-id]",
	Short: "Verify the integrity of a stored backup (or the whole repository with --all)",
	Long: "Re-compute the SHA-256 fingerprint of a backup artifact and compare it against the stored\n" +
		"manifest value. Pass a single <backup-id> to verify one backup, or --all to sweep every\n" +
		"recorded backup and produce an aggregate report + a single exit code.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		hasID := len(args) == 1

		// Exactly one of {<backup-id>, --all}. Both or neither is a
		// usage error.
		if hasID == all {
			return fmt.Errorf("provide exactly one of <backup-id> or --all")
		}

		cfgPath, _ := cmd.Flags().GetString("config")
		if cfgPath == "" {
			cfgPath = os.ExpandEnv("$HOME/.sentinel/config.yaml")
		}
		outputFmt := verifyOutputFormat(cmd)

		var backupID string
		if hasID {
			backupID = args[0]
		}

		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			verifyPrintError(outputFmt, backupID, "", fmt.Sprintf("failed to load config: %v", err))
			return fmt.Errorf("load config: %w", ErrVerifyInternal)
		}

		if outputFmt == "" {
			outputFmt = cfg.LogFormat
		}

		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			verifyPrintError(outputFmt, backupID, "", fmt.Sprintf("failed to open history db: %v", err))
			return fmt.Errorf("open history db: %w", ErrVerifyInternal)
		}
		defer mon.Close()

		ctx := context.Background()

		if all {
			return handleVerifyAll(ctx, cmd, cfg, mon, outputFmt)
		}

		exec, err := mon.GetExecution(ctx, backupID)
		if err != nil || exec == nil {
			verifyPrintError(outputFmt, backupID, "",
				fmt.Sprintf("backup ID %q not found in history\n  Fix: run 'sentinel monitor list' to see available backup IDs", backupID))
			return fmt.Errorf("backup %q: %w", backupID, ErrVerifyNotFound)
		}

		res := verifyExecution(ctx, cfg, exec, verifyOpts{})
		return renderSingleVerify(outputFmt, backupID, res)
	},
}

// verifyOutputFormat resolves the effective output format from the --output
// flag, falling back to the hidden --format alias (A6). An empty result means
// "inherit cfg.LogFormat", resolved by the caller after config load.
func verifyOutputFormat(cmd *cobra.Command) string {
	if out, _ := cmd.Flags().GetString("output"); out != "" {
		return out
	}
	fmtFlag, _ := cmd.Flags().GetString("format")
	return fmtFlag
}

// verifyExecution verifies a single recorded backup and classifies it into one
// of the four states (ok / corrupted / missing_artifact / missing_manifest),
// or reports an operational failure via verifyResult.Err. It is the shared body
// behind both single-id `verify <id>` and the `--all` sweep, so the two paths
// classify identically.
//
// Local backups verify against exec.FilePath directly. Remote backups
// (s3/gcs/azure/gdrive) hold a URI or object key there, unreachable via
// os.Open, so the artifact + its <key>.manifest.json sidecar are downloaded to
// a temp dir (deleted before this function returns) and verified
// against those local copies.
func verifyExecution(ctx context.Context, cfg *config.Configuration, exec *ports.Execution, _ verifyOpts) verifyResult {
	res := verifyResult{
		BackupID:       exec.ID,
		Job:            exec.BackupName,
		Timestamp:      exec.Timestamp,
		Path:           exec.FilePath,
		SizeBytes:      exec.FileSizeBytes,
		StorageBackend: exec.StorageBackend,
	}

	// No artifact reference recorded — nothing to hash. Mirrors the single-id
	// "skipped" outcome; classified as unverifiable (missing_manifest).
	if exec.FilePath == "" {
		res.Status = verifyStatusMissingManifest
		return res
	}

	manifestPath := exec.FilePath + ".manifest.json"
	hashTarget := exec.FilePath

	if isRemoteStorageBackend(exec.StorageBackend) {
		tmpDir, tmpErr := os.MkdirTemp("", "sentinel-verify-*")
		if tmpErr != nil {
			res.Err = fmt.Errorf("failed to create temp dir: %v", tmpErr)
			return res
		}
		defer os.RemoveAll(tmpDir)

		var storageCfg config.StorageConfig
		if job, ok := cfg.Databases[exec.BackupName]; ok {
			storageCfg = job.Storage
		}

		localArtifact, fetchErr := fetchRemoteBackupForVerify(ctx, exec, storageCfg, tmpDir)
		if fetchErr != nil {
			// A genuinely absent artifact is an integrity gap, not an
			// operational error; anything else (backend init, credentials,
			// transport) is operational.
			if verifyErrIsNotFound(fetchErr) {
				res.Status = verifyStatusMissingArtifact
				return res
			}
			res.Err = fmt.Errorf("failed to fetch remote backup: %v", fetchErr)
			return res
		}
		hashTarget = localArtifact
		manifestPath = localArtifact + ".manifest.json"
	}

	m, err := manifest.ReadManifest(manifestPath)
	if err != nil {
		if errors.Is(err, ports.ErrNoManifest) {
			res.Status = verifyStatusMissingManifest
			return res
		}
		res.Err = fmt.Errorf("failed to read manifest: %v", err)
		return res
	}
	res.StoredHash = m.Hash.Value
	res.HashAlgorithm = m.Hash.Algorithm

	computedHash, err := verifyComputeFileHash(hashTarget)
	if err != nil {
		// Manifest present but the (local) artifact is gone ⇒ missing_artifact;
		// any other read error is operational.
		if verifyErrIsNotFound(err) {
			res.Status = verifyStatusMissingArtifact
			return res
		}
		res.Err = fmt.Errorf("failed to compute hash: %v", err)
		return res
	}
	res.ComputedHash = computedHash

	if computedHash != m.Hash.Value {
		res.Status = verifyStatusCorrupted
		return res
	}

	res.Status = verifyStatusOk
	res.HashMatch = true
	return res
}

// verifyErrIsNotFound reports whether err indicates the artifact object was
// genuinely absent (as opposed to an operational backend/transport failure).
// Covers local/mock (os.ErrNotExist) and the S3/GCS not-found sentinels.
func verifyErrIsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, s3.ErrObjectNotFound) ||
		errors.Is(err, gcs.ErrObjectNotFound)
}

// renderSingleVerify reproduces the historical single-id `verify <id>` output
// (text + JSON) byte-for-byte from a verifyResult, and returns the matching
// sentinel error for the exit-code mapping.
func renderSingleVerify(outputFmt, backupID string, res verifyResult) error {
	if res.Err != nil {
		verifyPrintError(outputFmt, backupID, res.Job, res.Err.Error())
		return fmt.Errorf("verify %q: %w", backupID, ErrVerifyInternal)
	}

	switch res.Status {
	case verifyStatusMissingManifest:
		verifyPrintSkipped(outputFmt, backupID)
		return fmt.Errorf("backup %q: %w", backupID, ErrVerifySkipped)

	case verifyStatusMissingArtifact:
		verifyPrintError(outputFmt, backupID, res.Job, "backup artifact not found in storage")
		return fmt.Errorf("backup %q: %w", backupID, ErrVerifyInternal)

	case verifyStatusCorrupted:
		verifiedAt := time.Now().UTC()
		if outputFmt == "json" {
			verifyPrintJSON(map[string]interface{}{
				"backup_id":     backupID,
				"database":      res.Job,
				"stored_hash":   res.StoredHash,
				"computed_hash": res.ComputedHash,
				"result":        "fail",
				"error":         "hash mismatch: backup file has been modified or corrupted",
				"verified_at":   verifiedAt.Format(time.RFC3339),
			})
		} else {
			fmt.Printf("FAIL: Backup %s integrity check failed\n", backupID)
			fmt.Printf("  Database:      %s\n", res.Job)
			fmt.Printf("  File:          %s\n", res.Path)
			fmt.Printf("  Stored hash:   %s\n", res.StoredHash)
			fmt.Printf("  Computed hash: %s\n", res.ComputedHash)
			fmt.Println("  Status:        FAIL - hash mismatch")
		}
		return errors.New("hash mismatch")

	default: // verifyStatusOk
		verifiedAt := time.Now().UTC()
		if outputFmt == "json" {
			verifyPrintJSON(map[string]interface{}{
				"backup_id":     backupID,
				"database":      res.Job,
				"file_path":     res.Path,
				"stored_hash":   res.StoredHash,
				"computed_hash": res.ComputedHash,
				"result":        "pass",
				"verified_at":   verifiedAt.Format(time.RFC3339),
			})
		} else {
			fmt.Printf("PASS: Backup %s integrity verified\n", backupID)
			fmt.Printf("  Database: %s\n", res.Job)
			fmt.Printf("  File:     %s\n", res.Path)
			fmt.Printf("  Hash:     %s (%s)\n", res.StoredHash, res.HashAlgorithm)
			fmt.Println("  Status:   PASS")
		}
		return nil
	}
}

// sweepOptions carries the enumeration scope + per-verify options for
// runVerifySweep. It is the shared input for both the manual `verify --all`
// command (handleVerifyAll) and the scheduled integrity runner
// (runScheduledIntegrityCheck), so the two entry points
// drive one identical sweep implementation.
type sweepOptions struct {
	// job restricts the sweep to a single named backup job ("" = all jobs).
	job string
	// cutoff skips backups not strictly newer than this instant; the zero
	// value disables the recency filter (--since unset).
	cutoff time.Time
	// verify carries per-execution verify options (ignore-missing-manifest).
	verify verifyOpts
}

// runVerifySweep is the shared repository-wide integrity sweep core: enumerate
// every recorded successful backup from the monitor (optionally scoped to one
// job), verify each via verifyExecution (sequential; temp fetch deleted after
// each), skip anything older than the recency cutoff, and return the
// per-artifact results + their aggregate summary. It performs NO rendering and
// maps NO exit code — those stay with the caller (handleVerifyAll renders +
// exit-codes; runScheduledIntegrityCheck records + notifies). A ListExecutions
// failure is returned verbatim so the caller can format it.
func runVerifySweep(ctx context.Context, cfg *config.Configuration, mon *monitor.Monitor, opts sweepOptions) ([]verifyResult, verifySummary, error) {
	filter := &ports.Filter{Status: ports.StatusSuccess}
	if opts.job != "" {
		filter.BackupName = opts.job
	}

	execs, err := mon.ListExecutions(ctx, filter, verifyAllListLimit, 0)
	if err != nil {
		return nil, verifySummary{}, err
	}

	results := make([]verifyResult, 0, len(execs))
	for i := range execs {
		if !opts.cutoff.IsZero() && !execs[i].Timestamp.After(opts.cutoff) {
			continue // --since: skip backups older than the window
		}
		results = append(results, verifyExecution(ctx, cfg, &execs[i], opts.verify))
	}

	return results, summarizeVerify(results), nil
}

// handleVerifyAll runs the repository-wide integrity sweep and renders it: it
// reads the --job/--since/--ignore-missing-manifest flags, delegates the
// enumerate → verify → aggregate core to runVerifySweep, prints an aggregate
// report + summary, and maps the results to a single exit code. The rendering
// and exit-code behaviour is unchanged.
func handleVerifyAll(ctx context.Context, cmd *cobra.Command, cfg *config.Configuration, mon *monitor.Monitor, outputFmt string) error {
	job, _ := cmd.Flags().GetString("job")
	sinceStr, _ := cmd.Flags().GetString("since")
	ignoreMissing, _ := cmd.Flags().GetBool("ignore-missing-manifest")

	var cutoff time.Time
	if sinceStr != "" {
		window, perr := parseSince(sinceStr)
		if perr != nil {
			verifyPrintError(outputFmt, "", job, perr.Error())
			return fmt.Errorf("parse since: %w", ErrVerifyInternal)
		}
		cutoff = time.Now().UTC().Add(-window)
	}

	results, _, err := runVerifySweep(ctx, cfg, mon, sweepOptions{
		job:    job,
		cutoff: cutoff,
		verify: verifyOpts{ignoreMissingManifest: ignoreMissing},
	})
	if err != nil {
		verifyPrintError(outputFmt, "", job, fmt.Sprintf("failed to list executions: %v", err))
		return fmt.Errorf("list executions: %w", ErrVerifyInternal)
	}

	if outputFmt == "json" {
		printVerifyAllJSON(results)
	} else {
		printVerifyAllText(results)
	}

	return verifyAllExitError(results, ignoreMissing)
}

// verifySummary holds per-status counts for the sweep report.
type verifySummary struct {
	Checked         int
	OK              int
	Corrupted       int
	MissingArtifact int
	MissingManifest int
	Errored         int
}

func summarizeVerify(results []verifyResult) verifySummary {
	s := verifySummary{Checked: len(results)}
	for _, r := range results {
		if r.Err != nil {
			s.Errored++
			continue
		}
		switch r.Status {
		case verifyStatusOk:
			s.OK++
		case verifyStatusCorrupted:
			s.Corrupted++
		case verifyStatusMissingArtifact:
			s.MissingArtifact++
		case verifyStatusMissingManifest:
			s.MissingManifest++
		}
	}
	return s
}

func printVerifyAllText(results []verifyResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tJOB\tSTATUS\tHASH_MATCH\tTIMESTAMP")
	for _, r := range results {
		status := r.Status
		hashMatch := "-"
		if r.Err != nil {
			status = "error"
		} else {
			switch r.Status {
			case verifyStatusOk:
				hashMatch = "true"
			case verifyStatusCorrupted:
				hashMatch = "false"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			r.BackupID, r.Job, status, hashMatch, r.Timestamp.UTC().Format(time.RFC3339))
	}
	_ = w.Flush()

	s := summarizeVerify(results)
	fmt.Printf("%d checked · %d ok · %d corrupted · %d missing_artifact · %d missing_manifest\n",
		s.Checked, s.OK, s.Corrupted, s.MissingArtifact, s.MissingManifest)
	if s.Errored > 0 {
		fmt.Printf("  (%d operational error(s) — see rows marked 'error')\n", s.Errored)
	}
}

func printVerifyAllJSON(results []verifyResult) {
	s := summarizeVerify(results)
	verifyPrintJSON(map[string]interface{}{
		"results": results,
		"summary": map[string]int{
			"checked":          s.Checked,
			"ok":               s.OK,
			"corrupted":        s.Corrupted,
			"missing_artifact": s.MissingArtifact,
			"missing_manifest": s.MissingManifest,
			"errored":          s.Errored,
		},
	})
}

// verifyAllExitError maps sweep results to a sentinel error. An operational
// failure (backend/monitor/config) that prevented a clean check returns
// ErrVerifyInternal; a definitive integrity problem returns
// ErrVerifyIntegrityFailed; all-ok returns nil (A3). missing_manifest counts
// as an integrity failure unless --ignore-missing-manifest (A2).
func verifyAllExitError(results []verifyResult, ignoreMissing bool) error {
	var integrityFail, operationalFail bool
	for _, r := range results {
		if r.Err != nil {
			operationalFail = true
			continue
		}
		switch r.Status {
		case verifyStatusCorrupted, verifyStatusMissingArtifact:
			integrityFail = true
		case verifyStatusMissingManifest:
			if !ignoreMissing {
				integrityFail = true
			}
		}
	}
	if integrityFail {
		return fmt.Errorf("verify sweep: %w", ErrVerifyIntegrityFailed)
	}
	if operationalFail {
		return fmt.Errorf("verify sweep: %w", ErrVerifyInternal)
	}
	return nil
}

func verifyComputeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file %q: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash file %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isRemoteStorageBackend reports whether a recorded execution's storage
// backend is a non-local (remote) target whose artifact must be downloaded
// before it can be verified.
func isRemoteStorageBackend(storageType string) bool {
	return storageType != "" && storageType != "local"
}

// fetchRemoteBackupForVerify downloads the remote backup artifact referenced by
// exec into tmpDir and, when present, its <key>.manifest.json sidecar alongside
// it (so a subsequent manifest.ReadManifest + hash compare runs on local
// files). It returns the local artifact path. A missing manifest sidecar is not
// an error: the sidecar is simply not downloaded, and the caller's ReadManifest
// then surfaces the existing "no manifest / skipped" outcome. Storage /
// credential failures are returned as errors (never a silent skip).
func fetchRemoteBackupForVerify(ctx context.Context, exec *ports.Execution, storageCfg config.StorageConfig, tmpDir string) (string, error) {
	params, object, err := verifyBackendParamsAndObject(exec.StorageBackend, exec.FilePath, storageCfg)
	if err != nil {
		return "", err
	}

	backend, err := newVerifyBackend(params)
	if err != nil {
		return "", fmt.Errorf("failed to initialize %s backend: %w", exec.StorageBackend, err)
	}

	localArtifact := filepath.Join(tmpDir, filepath.Base(object))
	if err := backend.Download(ctx, object, localArtifact); err != nil {
		return "", fmt.Errorf("failed to download backup artifact %q: %w", object, err)
	}

	// Optional manifest sidecar: tolerate absence (pre-v1.1 backup or a remote
	// upload whose sidecar step failed) by leaving it undownloaded.
	manifestObject := object + ".manifest.json"
	exists, err := backend.Exists(ctx, manifestObject)
	if err != nil {
		return "", fmt.Errorf("failed to check manifest sidecar %q: %w", manifestObject, err)
	}
	if exists {
		if err := backend.Download(ctx, manifestObject, localArtifact+".manifest.json"); err != nil {
			return "", fmt.Errorf("failed to download manifest sidecar %q: %w", manifestObject, err)
		}
	}

	return localArtifact, nil
}

// verifyBackendParamsAndObject maps a recorded remote artifact reference
// (exec.FilePath) plus the job's resolved storage config into the storage
// registry params and the object key to download. It mirrors the per-type
// param construction in retention_cleaner.go and reuses parseBucketObjectRef to
// split bucket/object from either a scheme URI (gs://…) or a plain object key.
func verifyBackendParamsAndObject(storageType, filePath string, cfg config.StorageConfig) (*storage.BackendParams, string, error) {
	switch storageType {
	case "s3":
		_, object, err := parseBucketObjectRef(filePath, "s3", cfg.S3Bucket)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse s3 path %q: %w", filePath, err)
		}
		return &storage.BackendParams{
			StorageType:        "s3",
			AWSBucket:          cfg.S3Bucket,
			AWSRegion:          cfg.S3Region,
			AWSBucketEndpoint:  cfg.S3BucketEndpoint,
			AWSAccessKeyID:     cfg.S3AccessKeyID,
			AWSSecretAccessKey: cfg.S3SecretAccessKey,
		}, object, nil

	case "gcs":
		bucket, object, err := parseBucketObjectRef(filePath, "gs", cfg.GCSBucket)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse gcs uri %q: %w", filePath, err)
		}
		return &storage.BackendParams{
			StorageType:        "gcs",
			GCSBucket:          bucket,
			GCSProjectID:       cfg.GCSProjectID,
			GCSCredentialsFile: cfg.GCSCredentialsFile,
		}, object, nil

	case "azure":
		_, object, err := parseBucketObjectRef(filePath, "azure", cfg.AzureContainer)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse azure path %q: %w", filePath, err)
		}
		return &storage.BackendParams{
			StorageType:         "azure",
			AzureStorageAccount: cfg.AzureStorageAccount,
			AzureStorageKey:     cfg.AzureStorageKey,
			AzureContainer:      cfg.AzureContainer,
		}, object, nil

	case "google-drive":
		// Google Drive addresses files by name/path, not bucket/object, so the
		// recorded reference is the object key as-is.
		return &storage.BackendParams{
			StorageType:          "google-drive",
			GoogleDriveFolderId:  cfg.GDriveFolderID,
			GoogleServiceAccount: cfg.GDriveSAFile,
		}, filePath, nil

	default:
		return nil, "", fmt.Errorf("remote verify not supported for storage type %q", storageType)
	}
}

func verifyPrintError(format, backupID, database, msg string) {
	if format == "json" {
		verifyPrintJSON(map[string]interface{}{
			"backup_id": backupID,
			"database":  database,
			"result":    "error",
			"error":     msg,
		})
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
}

func verifyPrintSkipped(format, backupID string) {
	if format == "json" {
		verifyPrintJSON(map[string]string{
			"backup_id": backupID,
			"result":    "skipped",
			"warning":   "no manifest found for this backup; integrity cannot be verified (pre-v1.1 backup)",
		})
	} else {
		fmt.Printf("Warning: no manifest found for backup %s (pre-v1.1 backup)\n", backupID)
		fmt.Println("  Integrity cannot be verified.")
	}
}

func verifyPrintJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func init() {
	BackupCmd.AddCommand(backupVerifyCmd)
	backupVerifyCmd.Flags().String("config", "", "Path to sentinel YAML config")
	backupVerifyCmd.Flags().String("output", "", "Output format: json or text")
	backupVerifyCmd.Flags().Bool("allow-legacy-envelope", legacyEnvelopeEnvDefault(),
		"Decrypt artifacts produced before the v2 envelope fix. UNSAFE: pre-v2 streams used a flawed nonce scheme. Use only to recover plaintext for re-encryption.")

	// Repository-wide integrity sweep.
	backupVerifyCmd.Flags().Bool("all", false, "Verify every recorded backup (repository-wide integrity sweep); mutually exclusive with <backup-id>")
	backupVerifyCmd.Flags().String("since", "", "With --all: only verify backups newer than this age (e.g. 30d, 4w, 720h)")
	backupVerifyCmd.Flags().String("job", "", "With --all: restrict the sweep to a single named backup job")
	backupVerifyCmd.Flags().Bool("ignore-missing-manifest", false, "With --all: treat missing_manifest as a warning (exit 0) instead of an integrity failure")
	backupVerifyCmd.Flags().String("format", "", "Alias for --output")
	_ = backupVerifyCmd.Flags().MarkHidden("format")
}
