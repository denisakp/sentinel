package cli

// Retention orchestration, rewired from the deleted
// internal/retention.Manager is replaced by direct domain calls
// (CalculateCandidates + ProtectActiveBaseline), storage DELETE via
// ports.StorageBackend, and history DELETE via ports.Recorder.

import (
	"context"
	"fmt"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/config"
	domainret "github.com/denisakp/sentinel/internal/domain/retention"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

// retentionFetchLimit bounds the history read backing candidate calculation.
// The deleted Manager.fetchRecords read all rows; ListExecutions is paged, so
// a single generous page preserves behavior for any realistic history size.
const retentionFetchLimit = 100_000

// buildRetentionPolicy maps a config RetentionPolicy to the domain Policy,
// carrying the GFS tiers when present. Single config→domain mapping for the
// retention deletion path (used by manual `retention` commands and the
// automatic post-scheduled-backup sweep).
func buildRetentionPolicy(rp config.RetentionPolicy, dryRun bool) domainret.Policy {
	policy := domainret.Policy{
		KeepLast: rp.KeepLast,
		KeepDays: rp.KeepDays,
		DryRun:   dryRun,
	}
	if g := rp.GFS; g != nil {
		policy.GFS = &domainret.GFSPolicy{
			KeepDaily:   g.KeepDaily,
			KeepWeekly:  g.KeepWeekly,
			KeepMonthly: g.KeepMonthly,
			KeepYearly:  g.KeepYearly,
		}
	}
	return policy
}

// retentionEnabled reports whether a job's retention policy would act — flat
// rules, dry-run, or a non-empty GFS block. A GFS-only policy MUST NOT be
// skipped.
func retentionEnabled(rp config.RetentionPolicy) bool {
	if rp.KeepLast > 0 || rp.KeepDays > 0 {
		return true
	}
	if g := rp.GFS; g != nil {
		return g.KeepDaily > 0 || g.KeepWeekly > 0 || g.KeepMonthly > 0 || g.KeepYearly > 0
	}
	return false
}

func runRetention(cmd *cobra.Command, preview bool) error {
	path, _ := cmd.Flags().GetString("config")
	jobName, _ := cmd.Flags().GetString("job")
	dryRun := preview
	if cmd.Flags().Changed("dry-run") {
		value, _ := cmd.Flags().GetBool("dry-run")
		dryRun = value
	}

	cfg, err := LoadAndValidateConfig(path)
	if err != nil {
		return err
	}

	mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = mon.Close()
	}()

	ctx := context.Background()
	if jobName != "" {
		deleted, err := applyJobRetention(ctx, cfg, mon, jobName, dryRun)
		if err != nil {
			return err
		}
		printRetentionSummary(cmd, jobName, deleted, dryRun)
		return nil
	}

	summary := applyAllRetention(ctx, cfg, mon, dryRun)
	if len(summary.Errors) > 0 {
		cmd.PrintErrln("retention completed with errors")
	}
	for job, count := range summary.ByBackupJob {
		cmd.Printf("%s: deleted %d backups\n", job, count)
	}
	cmd.Printf("total deleted: %d backups\n", summary.TotalDeleted)
	return nil
}

// applyJobRetention executes the retention policy for one backup job:
// domain candidate calculation, storage DELETE through ports.StorageBackend,
// history DELETE through ports.Recorder.RetentionDeleteRecords.
func applyJobRetention(ctx context.Context, cfg *config.Configuration, rec ports.Recorder, jobName string, dryRun bool) ([]domainret.DeletedBackup, error) {
	job, ok := cfg.Databases[jobName]
	if !ok {
		return nil, fmt.Errorf("backup '%s' not found", jobName)
	}
	// A dry run is requested either by the caller, which is the --dry-run flag,
	// or by the job's own retention.dry_run in YAML. The two OR together
	// deliberately: dry_run is a safety switch, and a safety switch must never be
	// silently cancelled by the other party. Someone who writes dry_run: true is
	// asking for nothing to be deleted for this job, full stop; turning that off
	// is an edit to the file, not a flag.
	//
	// Before this, the YAML key was parsed, validated and inherited, and then read
	// by no delete path at all. The automatic sweep that runs after a scheduled
	// backup passed false unconditionally, so it deleted for real while the
	// operator believed the safety switch was on: silent data loss with the guard
	// engaged (#157).
	effectiveDryRun := dryRun || job.Retention.DryRun
	policy := buildRetentionPolicy(job.Retention, effectiveDryRun)

	records, err := fetchRetentionRecords(ctx, rec, jobName)
	if err != nil {
		return nil, err
	}
	candidates := domainret.CalculateCandidates(records, policy, time.Now().UTC())
	candidates = domainret.ProtectActiveBaseline(candidates, records)
	if effectiveDryRun {
		return candidatesToDeleted(candidates), nil
	}

	deleted, errs := deleteRetentionCandidates(ctx, candidates, job.Storage)

	// Delete history rows for exactly the artifacts whose storage deletion was
	// confirmed, and do it even when some candidate failed.
	//
	// Two bugs lived in the old ordering (#182). It passed every candidate, not
	// the confirmed ones, so a protected baseline kept its object and lost its
	// history row. And it returned early on the first error, so a partial
	// failure left every successfully deleted artifact still recorded as
	// present. Both directions leave the bucket and the history disagreeing;
	// this way the history says exactly what the bucket says.
	confirmed := confirmedCandidates(candidates, deleted)
	if err := rec.RetentionDeleteRecords(ctx, jobName, confirmed); err != nil {
		if len(errs) > 0 {
			return deleted, fmt.Errorf("retention delete failed: %v (history not updated: %w)", errs[0], err)
		}
		return deleted, err
	}

	if len(errs) > 0 {
		return deleted, fmt.Errorf("retention delete failed for %d of %d candidate(s): %v",
			len(errs), len(candidates), errs[0])
	}
	return deleted, nil
}

// confirmedCandidates narrows candidates to those whose artifact was actually
// removed from storage, matched by file path. Anything skipped, protected or
// failed is left out, so its history row survives alongside its object.
func confirmedCandidates(candidates []domainret.BackupCandidate, deleted []domainret.DeletedBackup) []domainret.BackupCandidate {
	if len(deleted) == 0 {
		return nil
	}
	gone := make(map[string]struct{}, len(deleted))
	for _, d := range deleted {
		gone[d.FilePath] = struct{}{}
	}
	out := make([]domainret.BackupCandidate, 0, len(deleted))
	for _, c := range candidates {
		if _, ok := gone[c.FilePath]; ok {
			out = append(out, c)
		}
	}
	return out
}

// applyAllRetention runs applyJobRetention for every job carrying a policy.
func applyAllRetention(ctx context.Context, cfg *config.Configuration, rec ports.Recorder, dryRun bool) domainret.ApplySummary {
	summary := domainret.ApplySummary{ByBackupJob: make(map[string]int)}

	for name, job := range cfg.Databases {
		if !retentionEnabled(job.Retention) {
			continue
		}
		deleted, err := applyJobRetention(ctx, cfg, rec, name, dryRun)
		if err != nil {
			summary.Errors = append(summary.Errors, err.Error())
			continue
		}
		summary.ByBackupJob[name] = len(deleted)
		summary.TotalDeleted += int64(len(deleted))
		for _, item := range deleted {
			summary.TotalSize += item.FileSize
		}
	}

	return summary
}

// fetchRetentionRecords reads the successful execution history for a job and
// maps it to domain BackupRecords, newest first (ListExecutions orders by
// timestamp DESC — ProtectActiveBaseline relies on records[0] being latest).
func fetchRetentionRecords(ctx context.Context, rec ports.Recorder, jobName string) ([]domainret.BackupRecord, error) {
	execs, err := rec.ListExecutions(ctx, &ports.Filter{BackupName: jobName, Status: "success"}, retentionFetchLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to query backup records: %w", err)
	}

	records := make([]domainret.BackupRecord, 0, len(execs))
	for _, e := range execs {
		records = append(records, domainret.BackupRecord{
			FilePath:   e.FilePath,
			FileSize:   e.FileSizeBytes,
			Timestamp:  e.Timestamp,
			Status:     e.Status,
			BackupType: e.BackupType,
			ChainID:    e.ChainID,
			ChainIndex: e.ChainIndex,
		})
	}
	return records, nil
}

func candidatesToDeleted(candidates []domainret.BackupCandidate) []domainret.DeletedBackup {
	deleted := make([]domainret.DeletedBackup, 0, len(candidates))
	for _, cand := range candidates {
		deleted = append(deleted, domainret.DeletedBackup{
			FilePath:      cand.FilePath,
			FileSize:      cand.FileSize,
			DeletionTime:  time.Now().UTC(),
			ReasonDeleted: cand.ReasonDeleted,
		})
	}
	return deleted
}

func printRetentionSummary(cmd *cobra.Command, jobName string, deleted []domainret.DeletedBackup, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "preview"
	}
	cmd.Printf("retention %s for %s\n", mode, jobName)
	for _, item := range deleted {
		cmd.Printf("- %s (%d bytes) - %s\n", item.FilePath, item.FileSize, item.ReasonDeleted)
	}
}
