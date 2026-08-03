// Package ports is the hexagonal interface seam for the Sentinel codebase.
//
// It hosts the contract each architectural concern exposes to the rest of the
// application — storage, dump, hasher, encryption, manifest, lock, recorder,
// notifier, crypto (key management), and tls. Adapters under
// internal/adapters/... (introduced by specs 029 — 035) implement these
// interfaces; the domain layer under internal/domain/... (specs 036, 027)
// depends only on ports.
//
// Dependency rule: this package MUST NOT import any implementation package
// within this repository. Concrete types live in adapter packages and depend
// on ports — never the other way around. The rule is enforced statically by
// a dependency-rule lint.
//
// Co-located types: option structs, sentinel errors, and any other type
// referenced by a port method signature live here as the single source of
// truth. Implementation packages import these types
// from ports rather than redeclaring them locally.
//
// Test-only file conformance_test.go declares one compile-time conformance
// assertion per port (var _ <Port> = (*<concrete>)(nil)) so that any drift
// between a port and its current concrete implementation fails the next
// `go build`.
//
// See specs/028-ports-skeleton/ for the full design rationale and ADR 0001
// for the broader hexagonal migration this package enables.
package ports
