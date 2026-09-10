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

// TestDirectoryFormatIsNotStreamed guards the exclusion that keeps the streaming
// fix for #164 from reproducing #191 in PostgreSQL.
//
// With --format=d, pg_dump writes a directory through --file= and produces
// nothing on stdout. Streaming stdout to the artifact path would create an empty
// file where the directory belongs, and record the digest of no bytes: a manifest
// hash that is wrong in a way that looks valid, which is the Mongo defect this
// campaign fixed in the same change.
//
// Every other format writes to stdout and is streamed.
func TestDirectoryFormatIsNotStreamed(t *testing.T) {
	for _, format := range []string{"d"} {
		args := &PgDumpArgs{
			Host: "h", Port: "5432", Username: "u", Database: "db",
			PgOutFormat: format,
			Storage:     &storage.Params{StorageType: "local", LocalPath: t.TempDir(), OutName: "dump"},
		}
		built, err := argsBuilder(args, t.TempDir())
		if err != nil {
			t.Fatalf("argsBuilder(%s) error = %v", format, err)
		}
		joined := strings.Join(built, " ")
		if !strings.Contains(joined, "--file=") {
			t.Errorf("format %q does not use --file=, so the assumption behind excluding it from "+
				"streaming no longer holds: %s", format, joined)
		}
	}

	for _, format := range []string{"c", "p", "t"} {
		args := &PgDumpArgs{
			Host: "h", Port: "5432", Username: "u", Database: "db",
			PgOutFormat: format,
			Storage:     &storage.Params{StorageType: "local", LocalPath: t.TempDir(), OutName: "dump"},
		}
		built, err := argsBuilder(args, t.TempDir())
		if err != nil {
			t.Fatalf("argsBuilder(%s) error = %v", format, err)
		}
		if strings.Contains(strings.Join(built, " "), "--file=") {
			t.Errorf("format %q uses --file=, so it does not write to stdout and must not be "+
				"streamed either", format)
		}
	}
}
