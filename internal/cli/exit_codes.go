package cli

import "errors"

// Sentinel errors translated by Code into specific process exit codes.
// See specs/016-remove-ping-fatal/data-model.md for the canonical mapping.
var (
	ErrVerifyNotFound = errors.New("backup not found")
	ErrVerifySkipped  = errors.New("verification skipped: no manifest")
	ErrVerifyInternal = errors.New("verify internal error")

	// `sentinel monitor doctor` exit-code carriers. Stable across releases
	// per specs/017-monitor-schema-migration/contracts/monitor-doctor-cli.md.
	ErrDoctorStalePending    = errors.New("monitor schema is stale; pending migrations exist")
	ErrDoctorForwardIncompat = errors.New("monitor schema is ahead of this binary")
	ErrDoctorMissing         = errors.New("monitor database is missing")
	ErrDoctorCorrupt         = errors.New("monitor database is corrupt or unreadable")
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
	case errors.Is(err, ErrDoctorStalePending):
		return 1
	case errors.Is(err, ErrDoctorForwardIncompat):
		return 2
	case errors.Is(err, ErrDoctorMissing):
		return 3
	case errors.Is(err, ErrDoctorCorrupt):
		return 4
	default:
		return 1
	}
}
