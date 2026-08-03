// Package db_probe is the driving adapter for ports.DBProber. It owns real
// I/O against target databases for connectivity (Ping), database enumeration
// (ListDatabases), and Postgres cascade-safety inspection
// (AssessPostgresCascadeSafety). The package consolidates code previously
// living in internal/backup/sql/{ping_database,list_databases,connectivity}.go
// and internal/restore/postgres_conflicts.go.
//
// Spec 037 — domain extraction. Implementation lands in Phase 2 (Sub-PR C).
package db_probe
