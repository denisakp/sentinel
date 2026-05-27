// Package lock provides the file-based advisory-lock adapter that
// implements ports.LockManager.
//
// Relocated from internal/lock/ to internal/adapters/lock/ per ADR 0001
// (hexagonal layering). On-disk lock-file format is the v1 layout pinned
// by ADR 0007. Spec 031.
package lock
