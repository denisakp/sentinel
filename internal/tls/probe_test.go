package tls_test

import (
	"context"
	"testing"

	"github.com/denisakp/sentinel/internal/tls"
)

func TestProbeTLSConnection_NilTLS(t *testing.T) {
	cfg := tls.DatabaseConfig{
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
	cfg := tls.DatabaseConfig{
		Type: "postgres",
		TLS:  &tls.Config{Enabled: false, Mode: "require"},
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
	cfg := tls.DatabaseConfig{
		Type: "mongodb",
		TLS:  &tls.Config{Enabled: true, Mode: "require"},
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
	cfg := tls.DatabaseConfig{
		Type: "oracle",
		TLS:  &tls.Config{Enabled: true, Mode: "require"},
	}
	result, err := tls.ProbeTLSConnection(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ProbeTLSConnection(unknown) error = %v", err)
	}
	if result.TLSActive {
		t.Error("ProbeTLSConnection(unknown): TLSActive should be false")
	}
}
