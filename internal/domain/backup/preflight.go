package backup

import (
	"context"
	"fmt"

	incremental "github.com/denisakp/sentinel/internal/domain/backup/incremental"
	"github.com/denisakp/sentinel/internal/ports"
)

// CheckIncrementalPrerequisites probes the LIVE server through ports.DBProber
// and evaluates the incremental prerequisites against what it reports
// (issue #185). These facts (log_bin, summarize_wal, replica-set membership,
// oplog window) cannot be known at config-load time, so this runs in the
// execution preflight, before any dump work.
//
// It returns a hard error for an unmet prerequisite (typed codes from the
// incremental package, e.g. "log_bin_off") and a non-fatal warning when the
// Mongo oplog window is below the configured threshold. A probe failure is an
// error: an unverifiable server is not assumed fine.
func CheckIncrementalPrerequisites(ctx context.Context, prober ports.DBProber, job Job) (warnings []string, err error) {
	if !job.IncrementalEnabled {
		return nil, nil
	}
	if prober == nil {
		return nil, fmt.Errorf("incremental prerequisite check: database prober is not configured")
	}
	caps, perr := prober.ProbeIncrementalCapabilities(ctx, job.DBConn)
	if perr != nil {
		return nil, fmt.Errorf("incremental prerequisite check: probe %s server: %w", job.Engine, perr)
	}
	if caps == nil {
		return nil, fmt.Errorf("incremental prerequisite check: probe returned no capabilities")
	}

	switch job.Engine {
	case "postgres":
		err = incremental.Ensure(incremental.ValidatePostgresIncrementalPrerequisites(caps.VersionMajor, caps.WALSummaryEnabled))
	case "mysql", "mariadb":
		// binlog_path is validated statically at config load; only the
		// server-side log_bin state is checked here.
		if !caps.LogBinEnabled {
			err = incremental.Ensure(incremental.ValidateMySQLIncrementalPrerequisites(false, ""))
		}
	case "mongodb":
		err = incremental.Ensure(incremental.ValidateMongoIncrementalPrerequisites(caps.ReplicaSet))
		if err == nil {
			res := incremental.ValidateMongoOplogWindowPrerequisite(caps.OplogWindowHours, job.OplogWindowWarnHours)
			if !res.Passed {
				warnings = append(warnings, fmt.Sprintf("warning: %s: %s", res.ErrorCode, res.Detail))
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("incremental prerequisite not met for %s backup: %w", job.Engine, err)
	}
	return warnings, nil
}
