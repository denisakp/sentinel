package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

// diffLargeSwingPct is the absolute percentage change at or above which a
// size/duration delta is annotated with a ⚠ marker. It is informational only —
// magnitude swings never change the exit code (only security regressions do,
// per PRD 36 Q1).
const diffLargeSwingPct = 50.0

// diffSide holds the two recorded metadata sources for one backup: the monitor
// row (always present) and the manifest sidecar (nil when absent — a pre-v1.1
// backup or a remote manifest that could not be fetched). No artifact bytes are
// ever read to populate this — only the SQLite row and the .manifest.json
// sidecar (PRD 36 exit criterion).
type diffSide struct {
	id       string
	exec     *ports.Execution
	manifest *ports.BackupManifest
}

func (s diffSide) hasManifest() bool { return s.manifest != nil }

// diffFieldResult is a single differing field in the comparison.
type diffFieldResult struct {
	Field    string `json:"field"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Delta    string `json:"delta"`
	Security bool   `json:"security"`
	Warn     bool   `json:"warn"`
}

// diffResult is the full structured comparison, emitted verbatim by --output
// json and rendered as a table by the default text output.
type diffResult struct {
	ID1                 string            `json:"id1"`
	ID2                 string            `json:"id2"`
	Differences         []diffFieldResult `json:"differences"`
	SecurityRegressions []string          `json:"security_regressions"`
	Warnings            []string          `json:"warnings,omitempty"`
}

func (r diffResult) hasSecurityRegression() bool { return len(r.SecurityRegressions) > 0 }

var backupDiffCmd = &cobra.Command{
	Use:   "diff <id1> <id2>",
	Short: "Compare the recorded metadata of two backups (flags security regressions)",
	Long: "Compare two backups' recorded metadata — size, duration, hash algorithm, encryption\n" +
		"posture, backup type and chain depth — reading only the monitor row and the\n" +
		"<artifact>.manifest.json sidecar for each. No artifact bytes are read.\n\n" +
		"Silent security regressions between the two backups — encryption turned off, a hash\n" +
		"algorithm change, or an encryption-parameter downgrade — are flagged explicitly and\n" +
		"yield a non-zero exit code so CI/alerting can gate on them. Size/duration swings are\n" +
		"informational (⚠ marker, exit 0).",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id1, id2 := args[0], args[1]

		cfgPath, _ := cmd.Flags().GetString("config")
		if cfgPath == "" {
			cfgPath = os.ExpandEnv("$HOME/.sentinel/config.yaml")
		}
		outputFmt := verifyOutputFormat(cmd)

		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			verifyPrintError(outputFmt, id1, "", fmt.Sprintf("failed to load config: %v", err))
			return fmt.Errorf("load config: %w", ErrVerifyInternal)
		}
		if outputFmt == "" {
			outputFmt = cfg.LogFormat
		}

		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			verifyPrintError(outputFmt, id1, "", fmt.Sprintf("failed to open history db: %v", err))
			return fmt.Errorf("open history db: %w", ErrVerifyInternal)
		}
		defer mon.Close()

		ctx := context.Background()

		s1, err := resolveDiffExecution(ctx, mon, id1)
		if err != nil {
			verifyPrintError(outputFmt, id1, "",
				fmt.Sprintf("backup ID %q not found in history\n  Fix: run 'sentinel monitor list' to see available backup IDs", id1))
			return fmt.Errorf("backup %q: %w", id1, ErrVerifyNotFound)
		}
		s2, err := resolveDiffExecution(ctx, mon, id2)
		if err != nil {
			verifyPrintError(outputFmt, id2, "",
				fmt.Sprintf("backup ID %q not found in history\n  Fix: run 'sentinel monitor list' to see available backup IDs", id2))
			return fmt.Errorf("backup %q: %w", id2, ErrVerifyNotFound)
		}

		var warnings []string
		if m, warn, mErr := resolveDiffManifest(ctx, cfg, s1.exec); mErr != nil {
			verifyPrintError(outputFmt, id1, s1.exec.BackupName, fmt.Sprintf("failed to read manifest: %v", mErr))
			return fmt.Errorf("read manifest for %q: %w", id1, ErrVerifyInternal)
		} else {
			s1.manifest = m
			if warn != "" {
				warnings = append(warnings, warn)
			}
		}
		if m, warn, mErr := resolveDiffManifest(ctx, cfg, s2.exec); mErr != nil {
			verifyPrintError(outputFmt, id2, s2.exec.BackupName, fmt.Sprintf("failed to read manifest: %v", mErr))
			return fmt.Errorf("read manifest for %q: %w", id2, ErrVerifyInternal)
		} else {
			s2.manifest = m
			if warn != "" {
				warnings = append(warnings, warn)
			}
		}

		fields, regressions := computeBackupDiff(s1, s2)
		result := diffResult{
			ID1:                 id1,
			ID2:                 id2,
			Differences:         fields,
			SecurityRegressions: regressions,
			Warnings:            warnings,
		}
		if result.Differences == nil {
			result.Differences = []diffFieldResult{}
		}
		if result.SecurityRegressions == nil {
			result.SecurityRegressions = []string{}
		}

		if outputFmt == "json" {
			verifyPrintJSON(result)
		} else {
			renderDiffText(result)
		}

		if result.hasSecurityRegression() {
			return fmt.Errorf("backup diff %s..%s: %w", id1, id2, ErrDiffSecurityRegression)
		}
		return nil
	},
}

// resolveDiffExecution fetches a backup's monitor row. It never opens the
// artifact referenced by FilePath.
func resolveDiffExecution(ctx context.Context, mon *monitor.Monitor, id string) (diffSide, error) {
	exec, err := mon.GetExecution(ctx, id)
	if err != nil || exec == nil {
		return diffSide{}, fmt.Errorf("execution %q not found", id)
	}
	return diffSide{id: id, exec: exec}, nil
}

// resolveDiffManifest reads the .manifest.json sidecar for one backup, reusing
// the byte-identical resolution `backup verify` uses. It returns:
//   - (m, "", nil)          — manifest read successfully;
//   - (nil, warning, nil)   — soft signal (pre-v1.1 backup with no manifest, or
//     a remote manifest that could not be fetched) ⇒ partial diff;
//   - (nil, "", err)        — a hard error (a present-but-corrupt manifest).
//
// It NEVER downloads or opens the backup artifact — for remote backups it
// fetches only the small <key>.manifest.json sidecar (PRD 36 Q2).
func resolveDiffManifest(ctx context.Context, cfg *config.Configuration, exec *ports.Execution) (*ports.BackupManifest, string, error) {
	missingWarn := fmt.Sprintf("no manifest for backup %s (pre-v1.1 backup); compared monitor-row fields only", exec.ID)

	if exec.FilePath == "" {
		return nil, missingWarn, nil
	}

	manifestPath := exec.FilePath + ".manifest.json"

	if isRemoteStorageBackend(exec.StorageBackend) {
		tmpDir, tmpErr := os.MkdirTemp("", "sentinel-diff-*")
		if tmpErr != nil {
			return nil, "", fmt.Errorf("failed to create temp dir: %w", tmpErr)
		}
		defer os.RemoveAll(tmpDir)

		var storageCfg config.StorageConfig
		if job, ok := cfg.Databases[exec.BackupName]; ok {
			storageCfg = job.Storage
		}

		local, fetchErr := fetchRemoteManifestForDiff(ctx, exec, storageCfg, tmpDir)
		if fetchErr != nil {
			if errors.Is(fetchErr, ports.ErrNoManifest) {
				return nil, missingWarn, nil
			}
			// Degrade gracefully on an operational fetch failure rather than
			// blocking the whole diff (PRD 36 Q2): warn and compare the
			// monitor-row fields only.
			return nil, fmt.Sprintf("could not fetch remote manifest for backup %s: %v; compared monitor-row fields only", exec.ID, fetchErr), nil
		}
		manifestPath = local
	}

	m, err := manifest.ReadManifest(manifestPath)
	if err != nil {
		if errors.Is(err, ports.ErrNoManifest) {
			return nil, missingWarn, nil
		}
		return nil, "", err
	}
	return m, "", nil
}

// fetchRemoteManifestForDiff downloads ONLY the <key>.manifest.json sidecar for
// a remote backup into tmpDir, reusing verify's storage seam
// (verifyBackendParamsAndObject + newVerifyBackend). It returns the local
// sidecar path, ports.ErrNoManifest if the sidecar object is absent, or an
// operational error. The backup artifact itself is never referenced.
func fetchRemoteManifestForDiff(ctx context.Context, exec *ports.Execution, storageCfg config.StorageConfig, tmpDir string) (string, error) {
	params, object, err := verifyBackendParamsAndObject(exec.StorageBackend, exec.FilePath, storageCfg)
	if err != nil {
		return "", err
	}

	backend, err := newVerifyBackend(params)
	if err != nil {
		return "", fmt.Errorf("failed to initialize %s backend: %w", exec.StorageBackend, err)
	}

	manifestObject := object + ".manifest.json"
	exists, err := backend.Exists(ctx, manifestObject)
	if err != nil {
		return "", fmt.Errorf("failed to check manifest sidecar %q: %w", manifestObject, err)
	}
	if !exists {
		return "", ports.ErrNoManifest
	}

	local := filepath.Join(tmpDir, filepath.Base(manifestObject))
	if err := backend.Download(ctx, manifestObject, local); err != nil {
		return "", fmt.Errorf("failed to download manifest sidecar %q: %w", manifestObject, err)
	}
	return local, nil
}

// computeBackupDiff is the pure comparison over two resolved sides. It returns
// only the fields that differ plus the deduplicated security-regression labels.
// It reads nothing but struct fields, so it demonstrably touches zero artifact
// bytes.
func computeBackupDiff(s1, s2 diffSide) ([]diffFieldResult, []string) {
	var fields []diffFieldResult
	var regressions []string

	// size_bytes — manifest first, monitor-row fallback.
	if b1, b2 := diffSizeBytes(s1), diffSizeBytes(s2); b1 != b2 {
		delta, big := pctDelta(float64(b1), float64(b2))
		if big {
			delta = "⚠ " + delta
		}
		fields = append(fields, diffFieldResult{
			Field: "size_bytes", Before: humanizeBytes(b1), After: humanizeBytes(b2),
			Delta: delta, Warn: big,
		})
	}

	// duration_ms — monitor row only.
	if d1, d2 := s1.exec.DurationMs, s2.exec.DurationMs; d1 != d2 {
		delta, big := pctDelta(float64(d1), float64(d2))
		if big {
			delta = "⚠ " + delta
		}
		fields = append(fields, diffFieldResult{
			Field: "duration_ms", Before: strconv.FormatInt(d1, 10), After: strconv.FormatInt(d2, 10),
			Delta: delta, Warn: big,
		})
	}

	// Manifest-derived fields require BOTH manifests (hash + encryption).
	if s1.hasManifest() && s2.hasManifest() {
		mf, mr := diffManifestFields(s1.manifest, s2.manifest)
		fields = append(fields, mf...)
		regressions = append(regressions, mr...)
	}

	// backup_type — monitor row first, manifest lineage fallback.
	if t1, t2 := diffBackupType(s1), diffBackupType(s2); t1 != t2 && (t1 != "" || t2 != "") {
		fields = append(fields, diffFieldResult{
			Field: "backup_type", Before: orNone(t1), After: orNone(t2), Delta: "changed",
		})
	}

	// chain_depth — manifest lineage first, monitor row fallback.
	if c1, c2 := diffChainDepth(s1), diffChainDepth(s2); c1 != c2 {
		delta := fmt.Sprintf("%+d", c2-c1)
		if c2 == 0 && c1 > 0 {
			delta = "reset"
		}
		fields = append(fields, diffFieldResult{
			Field: "chain_depth", Before: strconv.Itoa(c1), After: strconv.Itoa(c2), Delta: delta,
		})
	}

	return fields, dedupeStrings(regressions)
}

// diffManifestFields compares the manifest-only fields (hash + encryption) and
// returns the differing rows plus any security-regression labels.
func diffManifestFields(m1, m2 *ports.BackupManifest) ([]diffFieldResult, []string) {
	var fields []diffFieldResult
	var regressions []string

	// hash.algorithm — a change is a security signal (e.g. sha256 → sha1).
	if a1, a2 := m1.Hash.Algorithm, m2.Hash.Algorithm; a1 != a2 {
		fields = append(fields, diffFieldResult{
			Field: "hash.algorithm", Before: a1, After: a2,
			Delta: "⚠ HASH ALGORITHM CHANGED", Security: true,
		})
		regressions = append(regressions, "HASH ALGORITHM CHANGED")
	}

	// hash.value — expected to differ; informational.
	if m1.Hash.Value != m2.Hash.Value {
		fields = append(fields, diffFieldResult{
			Field: "hash.value", Before: shortHash(m1.Hash.Value), After: shortHash(m2.Hash.Value),
			Delta: "changed",
		})
	}

	e1, e2 := m1.Encryption, m2.Encryption
	switch {
	case e1 != nil && e2 == nil:
		// The single highest-value signal: a backup silently became plaintext.
		fields = append(fields, diffFieldResult{
			Field: "encryption", Before: orNone(e1.Algorithm), After: "(none)",
			Delta: "⚠ ENCRYPTION DISABLED", Security: true,
		})
		regressions = append(regressions, "ENCRYPTION DISABLED")
	case e1 == nil && e2 != nil:
		// Encryption turned on — an improvement, not a regression.
		fields = append(fields, diffFieldResult{
			Field: "encryption", Before: "(none)", After: orNone(e2.Algorithm), Delta: "enabled",
		})
	case e1 != nil && e2 != nil:
		if diffEncryptionParams(e1, e2, &fields) {
			regressions = append(regressions, "ENCRYPTION WEAKENED")
		}
	}

	return fields, regressions
}

// diffEncryptionParams appends a row for each differing encryption parameter and
// reports whether any change constitutes a downgrade (algorithm or key
// derivation changed, iterations decreased, or envelope version dropped). A
// change of the crypto algorithm or KDF between consecutive backups is treated
// conservatively as a downgrade signal (deterministic, no algorithm ranking).
func diffEncryptionParams(e1, e2 *ports.EncryptionInfo, fields *[]diffFieldResult) bool {
	weakened := false

	if e1.Algorithm != e2.Algorithm {
		*fields = append(*fields, diffFieldResult{
			Field: "encryption.algorithm", Before: orNone(e1.Algorithm), After: orNone(e2.Algorithm),
			Delta: "⚠ changed", Security: true,
		})
		weakened = true
	}
	if e1.KeyDerivation != e2.KeyDerivation {
		*fields = append(*fields, diffFieldResult{
			Field: "encryption.key_derivation", Before: orNone(e1.KeyDerivation), After: orNone(e2.KeyDerivation),
			Delta: "⚠ changed", Security: true,
		})
		weakened = true
	}
	if e1.Iterations != e2.Iterations {
		down := e2.Iterations < e1.Iterations
		delta := "increased"
		if down {
			delta = "⚠ decreased"
			weakened = true
		}
		*fields = append(*fields, diffFieldResult{
			Field: "encryption.iterations", Before: strconv.Itoa(e1.Iterations), After: strconv.Itoa(e2.Iterations),
			Delta: delta, Security: down,
		})
	}
	if e1.EnvelopeVersion != e2.EnvelopeVersion {
		down := e2.EnvelopeVersion < e1.EnvelopeVersion
		delta := "upgraded"
		if down {
			delta = "⚠ dropped"
			weakened = true
		}
		*fields = append(*fields, diffFieldResult{
			Field: "encryption.envelope_version", Before: strconv.Itoa(e1.EnvelopeVersion), After: strconv.Itoa(e2.EnvelopeVersion),
			Delta: delta, Security: down,
		})
	}

	return weakened
}

// diffSizeBytes prefers the manifest-recorded size, falling back to the monitor
// row's recorded file size.
func diffSizeBytes(s diffSide) int64 {
	if s.manifest != nil && s.manifest.SizeBytes > 0 {
		return s.manifest.SizeBytes
	}
	return s.exec.FileSizeBytes
}

// diffBackupType prefers the monitor row's BackupType (PRD 36 Q3), deriving from
// the manifest incremental lineage only when the row is empty.
func diffBackupType(s diffSide) string {
	if s.exec.BackupType != "" {
		return s.exec.BackupType
	}
	if lineage := diffLineage(s.manifest); lineage != nil && lineage.Enabled {
		if lineage.ChainIndex > 0 {
			return "incremental"
		}
		return "full"
	}
	return ""
}

// diffChainDepth prefers the manifest lineage chain index, falling back to the
// monitor row's chain index.
func diffChainDepth(s diffSide) int {
	if lineage := diffLineage(s.manifest); lineage != nil {
		return lineage.ChainIndex
	}
	return s.exec.ChainIndex
}

// diffLineage nil-safely reaches a manifest's incremental lineage metadata.
func diffLineage(m *ports.BackupManifest) *ports.IncrementalLineageMetadata {
	if m == nil || m.AdvancedRestore == nil {
		return nil
	}
	return m.AdvancedRestore.IncrementalLineage
}

// pctDelta renders the signed percentage change from before to after and
// reports whether its magnitude reaches the large-swing threshold.
func pctDelta(before, after float64) (string, bool) {
	if before == 0 {
		if after == 0 {
			return "0%", false
		}
		return "new", true
	}
	pct := (after - before) / before * 100
	return fmt.Sprintf("%+.0f%%", pct), math.Abs(pct) >= diffLargeSwingPct
}

// humanizeBytes renders a byte count in a compact human-readable form.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// shortHash truncates a long fingerprint for display; the exact bytes are not
// actionable in a diff (use `backup verify` for that).
func shortHash(h string) string {
	const keep = 12
	if len(h) > keep {
		return h[:keep] + "…"
	}
	return h
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// dedupeStrings removes duplicate entries while preserving first-seen order.
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// renderDiffText prints warnings, the FIELD|BEFORE|AFTER|DELTA table (only
// differing fields), and a security-regression summary when present.
func renderDiffText(r diffResult) {
	for _, w := range r.Warnings {
		fmt.Printf("Warning: %s\n", w)
	}

	if len(r.Differences) == 0 {
		fmt.Printf("No metadata differences between %s and %s.\n", r.ID1, r.ID2)
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tBEFORE\tAFTER\tDELTA")
	for _, f := range r.Differences {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Field, f.Before, f.After, f.Delta)
	}
	_ = tw.Flush()

	if len(r.SecurityRegressions) > 0 {
		fmt.Printf("\n⚠ Security regression detected: %s\n", strings.Join(r.SecurityRegressions, ", "))
	}
}

func init() {
	BackupCmd.AddCommand(backupDiffCmd)
	backupDiffCmd.Flags().String("config", "", "Path to sentinel YAML config")
	backupDiffCmd.Flags().String("output", "", "Output format: json or text")
	backupDiffCmd.Flags().String("format", "", "Alias for --output")
	_ = backupDiffCmd.Flags().MarkHidden("format")
}
