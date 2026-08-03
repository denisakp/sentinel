// Package dbprobertesting provides an in-memory test fake for ports.DBProber.
//
// Domain test code (internal/domain/*) imports MockProber to exercise restore
// + backup orchestration without touching a real database. The compile-time
// port assertion lives in the dbprobertesting_test package per the import-cycle
// pattern documented in CLAUDE.md.
//
// Spec 037 — domain extraction.
package dbprobertesting
