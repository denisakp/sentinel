package pg

import (
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
)

// TestTLSReachesThePgDumpCommandLine is the engine-side half of the guard for
// #189.
//
// Carrying a TLS config on the args struct is not enough on its own: if no
// builder read it, the connection would still be made in plaintext while the
// struct looked correctly populated. This asserts the configuration actually
// reaches the invocation.
//
// The nil case matters as much as the populated one. Before the fix, TLS was
// always nil here, so BuildTLSArgs contributed nothing and pg_dump connected
// without transport security whatever the YAML said.
func TestTLSReachesThePgDumpCommandLine(t *testing.T) {
	withTLS := &PgDumpArgs{
		Host:        "db.internal",
		Port:        "5432",
		Username:    "app",
		Database:    "appdb",
		Storage:     &storage.Params{OutName: "dump.sql"},
		PgOutFormat: "p",
		TLS: &ports.Config{
			Enabled:    true,
			Mode:       "verify-full",
			CACertPath: "/etc/ssl/ca.pem",
		},
	}
	withoutTLS := &PgDumpArgs{
		Host:        "db.internal",
		Port:        "5432",
		Username:    "app",
		Database:    "appdb",
		Storage:     &storage.Params{OutName: "dump.sql"},
		PgOutFormat: "p",
	}

	withArgs, err := argsBuilder(withTLS, t.TempDir())
	if err != nil {
		t.Fatalf("argsBuilder(with TLS) error = %v", err)
	}
	withoutArgs, err := argsBuilder(withoutTLS, t.TempDir())
	if err != nil {
		t.Fatalf("argsBuilder(without TLS) error = %v", err)
	}

	with := strings.Join(withArgs, " ")
	without := strings.Join(withoutArgs, " ")

	if with == without {
		t.Fatalf("a configured tls: block produced the same pg_dump arguments as no block at all, "+
			"so it has no effect on the connection.\nargs: %s", with)
	}
	if !strings.Contains(with, "verify-full") {
		t.Errorf("the configured TLS mode does not appear in the arguments.\nargs: %s", with)
	}
	if !strings.Contains(with, "/etc/ssl/ca.pem") {
		t.Errorf("the configured CA path does not appear in the arguments.\nargs: %s", with)
	}
}
