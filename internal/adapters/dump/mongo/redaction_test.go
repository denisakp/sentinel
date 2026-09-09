package mongo

import (
	"context"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
)

// okProber satisfies ports.DBProber and always reports a reachable database, so
// Backup proceeds past its connectivity check and reaches the mongodump
// invocation, which is where the leak was.
type okProber struct{}

func (okProber) Ping(context.Context, ports.DatabaseConfig) error { return nil }
func (okProber) ListDatabases(context.Context, ports.DatabaseConfig) ([]string, error) {
	return nil, nil
}
func (okProber) AssessPostgresCascadeSafety(context.Context, ports.DatabaseConfig, ports.CascadeTarget) (*ports.CascadeSafetyResult, error) {
	return nil, nil
}

// TestBackupErrorDoesNotLeakPassword is the regression guard for the dump half
// of #156.
//
// mongo_dump.go built its failure message with strings.Join(args, " "), and args
// begins with --uri=mongodb://user:PASSWORD@host. The password therefore landed
// in the returned error and in any log that captured it. This is what
// internal/sanitize exists to prevent, and the neighbouring stderr branch
// already called RedactStderr; this branch was simply missed.
//
// The test drives the real Backup path rather than the redaction helper in
// isolation, so it fails if someone reverts the call site. Asserting on
// sanitize.RedactArgs directly would keep passing against the bug.
func TestBackupErrorDoesNotLeakPassword(t *testing.T) {
	const secret = "sup3rs3cr3t-pw"
	da := &DumpMongoArgs{
		// .invalid is reserved by RFC 2606 and never resolves, so mongodump fails
		// fast on DNS rather than retrying a connection. If
		// mongodump is absent the exec error is taken instead; either way the
		// error path that formats the arguments is the one under test.
		Uri:      "mongodb://admin:" + secret + "@sentinel-test.invalid:27017/appdb",
		Database: "appdb",
		Storage: &storage.Params{
			StorageType: "local",
			LocalPath:   t.TempDir(),
			OutName:     "dump.archive",
		},
	}

	// Emptying PATH makes the mongodump lookup fail, so cmd.Run returns an exec
	// error having written nothing to either stream. That is what selects the
	// branch under test: the two branches above it print captured output and
	// were already redacted, so only this one ever formatted the raw arguments.
	// It is also the fast route. Reaching it through a real connection failure
	// costs mongodump's 30-second server-selection timeout on every run.
	t.Setenv("PATH", t.TempDir())

	_, err := Backup(okProber{}, da)
	if err == nil {
		t.Fatal("Backup() unexpectedly succeeded against a closed port")
	}

	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Errorf("password leaked into the error returned by Backup().\ngot: %s", msg)
	}
}
