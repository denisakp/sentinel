package cli

// `sentinel repair` — repository-wide internal-state reconciliation.
//
// Repair is a DRIVING/COMPOSITION command over existing adapters and domain
// functions (ADR 0001): it reconciles the three sources of truth — monitor
// SQLite rows, manifest sidecars, and storage artifacts — plus the lock
// directory. It introduces NO new port and NO adapter->adapter cross-import.
//
// It is NOT `monitor doctor --repair` (that is schema-only, one file). Repair
// detects six drift classes across every configured job's storage backend and,
// on opt-in, applies the recoverable fixes.
//
// Flag matrix (Q1 default = report-only; destructive actions strictly opt-in):
//
//	(no flags) / --dry-run : report every drift class, mutate NOTHING.
//	--fix                  : apply recoverable classes 4/5/6 (finalize stale
//	                         running rows, remove stale locks, mark broken chains).
//	--purge-orphans        : additionally delete orphan artifacts (class 1),
//	                         guarded by confirmation (--yes) + active-baseline
//	                         protection; implies --fix.
//	--dry-run              : forces report-only even combined with the above.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/lock"
	manifeststore "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	storage "github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	domainmanifest "github.com/denisakp/sentinel/internal/domain/manifest"
	incremental "github.com/denisakp/sentinel/internal/domain/restore/incremental"
	"github.com/denisakp/sentinel/internal/domain/retention"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

// Drift class identifiers (stable across releases; used in text + JSON output).
const (
	driftOrphanArtifact    = "orphan_artifact"
	driftUntrackedArtifact = "untracked_artifact"
	driftOrphanManifest    = "orphan_manifest"
	driftArtifactMissing   = "artifact_missing"
	driftStaleRunning      = "stale_running"
	driftStaleLock         = "stale_lock"
	driftChainBroken       = "chain_broken"
)

// repairListLimit bounds the number of executions the repair sweep enumerates
// from the monitor, mirroring the verify/retention fetch caps.
const repairListLimit = 100000

// newRepairBackend is a test seam over the storage registry (mirrors
// newVerifyBackend / newRetentionDeleteBackend).
var newRepairBackend = func(p *storage.BackendParams) (ports.StorageBackend, error) {
	return storage.NewBackend(p)
}

// repairConfirm prompts on out and reads a yes/no answer from in. Package-level
// var so tests can override.
var repairConfirm = func(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprint(out, prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// repairMode captures the resolved action posture from the flag matrix.
type repairMode struct {
	reportOnly bool // true: no mutation of any kind (no-flag / --dry-run)
	fix        bool // apply recoverable classes 4/5/6
	purge      bool // additionally delete orphan artifacts (class 1)
	assumeYes  bool // skip interactive confirmation for purge
}

// doRecoverable reports whether recoverable fixes (4/5/6) should be applied.
func (m repairMode) doRecoverable() bool { return !m.reportOnly && (m.fix || m.purge) }

// doPurge reports whether orphan-artifact deletion (class 1) should be applied.
func (m repairMode) doPurge() bool { return !m.reportOnly && m.purge }

// repairFinding is one detected drift item plus the action taken/intended.
type repairFinding struct {
	Class      string `json:"class"`
	Job        string `json:"job,omitempty"`
	Repository string `json:"repository,omitempty"`
	Path       string `json:"path,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Action     string `json:"action"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	Applied    bool   `json:"applied"`
}

// orphanCandidate carries what purge needs to delete an orphan artifact.
type orphanCandidate struct {
	repoKey    string
	repoLabel  string
	backend    ports.StorageBackend
	objectPath string // storage.List Path, deletable directly via backend.Delete
	base       string // filepath.Base, join key for baseline protection
	size       int64
}

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Reconcile internal state across monitor rows, manifests, artifacts, and locks",
	Long: "Detect and (on opt-in) fix repository-wide state drift: orphan artifacts, orphan\n" +
		"manifests, missing artifacts, stale running executions, stale locks, and broken\n" +
		"incremental chains. Report-only by default; pass --fix to apply recoverable fixes\n" +
		"and --purge-orphans to delete orphan artifacts.\n\n" +
		"This is NOT `monitor doctor --repair` (schema-only). Always run with --dry-run first.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runRepairCommand(cmd)
	},
}

func runRepairCommand(cmd *cobra.Command) error {
	mode := resolveRepairMode(cmd)
	format, _ := cmd.Flags().GetString("format")
	jobFilter, _ := cmd.Flags().GetString("job")

	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return fmt.Errorf("load config: %w: %v", ErrRepairInternal, err)
	}

	// Schema-health precondition: refuse to reconcile against a schema this
	// binary cannot trust. Diagnose is read-only and never migrates.
	report, derr := monitor.Diagnose(cfg.HistoryDBPath)
	if derr != nil {
		return fmt.Errorf("diagnose monitor db: %w: %v", ErrRepairInternal, derr)
	}
	if report.Status != monitor.StatusCurrent {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: monitor schema is %q; repair refuses to run.\n", report.Status)
		if report.Hint != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "  Fix: %s\n", report.Hint)
		}
		return schemaRefusalError(report.Status)
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return fmt.Errorf("open history db: %w: %v", ErrRepairInternal, err)
	}
	defer mon.Close()

	lm := lock.NewManager(cfg.Scheduler.LockDir)

	findings, err := runRepair(cmd.Context(), cmd, cfg, mon, lm, mode, jobFilter)
	if err != nil {
		return err
	}

	if strings.EqualFold(format, "json") {
		renderRepairJSON(cmd.OutOrStdout(), findings, mode)
	} else {
		renderRepairText(cmd.OutOrStdout(), findings, mode)
	}

	return repairExitError(findings)
}

// resolveRepairMode maps the flags to a posture. --dry-run always forces
// report-only; --purge-orphans implies --fix.
func resolveRepairMode(cmd *cobra.Command) repairMode {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	fix, _ := cmd.Flags().GetBool("fix")
	purge, _ := cmd.Flags().GetBool("purge-orphans")
	yes, _ := cmd.Flags().GetBool("yes")

	m := repairMode{fix: fix, purge: purge, assumeYes: yes}
	// Report-only when no action flag is set OR --dry-run is present.
	m.reportOnly = dryRun || (!fix && !purge)
	return m
}

// runRepair performs detection across all target jobs and applies fixes per the
// mode. It returns the ordered finding list.
func runRepair(ctx context.Context, cmd *cobra.Command, cfg *config.Configuration, mon *monitor.Monitor, lm *lock.Manager, mode repairMode, jobFilter string) ([]repairFinding, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	allRows, err := mon.ListExecutions(ctx, &ports.Filter{}, repairListLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w: %v", ErrRepairInternal, err)
	}

	// Global tracked base-name set (all rows, any status): an artifact is
	// "known" if ANY row references it — this avoids flagging one job's
	// artifacts as orphans on a bucket shared with another job.
	trackedBases := map[string]struct{}{}
	for i := range allRows {
		if b := baseName(allRows[i].FilePath); b != "" {
			trackedBases[b] = struct{}{}
		}
	}

	// Success records (newest-first) for baseline protection.
	records := successRecords(allRows)

	repos := collectRepos(cfg, jobFilter)

	var findings []repairFinding
	var purgeCandidates []orphanCandidate

	// Per-repo storage enumeration (orphan_artifact / untracked / orphan_manifest).
	for _, repo := range repos {
		params, prefix, perr := repairBackendParams(repo.cfg)
		if perr != nil {
			findings = append(findings, repairFinding{
				Class: driftOrphanArtifact, Repository: repo.label,
				Detail: perr.Error(), Action: "skipped (unsupported storage)",
			})
			continue
		}
		backend, berr := newRepairBackend(params)
		if berr != nil {
			findings = append(findings, repairFinding{
				Class: driftOrphanArtifact, Repository: repo.label,
				Detail: fmt.Sprintf("backend init: %v", berr), Action: "skipped (backend error)",
			})
			continue
		}
		objs, lerr := backend.List(ctx, prefix)
		if lerr != nil {
			// Degrade: orphan classes need the listing; row-driven classes still run.
			findings = append(findings, repairFinding{
				Class: driftOrphanArtifact, Repository: repo.label,
				Detail: fmt.Sprintf("list failed: %v", lerr), Action: "skipped (list error)",
			})
			repo.presentBases = nil
			continue
		}
		repo.presentBases = presentBaseSet(objs)

		orphans, cands := classifyStorageObjects(objs, trackedBases, backend, repo)
		findings = append(findings, orphans...)
		purgeCandidates = append(purgeCandidates, cands...)
	}

	// Per-job classes: artifact_missing (class 3) + chain_broken (class 6).
	for _, repo := range repos {
		for _, job := range repo.jobs {
			jobRows := rowsForJob(allRows, job)
			findings = append(findings, classifyMissing(jobRows, repo.presentBases, repo.label)...)
			findings = append(findings, detectBrokenChains(ctx, jobRows, repo)...)
		}
	}

	// stale_running (class 4) — GetStaleRunningExecutions is global; scope to
	// target jobs, lock-guarded, host-local honest.
	findings = append(findings, reconcileStaleRunning(ctx, mon, lm, cfg, mode, repos)...)

	// stale_lock (class 5) — host-local lock directory scan.
	findings = append(findings, reconcileStaleLocks(lm, cfg, mode, repos)...)

	// orphan purge (class 1) — the ONLY artifact-deleting path, guarded.
	findings = applyOrphanPurge(ctx, cmd, findings, purgeCandidates, records, mode)

	return findings, nil
}

// classifyStorageObjects detects orphan_artifact (no sidecar AND no row),
// untracked_artifact (sidecar present but no row — soft, never purged), and
// orphan_manifest (sidecar present, referenced artifact absent). Pure over the
// listing + tracked set; backend/repo carried only to build purge candidates.
func classifyStorageObjects(objs []ports.StorageObject, trackedBases map[string]struct{}, backend ports.StorageBackend, repo *repairRepo) ([]repairFinding, []orphanCandidate) {
	artifactBases := map[string]struct{}{}
	for _, o := range objs {
		if !isManifestKey(o.Path) {
			artifactBases[baseName(o.Path)] = struct{}{}
		}
	}

	var findings []repairFinding
	var cands []orphanCandidate

	for _, o := range objs {
		base := baseName(o.Path)
		if isManifestKey(o.Path) {
			// orphan_manifest: referenced artifact absent from the listing.
			ref := manifestRefBase(o.Path)
			if _, ok := artifactBases[ref]; !ok {
				findings = append(findings, repairFinding{
					Class: driftOrphanManifest, Repository: repo.label, Path: o.Path,
					Detail: "referenced artifact absent", Action: "report (kept; no artifact to purge)",
				})
			}
			continue
		}

		_, hasRow := trackedBases[base]
		_, hasSidecar := manifestSidecarPresent(objs, base)
		switch {
		case !hasRow && !hasSidecar:
			findings = append(findings, repairFinding{
				Class: driftOrphanArtifact, Repository: repo.label, Path: o.Path,
				SizeBytes: o.SizeBytes, Action: "report (use --purge-orphans to delete)",
			})
			cands = append(cands, orphanCandidate{
				repoKey: repo.key, repoLabel: repo.label, backend: backend,
				objectPath: o.Path, base: base, size: o.SizeBytes,
			})
		case !hasRow && hasSidecar:
			// Q3: sidecar present but no row is a softer untracked_artifact.
			findings = append(findings, repairFinding{
				Class: driftUntrackedArtifact, Repository: repo.label, Path: o.Path,
				SizeBytes: o.SizeBytes, Detail: "manifest present, no monitor row",
				Action: "report (never auto-purged)",
			})
		}
	}
	return findings, cands
}

// classifyMissing detects artifact_missing: a successful backup row whose
// artifact is absent from the storage listing. Manual action required.
func classifyMissing(rows []ports.Execution, presentBases map[string]struct{}, repoLabel string) []repairFinding {
	if presentBases == nil {
		return nil // listing unavailable/degraded — do not guess
	}
	var findings []repairFinding
	for i := range rows {
		r := rows[i]
		if r.Status != ports.StatusSuccess && r.Status != ports.StatusCompleted {
			continue
		}
		base := baseName(r.FilePath)
		if base == "" {
			continue
		}
		if _, ok := presentBases[base]; !ok {
			findings = append(findings, repairFinding{
				Class: driftArtifactMissing, Job: r.BackupName, Repository: repoLabel,
				Path: r.FilePath, Detail: "recorded artifact absent from storage",
				Action: "manual action required",
			})
		}
	}
	return findings
}

// detectBrokenChains groups a job's incremental rows by ChainID, loads each
// member's manifest for lineage fields, and runs the SAME resolver that powers
// `restore validate-chain` (incremental.ResolveOrderedChain) plus the pure
// lineage-field validator. Repair does NOT re-hash: chain detection is
// structural, so HashVerified is asserted true and only missing / non-contiguous
// / mismatched links surface as chain_broken.
func detectBrokenChains(ctx context.Context, rows []ports.Execution, repo *repairRepo) []repairFinding {
	chains := map[string][]ports.Execution{}
	order := []string{}
	for i := range rows {
		r := rows[i]
		if r.ChainID == "" {
			continue
		}
		if r.Status != ports.StatusSuccess && r.Status != ports.StatusCompleted {
			continue
		}
		if _, seen := chains[r.ChainID]; !seen {
			order = append(order, r.ChainID)
		}
		chains[r.ChainID] = append(chains[r.ChainID], r)
	}

	var findings []repairFinding
	for _, chainID := range order {
		members := chains[chainID]
		sort.SliceStable(members, func(i, j int) bool { return members[i].ChainIndex < members[j].ChainIndex })

		arts := make([]incremental.ChainArtifact, 0, len(members))
		var lineageErr string
		for i := range members {
			m := members[i]
			man := loadManifestForRow(ctx, repo, m)
			art := incremental.ChainArtifact{
				BackupID:        m.ID,
				ChainIndex:      m.ChainIndex,
				ManifestPresent: man != nil,
				HashVerified:    true, // structural check only; repair does not re-hash
			}
			if man != nil {
				if man.BackupID != "" {
					art.BackupID = man.BackupID
				}
				art.HashValue = man.Hash.Value
				art.HashAlgorithm = man.Hash.Algorithm
				if man.AdvancedRestore != nil && man.AdvancedRestore.IncrementalLineage != nil {
					lin := man.AdvancedRestore.IncrementalLineage
					art.BaselineBackupID = lin.BaselineBackupID
					art.TimelineID = lin.TimelineID
				}
				if verr := domainmanifest.ValidateIncrementalLineageContract(man); verr != nil && lineageErr == "" {
					lineageErr = verr.Error()
				}
			}
			arts = append(arts, art)
		}

		_, rerr := incremental.ResolveOrderedChain(arts, "")
		if rerr != nil {
			findings = append(findings, repairFinding{
				Class: driftChainBroken, Job: firstJob(members), Repository: repo.label,
				Detail:  fmt.Sprintf("chain=%s: %v", chainID, rerr),
				Action:  "manual action required (blocks incremental restore)",
			})
			continue
		}
		if lineageErr != "" {
			findings = append(findings, repairFinding{
				Class: driftChainBroken, Job: firstJob(members), Repository: repo.label,
				Detail:  fmt.Sprintf("chain=%s: %s", chainID, lineageErr),
				Action:  "manual action required (blocks incremental restore)",
			})
		}
	}
	return findings
}

// reconcileStaleRunning finalizes stale running rows to `interrupted`, but ONLY
// when no live lock is held for the job (the correctness improvement over the
// blind ReconcileStaleExecutions). Foreign-host locks are skipped with a
// warning (host-scoping honesty).
func reconcileStaleRunning(ctx context.Context, mon *monitor.Monitor, lm *lock.Manager, cfg *config.Configuration, mode repairMode, repos []*repairRepo) []repairFinding {
	staleRows, err := mon.GetStaleRunningExecutions(ctx)
	if err != nil {
		return []repairFinding{{
			Class: driftStaleRunning, Detail: fmt.Sprintf("query failed: %v", err),
			Action: "skipped (query error)",
		}}
	}

	targets := jobLabelIndex(repos)
	threshold := staleThreshold(cfg)
	thisHost, _ := os.Hostname()

	var findings []repairFinding
	for i := range staleRows {
		r := staleRows[i]
		label, ok := targets[r.BackupName]
		if !ok {
			continue // not a target job
		}

		jl, _ := lm.ReadLock(r.BackupName)
		decision, detail := classifyStaleRunning(jl, threshold, thisHost)

		f := repairFinding{
			Class: driftStaleRunning, Job: r.BackupName, Repository: label,
			Path: r.ID, Detail: detail,
		}
		switch decision {
		case staleSkipLive:
			f.Action = "skipped (live lock)"
		case staleSkipForeign:
			f.Action = "skipped (foreign-host lock)"
		case staleFinalize:
			if mode.doRecoverable() {
				if rerr := mon.RecordInterrupted(ctx, r.ID, false, nil, "unclean shutdown (repair)"); rerr != nil {
					f.Action = fmt.Sprintf("finalize failed: %v", rerr)
				} else {
					f.Action = "marked interrupted"
					f.Applied = true
				}
			} else {
				f.Action = "would mark interrupted"
			}
		}
		findings = append(findings, f)
	}
	return findings
}

type staleDecision int

const (
	staleFinalize staleDecision = iota
	staleSkipLive
	staleSkipForeign
)

// classifyStaleRunning decides how to handle a stale running row given the
// current lock (may be nil). Pure.
func classifyStaleRunning(jl *ports.JobLock, threshold time.Duration, thisHost string) (staleDecision, string) {
	if jl == nil {
		return staleFinalize, "no live lock"
	}
	if jl.Hostname != "" && thisHost != "" && jl.Hostname != thisHost {
		return staleSkipForeign, fmt.Sprintf("lock held on host %q (pid=%d)", jl.Hostname, jl.PID)
	}
	state := lock.EvaluateLockState(jl, threshold)
	if state.Live {
		return staleSkipLive, fmt.Sprintf("live lock pid=%d", jl.PID)
	}
	return staleFinalize, fmt.Sprintf("no live lock (dead pid=%d)", jl.PID)
}

// reconcileStaleLocks reports removable locks and, when applying, removes them
// via ScanStale (which already skips foreign-host and live locks).
func reconcileStaleLocks(lm *lock.Manager, cfg *config.Configuration, mode repairMode, repos []*repairRepo) []repairFinding {
	threshold := staleThreshold(cfg)
	thisHost, _ := os.Hostname()

	files, err := lm.ListLockFiles()
	if err != nil {
		return []repairFinding{{
			Class: driftStaleLock, Detail: fmt.Sprintf("list lock files failed: %v", err),
			Action: "skipped (list error)",
		}}
	}

	labels := jobLabelIndex(repos)

	// Detect removable locks first (report intent), then apply once via ScanStale.
	type pending struct {
		idx  int
		path string
	}
	var findings []repairFinding
	var removable []pending
	for _, path := range files {
		job := strings.TrimSuffix(filepath.Base(path), ".lock")
		jl, rerr := lm.ReadLock(job)
		if rerr != nil || jl == nil {
			continue
		}
		if jl.Hostname != "" && thisHost != "" && jl.Hostname != thisHost {
			continue // foreign-host lock: never touched
		}
		state := lock.EvaluateLockState(jl, threshold)
		if !state.Removable {
			continue
		}
		f := repairFinding{
			Class: driftStaleLock, Job: job, Repository: labels[job], Path: path,
			Detail: fmt.Sprintf("dead pid=%d age=%s", state.PID, state.Age.Truncate(time.Second)),
		}
		if mode.doRecoverable() {
			f.Action = "would remove" // provisionally; upgraded to "removed" below
		} else {
			f.Action = "report (use --fix to remove)"
		}
		removable = append(removable, pending{idx: len(findings), path: path})
		findings = append(findings, f)
	}

	if mode.doRecoverable() && len(removable) > 0 {
		removed, _ := lm.ScanStale(threshold)
		removedSet := map[string]struct{}{}
		for _, p := range removed {
			removedSet[p] = struct{}{}
		}
		for _, p := range removable {
			if _, ok := removedSet[p.path]; ok {
				findings[p.idx].Action = "removed"
				findings[p.idx].Applied = true
			} else {
				findings[p.idx].Action = "kept (scan skipped it)"
			}
		}
	}
	return findings
}

// applyOrphanPurge deletes orphan artifacts (class 1) when --purge-orphans is
// active. It protects active chain baselines via retention.ProtectActiveBaseline
// and requires confirmation unless --yes. In report-only / no-purge modes the
// candidates are already reported by classifyStorageObjects; this only mutates
// findings that get deleted.
func applyOrphanPurge(ctx context.Context, cmd *cobra.Command, findings []repairFinding, candidates []orphanCandidate, records []retention.BackupRecord, mode repairMode) []repairFinding {
	if len(candidates) == 0 || !mode.doPurge() {
		return findings
	}

	purge, protected := filterProtectedOrphans(candidates, records)

	// Reflect baseline protection in the corresponding findings.
	protectedPaths := map[string]struct{}{}
	for _, c := range protected {
		protectedPaths[c.objectPath] = struct{}{}
	}
	for i := range findings {
		if findings[i].Class == driftOrphanArtifact {
			if _, ok := protectedPaths[findings[i].Path]; ok {
				findings[i].Action = "kept (protected active baseline)"
			}
		}
	}

	if len(purge) == 0 {
		return findings
	}

	confirmed := mode.assumeYes
	if !confirmed {
		var total int64
		for _, c := range purge {
			total += c.size
		}
		prompt := fmt.Sprintf("Delete %d orphan artifact(s) (%d bytes)? [y/N] ", len(purge), total)
		confirmed = repairConfirm(cmd.InOrStdin(), cmd.OutOrStdout(), prompt)
	}

	purgeSet := map[string]struct{}{}
	for _, c := range purge {
		purgeSet[c.objectPath] = struct{}{}
	}

	for _, c := range purge {
		var action string
		var applied bool
		if !confirmed {
			action = "kept (not confirmed)"
		} else if err := c.backend.Delete(ctx, c.objectPath); err != nil {
			action = fmt.Sprintf("delete failed: %v", err)
		} else {
			action = "deleted"
			applied = true
		}
		for i := range findings {
			if findings[i].Class == driftOrphanArtifact && findings[i].Path == c.objectPath {
				findings[i].Action = action
				findings[i].Applied = applied
			}
		}
	}
	return findings
}

// filterProtectedOrphans partitions orphan candidates into purgeable vs
// protected-active-baseline, wiring retention.ProtectActiveBaseline over a
// base-name join. An orphan whose base name matches the active chain's baseline
// is protected even under --purge-orphans.
func filterProtectedOrphans(candidates []orphanCandidate, records []retention.BackupRecord) (purge, protected []orphanCandidate) {
	byBase := map[string]orphanCandidate{}
	rc := make([]retention.BackupCandidate, 0, len(candidates))
	for _, c := range candidates {
		byBase[c.base] = c
		rc = append(rc, retention.BackupCandidate{FilePath: c.base})
	}

	survivors := retention.ProtectActiveBaseline(rc, records)
	survivingBase := map[string]struct{}{}
	for _, s := range survivors {
		survivingBase[s.FilePath] = struct{}{}
	}

	for _, c := range candidates {
		if _, ok := survivingBase[c.base]; ok {
			purge = append(purge, c)
		} else {
			protected = append(protected, c)
		}
	}
	return purge, protected
}

// ---------- helpers ----------

// repairRepo groups a distinct storage backend identity with the jobs using it.
type repairRepo struct {
	key          string
	label        string
	cfg          config.StorageConfig
	jobs         []string
	presentBases map[string]struct{} // filled during enumeration; nil if listing degraded
}

// collectRepos builds the distinct-repository grouping for the target jobs
// (all enabled jobs, or the single --job).
func collectRepos(cfg *config.Configuration, jobFilter string) []*repairRepo {
	byKey := map[string]*repairRepo{}
	order := []string{}
	// Stable job order.
	names := make([]string, 0, len(cfg.Databases))
	for name := range cfg.Databases {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		job := cfg.Databases[name]
		if job.Enabled != nil && !*job.Enabled {
			continue
		}
		if jobFilter != "" && name != jobFilter {
			continue
		}
		sc := effectiveStorageForJob(cfg, job)
		key := repoKeyFor(sc)
		repo, ok := byKey[key]
		if !ok {
			repo = &repairRepo{key: key, label: repoLabelFor(sc), cfg: sc}
			byKey[key] = repo
			order = append(order, key)
		}
		repo.jobs = append(repo.jobs, name)
	}

	out := make([]*repairRepo, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

func effectiveStorageForJob(cfg *config.Configuration, job config.BackupJob) config.StorageConfig {
	if job.Storage.Type != "" {
		return job.Storage
	}
	if cfg.Defaults.Storage.Type != "" {
		return cfg.Defaults.Storage
	}
	return config.StorageConfig{Type: "local"}
}

func repoKeyFor(sc config.StorageConfig) string {
	switch storageTypeOf(sc) {
	case "s3":
		return "s3:" + sc.S3BucketEndpoint + "/" + sc.S3Bucket
	case "gcs":
		return "gcs:" + sc.GCSBucket
	case "azure":
		return "azure:" + sc.AzureStorageAccount + "/" + sc.AzureContainer
	case "google-drive":
		return "google-drive:" + sc.GDriveFolderID
	default:
		return "local:" + sc.LocalPath
	}
}

func repoLabelFor(sc config.StorageConfig) string {
	switch storageTypeOf(sc) {
	case "s3":
		return "s3://" + sc.S3Bucket
	case "gcs":
		return "gs://" + sc.GCSBucket
	case "azure":
		return "azure://" + sc.AzureContainer
	case "google-drive":
		return "gdrive:" + sc.GDriveFolderID
	default:
		if sc.LocalPath != "" {
			return sc.LocalPath
		}
		return "local"
	}
}

func storageTypeOf(sc config.StorageConfig) string {
	if sc.Type == "" {
		return "local"
	}
	return sc.Type
}

// repairBackendParams builds registry params + a List prefix from a storage
// config, mirroring retention_cleaner.go's per-type construction.
func repairBackendParams(sc config.StorageConfig) (*storage.BackendParams, string, error) {
	switch storageTypeOf(sc) {
	case "local":
		return &storage.BackendParams{StorageType: "local", LocalPath: sc.LocalPath}, "", nil
	case "s3":
		return &storage.BackendParams{
			StorageType:        "s3",
			AWSBucket:          sc.S3Bucket,
			AWSRegion:          sc.S3Region,
			AWSBucketEndpoint:  sc.S3BucketEndpoint,
			AWSAccessKeyID:     sc.S3AccessKeyID,
			AWSSecretAccessKey: sc.S3SecretAccessKey,
		}, "", nil
	case "gcs":
		return &storage.BackendParams{
			StorageType:        "gcs",
			GCSBucket:          sc.GCSBucket,
			GCSProjectID:       sc.GCSProjectID,
			GCSCredentialsFile: sc.GCSCredentialsFile,
		}, "", nil
	case "azure":
		return &storage.BackendParams{
			StorageType:         "azure",
			AzureStorageAccount: sc.AzureStorageAccount,
			AzureStorageKey:     sc.AzureStorageKey,
			AzureContainer:      sc.AzureContainer,
		}, "", nil
	case "google-drive":
		return &storage.BackendParams{
			StorageType:          "google-drive",
			GoogleDriveFolderId:  sc.GDriveFolderID,
			GoogleServiceAccount: sc.GDriveSAFile,
		}, "", nil
	default:
		return nil, "", fmt.Errorf("repair not supported for storage type %q", sc.Type)
	}
}

// loadManifestForRow reads a chain member's manifest. Local: from the recorded
// ManifestPath (or FilePath + .manifest.json). Remote: download the sidecar to
// a temp dir. Returns nil when the manifest cannot be read.
func loadManifestForRow(ctx context.Context, repo *repairRepo, row ports.Execution) *ports.BackupManifest {
	if storageTypeOf(repo.cfg) == "local" {
		path := row.ManifestPath
		if path == "" {
			path = row.FilePath + ".manifest.json"
		}
		m, err := manifeststore.ReadManifest(path)
		if err != nil {
			return nil
		}
		return m
	}

	// Remote: fetch just the sidecar.
	params, _, perr := repairBackendParams(repo.cfg)
	if perr != nil {
		return nil
	}
	backend, berr := newRepairBackend(params)
	if berr != nil {
		return nil
	}
	object, oerr := objectKeyForRow(repo.cfg, row.FilePath)
	if oerr != nil {
		return nil
	}
	tmpDir, terr := os.MkdirTemp("", "sentinel-repair-*")
	if terr != nil {
		return nil
	}
	defer os.RemoveAll(tmpDir)

	local := filepath.Join(tmpDir, filepath.Base(object)+".manifest.json")
	if err := backend.Download(ctx, object+".manifest.json", local); err != nil {
		return nil
	}
	m, err := manifeststore.ReadManifest(local)
	if err != nil {
		return nil
	}
	return m
}

// objectKeyForRow resolves the storage object key for a remote artifact
// reference, reusing parseBucketObjectRef.
func objectKeyForRow(sc config.StorageConfig, filePath string) (string, error) {
	switch storageTypeOf(sc) {
	case "s3":
		_, object, err := parseBucketObjectRef(filePath, "s3", sc.S3Bucket)
		return object, err
	case "gcs":
		_, object, err := parseBucketObjectRef(filePath, "gs", sc.GCSBucket)
		return object, err
	case "azure":
		_, object, err := parseBucketObjectRef(filePath, "azure", sc.AzureContainer)
		return object, err
	case "google-drive":
		return filePath, nil
	default:
		return filePath, nil
	}
}

func successRecords(rows []ports.Execution) []retention.BackupRecord {
	recs := make([]retention.BackupRecord, 0, len(rows))
	for i := range rows {
		r := rows[i]
		if r.Status != ports.StatusSuccess && r.Status != ports.StatusCompleted {
			continue
		}
		recs = append(recs, retention.BackupRecord{
			FilePath:   baseName(r.FilePath),
			Timestamp:  r.Timestamp,
			FileSize:   r.FileSizeBytes,
			Status:     "success",
			BackupType: r.BackupType,
			ChainID:    r.ChainID,
			ChainIndex: r.ChainIndex,
		})
	}
	// Newest-first (ProtectActiveBaseline treats records[0] as the latest).
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Timestamp.After(recs[j].Timestamp) })
	return recs
}

func rowsForJob(rows []ports.Execution, job string) []ports.Execution {
	out := make([]ports.Execution, 0)
	for i := range rows {
		if rows[i].BackupName == job {
			out = append(out, rows[i])
		}
	}
	return out
}

func jobLabelIndex(repos []*repairRepo) map[string]string {
	idx := map[string]string{}
	for _, repo := range repos {
		for _, job := range repo.jobs {
			idx[job] = repo.label
		}
	}
	return idx
}

func presentBaseSet(objs []ports.StorageObject) map[string]struct{} {
	set := map[string]struct{}{}
	for _, o := range objs {
		if isManifestKey(o.Path) {
			continue
		}
		set[baseName(o.Path)] = struct{}{}
	}
	return set
}

func manifestSidecarPresent(objs []ports.StorageObject, artifactBase string) (string, bool) {
	want := artifactBase + ".manifest.json"
	for _, o := range objs {
		if baseName(o.Path) == want {
			return o.Path, true
		}
	}
	return "", false
}

func baseName(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Base(p)
}

func isManifestKey(p string) bool { return strings.HasSuffix(p, ".manifest.json") }

func manifestRefBase(p string) string {
	return strings.TrimSuffix(baseName(p), ".manifest.json")
}

func firstJob(rows []ports.Execution) string {
	if len(rows) == 0 {
		return ""
	}
	return rows[0].BackupName
}

func staleThreshold(cfg *config.Configuration) time.Duration {
	minutes := cfg.Scheduler.StaleLockThreshold
	if minutes <= 0 {
		minutes = 60
	}
	return time.Duration(minutes) * time.Minute
}

// schemaRefusalError maps a non-current doctor status to the sentinel whose
// Code() mapping produces the doctor-contract exit code.
func schemaRefusalError(status string) error {
	switch status {
	case monitor.StatusStalePending:
		return ErrDoctorStalePending
	case monitor.StatusForwardIncompatible:
		return ErrDoctorForwardIncompat
	case monitor.StatusMissing:
		return ErrDoctorMissing
	case monitor.StatusCorrupt:
		return ErrDoctorCorrupt
	default:
		return fmt.Errorf("monitor schema is %q: %w", status, ErrRepairInternal)
	}
}

// repairExitError returns a non-zero sentinel when manual-action inconsistencies
// (artifact_missing, chain_broken) remain — so repair can gate CI/cron.
func repairExitError(findings []repairFinding) error {
	for _, f := range findings {
		if f.Class == driftArtifactMissing || f.Class == driftChainBroken {
			return fmt.Errorf("repair: unresolved inconsistencies remain: %w", ErrRepairInconsistent)
		}
	}
	return nil
}

// ---------- rendering ----------

func renderRepairText(w io.Writer, findings []repairFinding, mode repairMode) {
	// Group by repository label, preserving first-seen order.
	order := []string{}
	groups := map[string][]repairFinding{}
	global := []repairFinding{}
	for _, f := range findings {
		key := f.Repository
		if key == "" {
			global = append(global, f)
			continue
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], f)
	}

	if len(findings) == 0 {
		fmt.Fprintln(w, "No drift detected.")
	}

	for _, label := range order {
		fmt.Fprintf(w, "\nRepository: %s\n", label)
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for _, f := range groups[label] {
			detail := f.Detail
			if f.Path != "" {
				if detail != "" {
					detail = f.Path + "  " + detail
				} else {
					detail = f.Path
				}
			}
			fmt.Fprintf(tw, "  %s\t%s\t→ %s\n", f.Class, detail, f.Action)
		}
		_ = tw.Flush()
	}

	if len(global) > 0 {
		fmt.Fprintln(w, "\nGeneral:")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for _, f := range global {
			fmt.Fprintf(tw, "  %s\t%s\t→ %s\n", f.Class, f.Detail, f.Action)
		}
		_ = tw.Flush()
	}

	s := summarizeRepair(findings)
	fmt.Fprintf(w, "\n%d finding(s): %d orphan_artifact · %d untracked · %d orphan_manifest · %d artifact_missing · %d stale_running · %d stale_lock · %d chain_broken\n",
		s.total, s.orphanArtifact, s.untracked, s.orphanManifest, s.artifactMissing, s.staleRunning, s.staleLock, s.chainBroken)

	if mode.reportOnly {
		fmt.Fprintln(w, "Nothing was modified (report-only; use --fix / --purge-orphans to apply).")
	} else if s.applied == 0 {
		fmt.Fprintln(w, "Nothing was modified.")
	} else {
		fmt.Fprintf(w, "%d change(s) applied.\n", s.applied)
	}
}

func renderRepairJSON(w io.Writer, findings []repairFinding, mode repairMode) {
	s := summarizeRepair(findings)
	if findings == nil {
		findings = []repairFinding{}
	}
	payload := map[string]interface{}{
		"mode": map[string]bool{
			"report_only": mode.reportOnly,
			"fix":         mode.fix,
			"purge":       mode.purge,
		},
		"findings": findings,
		"summary": map[string]int{
			"total":            s.total,
			"orphan_artifact":  s.orphanArtifact,
			"untracked":        s.untracked,
			"orphan_manifest":  s.orphanManifest,
			"artifact_missing": s.artifactMissing,
			"stale_running":    s.staleRunning,
			"stale_lock":       s.staleLock,
			"chain_broken":     s.chainBroken,
			"applied":          s.applied,
		},
		"modified": s.applied > 0,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

type repairSummary struct {
	total           int
	orphanArtifact  int
	untracked       int
	orphanManifest  int
	artifactMissing int
	staleRunning    int
	staleLock       int
	chainBroken     int
	applied         int
}

func summarizeRepair(findings []repairFinding) repairSummary {
	s := repairSummary{total: len(findings)}
	for _, f := range findings {
		if f.Applied {
			s.applied++
		}
		switch f.Class {
		case driftOrphanArtifact:
			s.orphanArtifact++
		case driftUntrackedArtifact:
			s.untracked++
		case driftOrphanManifest:
			s.orphanManifest++
		case driftArtifactMissing:
			s.artifactMissing++
		case driftStaleRunning:
			s.staleRunning++
		case driftStaleLock:
			s.staleLock++
		case driftChainBroken:
			s.chainBroken++
		}
	}
	return s
}

func init() {
	repairCmd.Flags().StringP("config", "c", "", "Path to YAML configuration file")
	repairCmd.Flags().Bool("dry-run", false, "Report-only: detect all drift, mutate nothing (default posture)")
	repairCmd.Flags().Bool("fix", false, "Apply recoverable fixes: finalize stale running rows, remove stale locks, mark broken chains")
	repairCmd.Flags().Bool("purge-orphans", false, "Also delete orphan artifacts (implies --fix); requires confirmation unless --yes")
	repairCmd.Flags().Bool("yes", false, "Skip the interactive confirmation for --purge-orphans")
	repairCmd.Flags().String("job", "", "Restrict reconciliation to a single named backup job")
	repairCmd.Flags().String("format", "text", "Output format: text or json")
}
