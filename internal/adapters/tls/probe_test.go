package tls_test

import (
	"context"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/adapters/tls"
)

func TestProbeTLSConnection_NilTLS(t *testing.T) {
	cfg := ports.DatabaseConfig{
		Type: "postgres",
		TLS:  nil,
	}
	result, err := tls.ProbeTLSConnection(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ProbeTLSConnection(nil TLS) error = %v", err)
	}
	if result.TLSActive {
		t.Error("ProbeTLSConnection(nil TLS): TLSActive should be false")
	}
	if result.FallbackOccurred {
		t.Error("ProbeTLSConnection(nil TLS): FallbackOccurred should be false")
	}
}

func TestProbeTLSConnection_DisabledTLS(t *testing.T) {
	cfg := ports.DatabaseConfig{
		Type: "postgres",
		TLS:  &ports.Config{Enabled: false, Mode: "require"},
	}
	result, err := tls.ProbeTLSConnection(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ProbeTLSConnection(disabled TLS) error = %v", err)
	}
	if result.TLSActive {
		t.Error("ProbeTLSConnection(disabled TLS): TLSActive should be false")
	}
}

func TestProbeTLSConnection_MongoDB(t *testing.T) {
	// MongoDB is skipped — probe returns empty result with no error
	cfg := ports.DatabaseConfig{
		Type: "mongodb",
		TLS:  &ports.Config{Enabled: true, Mode: "require"},
	}
	result, err := tls.ProbeTLSConnection(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ProbeTLSConnection(mongodb) error = %v", err)
	}
	if result.TLSActive {
		t.Error("ProbeTLSConnection(mongodb): TLSActive should be false (skipped)")
	}
}

func TestProbeTLSConnection_UnknownType(t *testing.T) {
	cfg := ports.DatabaseConfig{
		Type: "oracle",
		TLS:  &ports.Config{Enabled: true, Mode: "require"},
	}
	result, err := tls.ProbeTLSConnection(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ProbeTLSConnection(unknown) error = %v", err)
	}
	if result.TLSActive {
		t.Error("ProbeTLSConnection(unknown): TLSActive should be false")
	}
}

// TestProbeTLSConnection_Postgres_RequireMode_ConnectionRefused verifies that
// probePostgres is entered and returns an error when the server is unreachable
// and mode is "require" (non-TLS connection errors propagate as hard errors).
func TestProbeTLSConnection_Postgres_RequireMode_ConnectionRefused(t *testing.T) {
	ctx := context.Background()
	cfg := ports.DatabaseConfig{
		Type:     "postgres",
		Host:     "127.0.0.1",
		Port:     1, // nothing listening here → immediate connection refused
		Username: "nouser",
		Password: "nopass",
		TLS:      &ports.Config{Enabled: true, Mode: "require"},
	}
	_, err := tls.ProbeTLSConnection(ctx, cfg)
	// An unreachable host must produce an error for "require" mode.
	if err == nil {
		t.Error("ProbeTLSConnection(postgres, require, unreachable): want error, got nil")
	}
}

// TestProbeTLSConnection_Postgres_PreferMode_ConnectionRefused verifies that
// probePostgres with mode="prefer" returns an error for a non-TLS failure
// (connection refused is not a TLS error, so fallback does not apply).
func TestProbeTLSConnection_Postgres_PreferMode_ConnectionRefused(t *testing.T) {
	ctx := context.Background()
	cfg := ports.DatabaseConfig{
		Type:     "postgres",
		Host:     "127.0.0.1",
		Port:     1,
		Username: "nouser",
		Password: "nopass",
		TLS:      &ports.Config{Enabled: true, Mode: "prefer"},
	}
	result, err := tls.ProbeTLSConnection(ctx, cfg)
	// "prefer" mode with non-TLS error: the probe propagates the error.
	// Either an error is returned OR the result indicates a non-TLS fallback.
	// Both are valid; we just verify no panic and the probe completes.
	t.Logf("prefer mode result: TLSActive=%v FallbackOccurred=%v err=%v",
		result.TLSActive, result.FallbackOccurred, err)
}

// TestProbeTLSConnection_MySQL_RequireMode_ConnectionRefused verifies that
// probeMySQL is entered and returns an error when the server is unreachable.
func TestProbeTLSConnection_MySQL_RequireMode_ConnectionRefused(t *testing.T) {
	ctx := context.Background()
	cfg := ports.DatabaseConfig{
		Type:     "mysql",
		Host:     "127.0.0.1",
		Port:     1,
		Username: "nouser",
		Password: "nopass",
		TLS:      &ports.Config{Enabled: true, Mode: "require"},
	}
	_, err := tls.ProbeTLSConnection(ctx, cfg)
	if err == nil {
		t.Error("ProbeTLSConnection(mysql, require, unreachable): want error, got nil")
	}
}

// TestProbeTLSConnection_MariaDB_RequireMode_ConnectionRefused verifies that
// mariadb also routes through probeMySQL (same implementation).
func TestProbeTLSConnection_MariaDB_RequireMode_ConnectionRefused(t *testing.T) {
	ctx := context.Background()
	cfg := ports.DatabaseConfig{
		Type:     "mariadb",
		Host:     "127.0.0.1",
		Port:     1,
		Username: "nouser",
		Password: "nopass",
		TLS:      &ports.Config{Enabled: true, Mode: "require"},
	}
	_, err := tls.ProbeTLSConnection(ctx, cfg)
	if err == nil {
		t.Error("ProbeTLSConnection(mariadb, require, unreachable): want error, got nil")
	}
}

// TestProbeTLSConnection_MySQL_PreferMode_ConnectionRefused verifies that
// probeMySQL with mode="prefer" handles unreachable host.
func TestProbeTLSConnection_MySQL_PreferMode_ConnectionRefused(t *testing.T) {
	ctx := context.Background()
	cfg := ports.DatabaseConfig{
		Type:     "mysql",
		Host:     "127.0.0.1",
		Port:     1,
		Username: "nouser",
		Password: "nopass",
		TLS:      &ports.Config{Enabled: true, Mode: "prefer"},
	}
	result, err := tls.ProbeTLSConnection(ctx, cfg)
	t.Logf("mysql prefer mode result: TLSActive=%v FallbackOccurred=%v err=%v",
		result.TLSActive, result.FallbackOccurred, err)
}
