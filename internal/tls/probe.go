package tls

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	// Import drivers so the probe can open connections.
	// These are already in go.mod as dependencies of the backup packages.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"github.com/denisakp/sentinel/internal/ports"
)

// ProbeTLSConnection attempts a lightweight database connection using the
// configured TLS settings and reports whether TLS is active.
//
// For mode "prefer": if the TLS-enabled probe fails with a TLS handshake
// error, ports.ProbeResult.FallbackOccurred is set to true and no error is returned
// (the caller should emit a WARN log and proceed without TLS).
//
// For modes require/verify-ca/verify-full: a TLS failure is returned as an
// error, preventing the backup from starting.
//
// MongoDB is skipped (returns TLSActive=false, no error) because mongodump
// manages its own TLS via the URI/flags; Go has no lightweight mongo probe.
func ProbeTLSConnection(ctx context.Context, cfg ports.DatabaseConfig) (ports.ProbeResult, error) {
	if cfg.TLS == nil || !cfg.TLS.Enabled {
		return ports.ProbeResult{}, nil
	}

	mode := cfg.TLS.Mode
	if mode == "" {
		mode = "prefer"
	}

	switch cfg.Type {
	case "postgres":
		return probePostgres(ctx, cfg, mode)
	case "mysql", "mariadb":
		return probeMySQL(ctx, cfg, mode)
	default:
		// mongodb and unknown types: skip probe
		return ports.ProbeResult{}, nil
	}
}

func probePostgres(ctx context.Context, cfg ports.DatabaseConfig, mode string) (ports.ProbeResult, error) {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Port
	if port == 0 {
		port = 5432
	}

	pgMode := pgSSLMode(mode)
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=postgres sslmode=%s connect_timeout=5",
		host, port, cfg.Username, cfg.Password, pgMode,
	)
	if cfg.TLS.CACertPath != "" {
		dsn += fmt.Sprintf(" sslrootcert=%s", cfg.TLS.CACertPath)
	}
	if cfg.TLS.ClientCert != "" {
		dsn += fmt.Sprintf(" sslcert=%s sslkey=%s", cfg.TLS.ClientCert, cfg.TLS.ClientKey)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return ports.ProbeResult{}, fmt.Errorf("failed to open postgres probe connection: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		if mode == "prefer" && isTLSError(err) {
			return ports.ProbeResult{TLSActive: false, FallbackOccurred: true}, nil
		}
		return ports.ProbeResult{}, fmt.Errorf("postgres TLS probe failed: %w", err)
	}

	return ports.ProbeResult{TLSActive: true}, nil
}

func probeMySQL(ctx context.Context, cfg ports.DatabaseConfig, mode string) (ports.ProbeResult, error) {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Port
	if port == 0 {
		port = 3306
	}

	sslMode := mysqlSSLMode(mode)
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/mysql?tls=%s&timeout=5s",
		cfg.Username, cfg.Password, host, port, strings.ToLower(sslMode),
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return ports.ProbeResult{}, fmt.Errorf("failed to open mysql probe connection: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		if mode == "prefer" && isTLSError(err) {
			return ports.ProbeResult{TLSActive: false, FallbackOccurred: true}, nil
		}
		return ports.ProbeResult{}, fmt.Errorf("mysql TLS probe failed: %w", err)
	}

	return ports.ProbeResult{TLSActive: true}, nil
}

// isTLSError returns true when err is a TLS handshake / certificate error,
// indicating the server does not support TLS (relevant for "prefer" mode fallback).
func isTLSError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tls") ||
		strings.Contains(msg, "ssl") ||
		strings.Contains(msg, "handshake") ||
		strings.Contains(msg, "certificate")
}
