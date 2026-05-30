// TODO(spec-038): remove
//
// Spec 037 Sub-PR B introduced this bridge file. Pure types relocated to
// internal/domain/retention/types.go; legacy callers (cli/backup,
// cli/retention_helpers, scheduler/restore_integration, tests/*) still import
// internal/retention. Once those callers are fully rewired to
// internal/domain/retention (a future sub-PR / spec 038), delete this file
// along with the rest of internal/retention/ per spec FR-008.
package retention

import "github.com/denisakp/sentinel/internal/domain/retention"

// Re-exports of the pure domain types. NO function bodies, NO logic in this
// file — see specs/037-domain-extraction/research.md R4 (bridge policy).
type (
	Policy          = retention.Policy
	BackupRecord    = retention.BackupRecord
	BackupCandidate = retention.BackupCandidate
	DeletedBackup   = retention.DeletedBackup
	ApplySummary    = retention.ApplySummary
)
