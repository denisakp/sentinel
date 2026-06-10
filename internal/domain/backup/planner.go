package backup

// Full-vs-incremental planning. Relocated from internal/cli/backup.go
// (deriveIncrementalBackupContext + latestIncrementalExecution +
// resolveBaselineBackupID + canonicalScheduledExtension) by spec 038
// Sub-PR K; history reads go through ports.Recorder instead of opening the
// monitor adapter directly.

import (
	"context"
	"strings"

	incremental "github.com/denisakp/sentinel/internal/domain/backup/incremental"
	"github.com/denisakp/sentinel/internal/ports"
)

// DeriveIncrementalContext decides full vs incremental for the job and
// assembles the lineage metadata recorded in manifests + history rows.
// rec may be nil (no history persistence) — the decision degrades to a
// fresh chain, matching the pre-carve behavior with an empty history DB.
func DeriveIncrementalContext(ctx context.Context, rec ports.Recorder, job Job, fileSize int64) IncrementalContext {
	if !job.IncrementalEnabled {
		return IncrementalContext{BackupType: "full"}
	}

	previous := incremental.ChainState{MaxDepth: job.MaxChainDepth}
	latest := latestChainExecution(ctx, rec, job.Name)
	if latest != nil {
		previous.ChainID = latest.ChainID
		previous.CurrentIndex = latest.ChainIndex
		previous.BaselineBackupID = resolveBaselineBackupID(ctx, rec, job.Name, latest)
	}

	decision, err := incremental.Decide(previous, job.ForceFull)
	if err != nil {
		return IncrementalContext{Enabled: true, BackupType: "full", MaxChainDepth: job.MaxChainDepth}
	}

	out := IncrementalContext{
		Enabled:       true,
		BackupType:    decision.Type,
		ChainID:       decision.ChainID,
		ChainIndex:    decision.ChainIndex,
		MaxChainDepth: job.MaxChainDepth,
	}

	if decision.Type == "incremental" {
		out.BaselineBackupID = decision.BaselineBackupID
		out.RequiredBackupIDs = []string{decision.BaselineBackupID}
		out.DeltaSizeBytes = fileSize
	} else {
		out.FullBackupSizeBytes = fileSize
	}

	if out.FullBackupSizeBytes > 0 && out.DeltaSizeBytes > 0 {
		out.CompressionRatio = float64(out.DeltaSizeBytes) / float64(out.FullBackupSizeBytes)
	}

	return out
}

// latestChainExecution returns the most recent successful chain-bearing
// execution for jobName, or nil when none (or no recorder).
func latestChainExecution(ctx context.Context, rec ports.Recorder, jobName string) *ports.Execution {
	if rec == nil || strings.TrimSpace(jobName) == "" {
		return nil
	}

	executions, err := rec.ListExecutions(ctx, &ports.Filter{BackupName: jobName}, 100, 0)
	if err != nil {
		return nil
	}

	for i := range executions {
		exec := executions[i]
		if exec.Status != "success" && exec.Status != ports.StatusCompleted {
			continue
		}
		if strings.TrimSpace(exec.ChainID) == "" {
			continue
		}
		if exec.BackupType != "full" && exec.BackupType != "incremental" {
			continue
		}
		return &exec
	}

	return nil
}

// resolveBaselineBackupID locates the chain baseline (full / index-0)
// artifact reference for the chain `latest` belongs to.
func resolveBaselineBackupID(ctx context.Context, rec ports.Recorder, jobName string, latest *ports.Execution) string {
	if latest == nil {
		return ""
	}

	if latest.BackupType == "full" {
		if latest.FilePath != "" {
			return latest.FilePath
		}
		return latest.ID
	}

	if rec == nil {
		return latest.ID
	}

	executions, err := rec.ListExecutions(ctx, &ports.Filter{BackupName: jobName}, 300, 0)
	if err != nil {
		return latest.ID
	}

	for i := range executions {
		exec := executions[i]
		if exec.Status != "success" && exec.Status != ports.StatusCompleted {
			continue
		}
		if exec.ChainID != latest.ChainID {
			continue
		}
		if exec.BackupType == "full" || exec.ChainIndex == 0 {
			if exec.FilePath != "" {
				return exec.FilePath
			}
			return exec.ID
		}
	}

	return latest.ID
}

// CanonicalScheduledExtension returns the canonical artifact extension for
// scheduled output naming. pgOutFormat applies to postgres only ("" → plain).
func CanonicalScheduledExtension(engine, pgOutFormat string) string {
	switch engine {
	case "postgres":
		format := "p"
		if pgOutFormat != "" {
			format = pgOutFormat
		}
		switch format {
		case "c":
			return ".backup"
		case "t":
			return ".tar"
		case "d":
			return ""
		default:
			return ".sql"
		}
	case "mysql", "mariadb":
		return ".sql"
	default:
		return ""
	}
}
