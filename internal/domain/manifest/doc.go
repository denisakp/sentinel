// Package manifest holds the pure-domain manifest v1 format and the
// HashingWriter used to compute SHA-256 digests over backup artifacts.
//
// Spec 037 — domain extraction. Allowed imports: stdlib, internal/ports,
// internal/sanitize, sibling internal/domain/*. No I/O.
package manifest
