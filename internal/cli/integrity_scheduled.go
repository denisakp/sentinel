package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/notifier"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// Notification modes for the scheduled integrity sweep (integrity.scheduled_check.notify_on).
const (
	integrityNotifyFailure = "failure" // page on any non-ok result (default)
	integrityNotifyAlways  = "always"  // also confirm clean runs
	integrityNotifyNever   = "never"   // silent regardless of outcome
)

// newIntegrityDispatcher constructs the notification dispatcher for the
// scheduled integrity sweep from configured channels. It is a package-level
// seam (mirroring newVerifyBackend) so tests can inject a mock ports.Dispatcher
// without wiring real channels.
var newIntegrityDispatcher = func(notifications []config.NotificationChannel) (ports.Dispatcher, error) {
	return notifier.NewDispatcherFromConfig(notifications)
}

// runScheduledIntegrityCheck is the scheduled-integrity job body. It runs the
// shared repository integrity sweep (runVerifySweep — the
// same core as `backup verify --all`), records the per-artifact results as one
// grouped run in the integrity_checks table (Trigger="scheduled"), and — when
// the sweep found any non-ok result and notify_on permits — dispatches a
// failure notification through the configured channels. Notification delivery
// is best-effort: a delivery failure is logged as a warning and never crashes
// the scheduler nor discards the recorded run.
func runScheduledIntegrityCheck(ctx context.Context, cfg *config.Configuration, mon *monitor.Monitor, ic config.IntegrityScheduledCheck) error {
	startedAt := time.Now().UTC()

	opts := sweepOptions{job: ic.Job}
	if ic.Since != "" {
		window, err := parseSince(ic.Since)
		if err != nil {
			return fmt.Errorf("scheduled integrity check: invalid since %q: %w", ic.Since, err)
		}
		opts.cutoff = time.Now().UTC().Add(-window)
	}

	results, summary, err := runVerifySweep(ctx, cfg, mon, opts)
	if err != nil {
		return fmt.Errorf("scheduled integrity check: list executions: %w", err)
	}

	runID := newRunID()
	run := ports.IntegrityRun{
		RunID:   runID,
		Trigger: ports.IntegrityTriggerScheduled,
		Results: verifyResultsToIntegrityResults(results, startedAt),
	}
	if err := mon.RecordIntegrityCheck(ctx, run); err != nil {
		return fmt.Errorf("scheduled integrity check: record run %s: %w", runID, err)
	}

	slog.Info("scheduled integrity sweep complete",
		"event", "integrity_sweep_complete",
		"run_id", runID,
		"checked", summary.Checked,
		"ok", summary.OK,
		"corrupted", summary.Corrupted,
		"missing_artifact", summary.MissingArtifact,
		"missing_manifest", summary.MissingManifest,
		"errored", summary.Errored,
	)

	// Notification is a post-record best-effort step: never let a delivery
	// failure discard the already-recorded run.
	notifyIntegrityResult(cfg, ic, runID, summary, startedAt)
	return nil
}

// integritySweepHasFailure reports whether a summary carries any non-ok
// integrity verdict (corrupted / missing_artifact / missing_manifest). These
// are the recorded outcomes that constitute repository corruption.
func integritySweepHasFailure(s verifySummary) bool {
	return s.Corrupted > 0 || s.MissingArtifact > 0 || s.MissingManifest > 0
}

// notifyIntegrityResult dispatches (or suppresses) the sweep notification per
// the notify_on mode. failure/"" → dispatch only on a non-ok result;
// always → dispatch on every run (Success when clean, Failure when not);
// never → silent. Best-effort: construction or delivery failures are logged as
// warnings, never returned.
func notifyIntegrityResult(cfg *config.Configuration, ic config.IntegrityScheduledCheck, runID string, summary verifySummary, startedAt time.Time) {
	mode := ic.NotifyOn
	if mode == "" {
		mode = integrityNotifyFailure
	}
	if mode == integrityNotifyNever {
		return
	}

	hasFailure := integritySweepHasFailure(summary)
	if mode == integrityNotifyFailure && !hasFailure {
		return // failure mode stays silent on clean runs
	}

	dispatcher, err := newIntegrityDispatcher(cfg.Defaults.Notifications)
	if err != nil {
		slog.Warn("scheduled integrity notification dispatcher unavailable",
			"event", "integrity_notify_dispatcher_error",
			"run_id", runID,
			"error", err.Error())
		return
	}

	message := fmt.Sprintf(
		"integrity sweep %s: %d checked, %d ok, %d corrupted, %d missing_artifact, %d missing_manifest",
		runID, summary.Checked, summary.OK, summary.Corrupted, summary.MissingArtifact, summary.MissingManifest,
	)

	backup := &ports.BackupContext{
		BackupName: "integrity:" + runID,
		Status:     ports.NotifyStatusSuccess,
		StartTime:  startedAt,
		EndTime:    time.Now().UTC(),
	}
	if hasFailure {
		// Only a failing sweep carries the summary in the Error field (the
		// notifier renders "Error:" whenever it is non-empty, so leaving it
		// empty keeps the "always + clean" run a clean success confirmation).
		backup.Status = ports.NotifyStatusFailure
		backup.Error = message
	}
	if err := dispatcher.Notify(backup); err != nil {
		slog.Warn("scheduled integrity notification delivery failed",
			"event", "integrity_notify_delivery_error",
			"run_id", runID,
			"error", err.Error())
	}
}

// verifyResultsToIntegrityResults maps sweep results to the recordable
// per-artifact integrity rows. Only results carrying one of the four classified
// states (ok / corrupted / missing_artifact / missing_manifest) are recorded;
// operational-error results (verifyResult.Err set, empty Status) are not
// integrity verdicts and fall outside the store's fixed vocabulary, so they are
// skipped from the audit rows (still surfaced in the sweep summary / logs).
func verifyResultsToIntegrityResults(results []verifyResult, checkedAt time.Time) []ports.IntegrityResult {
	out := make([]ports.IntegrityResult, 0, len(results))
	for _, r := range results {
		if r.Status == "" {
			continue // operational error — not a recordable integrity verdict
		}
		out = append(out, ports.IntegrityResult{
			BackupID:       r.BackupID,
			Job:            r.Job,
			Result:         r.Status,
			StoredHash:     r.StoredHash,
			ComputedHash:   r.ComputedHash,
			StorageBackend: r.StorageBackend,
			ArtifactPath:   r.Path,
			CheckedAt:      checkedAt,
		})
	}
	return out
}

// newRunID returns a random UUID v4 string used to group a sweep's per-artifact
// integrity rows. Mirrors the monitor adapter's own newUUID (kept local so the
// cli package does not depend on an adapter-internal helper).
func newRunID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
