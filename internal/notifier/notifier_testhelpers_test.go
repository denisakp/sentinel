package notifier

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sentinelSecrets are the canonical leak-detection secret values. Each is
// embedded in newFailure* fixtures inside a credential-bearing pattern that
// upstream redaction (internal/sanitize) recognises; tests then assert these
// values never appear verbatim in outbound bytes.
var sentinelSecrets = []string{
	"PASSWORD-S3CRET-do-not-leak",
	"AKIA-FAKEAWSACCESSKEYID",
	"THIS-IS-A-PASSWORD-do-not-leak",
}

// sentinelErrorPayload is the error string that fixtures populate. It mirrors
// the shape of a realistic upstream-redacted error (sentinel.RedactLog output):
// secret values have already been replaced with "*****" before reaching the
// notifier. The channel is an identity sink (see webhook_sink_identity_test);
// these fixtures + assertNoSentinelLeak verify the channel does not inject
// raw sentinel values from elsewhere. Sabotaging a channel to embed a raw
// sentinel (T024 mutation check) will be detected.
const sentinelErrorPayload = "operation aborted: cmd ran with --password=*****; env PGPASSWORD=*****; uri mongodb://user:*****@host/db"

type recordedRequest struct {
	Method  string
	URL     string
	Headers http.Header
	Body    []byte
}

type recordedSMTP struct {
	Addr     string
	From     string
	To       []string
	Msg      []byte
	UseTLS   bool
	Host     string
	Username string
	Password string
}

type fakeHTTPServer struct {
	mu       sync.Mutex
	server   *httptest.Server
	requests []recordedRequest
}

func newFakeHTTPServer(handler http.HandlerFunc) *fakeHTTPServer {
	f := &fakeHTTPServer{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{
			Method:  r.Method,
			URL:     r.URL.String(),
			Headers: r.Header.Clone(),
			Body:    body,
		})
		f.mu.Unlock()
		// Restore body for handler (in case it inspects)
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler(w, r)
	}))
	return f
}

func (f *fakeHTTPServer) URL() string { return f.server.URL }

func (f *fakeHTTPServer) Requests() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func (f *fakeHTTPServer) Close() { f.server.Close() }

var (
	fixedStart = time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	fixedEnd   = time.Date(2026, 5, 21, 10, 0, 5, 0, time.UTC)
)

func newSuccessBackup() *BackupContext {
	return &BackupContext{
		BackupName:   "bk-001",
		DatabaseType: "postgres",
		DatabaseName: "appdb",
		Status:       StatusSuccess,
		StartTime:    fixedStart,
		EndTime:      fixedEnd,
		FilePath:     "/var/backups/bk-001.sql",
		FileSize:     1024,
	}
}

func newFailureBackup() *BackupContext {
	b := newSuccessBackup()
	b.Status = StatusFailure
	b.Error = sentinelErrorPayload
	return b
}

func newSuccessRestore() *RestoreContext {
	return &RestoreContext{
		RestoreName:        "rs-001",
		DatabaseType:       "postgres",
		DatabaseName:       "appdb",
		Status:             StatusSuccess,
		StartTime:          fixedStart,
		EndTime:            fixedEnd,
		BytesRestored:      2048,
		SourceBackupPath:   "/var/backups/bk-001.sql",
		VerificationPassed: true,
	}
}

func newFailureRestore() *RestoreContext {
	r := newSuccessRestore()
	r.Status = StatusFailure
	r.Error = sentinelErrorPayload
	r.VerificationPassed = false
	return r
}

// assertNoSentinelLeak ensures no sentinel secret appears in any of the supplied
// payloads (byte slices or strings).
func assertNoSentinelLeak(t *testing.T, payloads ...any) {
	t.Helper()
	for _, p := range payloads {
		var s string
		switch v := p.(type) {
		case []byte:
			s = string(v)
		case string:
			s = v
		case http.Header:
			for _, vals := range v {
				for _, vv := range vals {
					for _, secret := range sentinelSecrets {
						if strings.Contains(vv, secret) {
							t.Fatalf("sentinel secret %q leaked into header value %q", secret, vv)
						}
					}
				}
			}
			continue
		default:
			t.Fatalf("assertNoSentinelLeak: unsupported payload type %T", p)
		}
		for _, secret := range sentinelSecrets {
			if strings.Contains(s, secret) {
				t.Fatalf("sentinel secret %q leaked into payload %q", secret, s)
			}
		}
	}
}

// allEventsWebhook returns a config that accepts every event status.
func allEventsWebhook(url string) *WebhookNotificationConfig {
	return &WebhookNotificationConfig{
		WebhookURL:     url,
		Events:         []string{"success", "failure", "warning"},
		TimeoutSeconds: 5,
		Enabled:        true,
	}
}

// allEventsEmail returns an EmailNotificationConfig that accepts every status.
func allEventsEmail() *EmailNotificationConfig {
	return &EmailNotificationConfig{
		SMTPHost:     "smtp.example.com",
		SMTPPort:     587,
		SMTPUsername: "user",
		SMTPPassword: "REDACTED-PASS",
		FromAddress:  "alerts@example.com",
		ToAddresses:  []string{"ops@example.com"},
		UseTLS:       true,
		Events:       []string{"success", "failure", "warning"},
		Enabled:      true,
	}
}

// okHandler returns 200 with empty JSON body.
func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}
}
