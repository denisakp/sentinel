// TODO(spec-038): remove
//
// Spec 037 Sub-PR B introduced this bridge file. Pure policy evaluation
// relocated to internal/domain/retention/policy.go. Legacy callers reach the
// pure functions through these `var X = domain.X` re-exports (no function
// bodies — see specs/037-domain-extraction/research.md R4).
package retention

import (
	domainret "github.com/denisakp/sentinel/internal/domain/retention"
)

// CalculateCandidates re-exports the pure domain implementation.
var CalculateCandidates = domainret.CalculateCandidates
