// Package config_census is a gate over internal/config, not a part of it.
//
// It answers one question mechanically: can every configuration key an operator
// is allowed to set actually reach code that acts on it? A key that is parsed,
// validated, and then read by nothing is worse than a key that is rejected,
// because the operator is told nothing at all. That shape accounts for a large
// share of the defects recorded in docs/audit/documentation-batch-defects/.
//
// The package lives outside internal/ deliberately. Placed inside the tree it
// polices, it could import freely from what it is meant to check, and the usual
// drift would follow. From here it sees internal/config exactly as any other
// consumer does.
//
// The check runs two passes and compares them:
//
//   - a dynamic pass reflecting from the root config.Configuration type, which
//     yields every addressable path an operator can set;
//   - a static pass parsing the source of internal/config, which yields every
//     type that declares schema-tagged fields.
//
// The second exists because reflection cannot see a type that nothing
// references. A reflection-only census would report a clean result while
// orphaned types sit unreachable, which is precisely the shape of issue #144.
//
// See specs/061-e2e-harness-foundation/contracts/reachability-record.md.
package config_census
