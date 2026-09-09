package mongo

import (
	"context"
	"strings"
	"testing"
)

// TestCheckConnectivityDoesNotLeakPassword is the regression guard for #156.
//
// The failure message printed ra.URI verbatim. A MongoDB URI carries userinfo,
// so the password went into the error, and from there into every log that
// captured it. This is exactly what internal/sanitize exists to prevent, and
// the surrounding code already used it; this path was missed.
//
// The test asserts both halves of a good redaction: the secret is gone, and the
// parts an operator needs in order to act on the error are still there. A
// redaction that removes the whole URI would pass a "no password" check while
// making the message useless.
func TestCheckConnectivityDoesNotLeakPassword(t *testing.T) {
	const secret = "sup3rs3cr3t-pw"
	ra := &RestoreArgs{
		// Port 1 is reserved and never listening, so the connection fails fast.
		// If mongosh is absent the exec error is taken instead; either way the
		// error path under test is the one that formats the URI.
		URI: "mongodb://admin:" + secret + "@127.0.0.1:1/admin",
	}

	err := checkConnectivity(context.Background(), ra)
	if err == nil {
		t.Fatal("checkConnectivity() unexpectedly succeeded against a closed port")
	}

	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Errorf("password leaked into the error message.\ngot: %s", msg)
	}
	if !strings.Contains(msg, "*****") {
		t.Errorf("expected the password to be replaced by the redaction marker.\ngot: %s", msg)
	}
	// The message must remain actionable.
	if !strings.Contains(msg, "127.0.0.1") {
		t.Errorf("host was lost from the error, leaving it unactionable.\ngot: %s", msg)
	}
	if !strings.Contains(msg, "admin") {
		t.Errorf("username was lost from the error; only the secret should go.\ngot: %s", msg)
	}
}
