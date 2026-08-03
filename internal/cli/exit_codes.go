package cli

import "errors"

// Sentinel errors translated by Code into specific process exit codes.
// See specs/016-remove-ping-fatal/data-model.md for the canonical mapping.
var (
	ErrVerifyNotFound = errors.New("backup not found")
	ErrVerifySkipped  = errors.New("verification skipped: no manifest")
	ErrVerifyInternal = errors.New("verify internal error")

	// ErrVerifyIntegrityFailed is returned by the `backup verify --all` sweep
	// when at least one checked backup is a real integrity problem
	// (corrupted / missing_artifact, or missing_manifest unless
	// --ignore-missing-manifest). It maps to a DISTINCT exit code so an
	// alerting pipeline can tell "backups are broken" (integrity) apart from
	// "the check itself could not run" (operational → ErrVerifyInternal).
	// Spec 051 / PRD 34.
	ErrVerifyIntegrityFailed = errors.New("integrity check failed")

	// ErrDiffSecurityRegression is returned by `backup diff <id1> <id2>` when it
	// detects a security regression between the two backups (encryption turned
	// off, a hash-algorithm change, or an encryption-parameter downgrade). It
	// maps to a non-zero exit (1) so CI/alerting can gate on it. Size/duration
	// magnitude swings never set this — they stay informational (exit 0).
	// PRD 36.
	ErrDiffSecurityRegression = errors.New("backup diff: security regression detected")

	// `sentinel monitor doctor` exit-code carriers. Stable across releases
	// per specs/017-monitor-schema-migration/contracts/monitor-doctor-cli.md.
	ErrDoctorStalePending    = errors.New("monitor schema is stale; pending migrations exist")
	ErrDoctorForwardIncompat = errors.New("monitor schema is ahead of this binary")
	ErrDoctorMissing         = errors.New("monitor database is missing")
	ErrDoctorCorrupt         = errors.New("monitor database is corrupt or unreadable")

	// `sentinel repair` exit-code carriers (PRD 37). ErrRepairInternal marks an
	// operational failure that prevented reconciliation (config/backend/db);
	// ErrRepairInconsistent marks that manual-action inconsistencies remain
	// (artifact_missing / chain_broken) so repair can gate CI/cron. Schema
	// refusal reuses the doctor sentinels above.
	ErrRepairInternal     = errors.New("repair internal error")
	ErrRepairInconsistent = errors.New("repair: unresolved inconsistencies require manual action")
)

// Code translates an error returned from RootCmd.Execute() into a process exit code.
// Returns 0 for nil, the mapped code for known sentinel errors (matched via errors.Is),
// and 1 for any other non-nil error.
func Code(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrVerifyNotFound):
		return 2
	case errors.Is(err, ErrVerifySkipped):
		return 3
	case errors.Is(err, ErrVerifyInternal):
		return 4
	case errors.Is(err, ErrVerifyIntegrityFailed):
		return 5
	case errors.Is(err, ErrDiffSecurityRegression):
		// Non-zero, deliberately kept at 1 (matches the PRD 36 example) so a CI
		// gate simply checks for a non-zero exit.
		return 1
	case errors.Is(err, ErrDoctorStalePending):
		return 1
	case errors.Is(err, ErrDoctorForwardIncompat):
		return 2
	case errors.Is(err, ErrDoctorMissing):
		return 3
	case errors.Is(err, ErrDoctorCorrupt):
		return 4
	case errors.Is(err, ErrRepairInternal):
		return 4
	case errors.Is(err, ErrRepairInconsistent):
		return 5
	default:
		return 1
	}
}
