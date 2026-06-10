package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

// ---- US1: happy path ----

func TestDiscordSendBackup_Success(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	reqs := fake.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if got := reqs[0].Headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(reqs[0].Body, &payload); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if _, ok := payload["embeds"]; !ok {
		t.Fatalf("payload missing 'embeds' key: %v", payload)
	}
}

func TestDiscordSendRestore_Success(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	if err := n.SendRestore(context.Background(), newSuccessRestore()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := len(fake.Requests()); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

func TestDiscordSend_Deprecated(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	if err := n.Send(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := len(fake.Requests()); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

// ---- US2: failure modes ----

func TestDiscordSendBackup_Status500(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	err := n.SendBackup(context.Background(), newSuccessBackup())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ports.ErrNon2xxResponse) {
		t.Fatalf("expected ports.ErrNon2xxResponse, got %v", err)
	}
	for _, s := range []string{"500", "boom", "discord"} {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("err missing %q: %q", s, err.Error())
		}
	}
	if got := len(fake.Requests()); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

func TestDiscordSendBackup_Status429RetryAfter(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	})
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	err := n.SendBackup(context.Background(), newSuccessBackup())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ports.ErrNon2xxResponse) {
		t.Fatalf("expected ports.ErrNon2xxResponse, got %v", err)
	}
	for _, s := range []string{"429", "30", "slow down", "discord"} {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("err missing %q: %q", s, err.Error())
		}
	}
	if got := len(fake.Requests()); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

func TestDiscordSendBackup_TransportError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	n := NewDiscordNotifier(allEventsWebhook("http://" + addr))
	err = n.SendBackup(context.Background(), newSuccessBackup())
	if err == nil {
		t.Fatal("expected transport error")
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) && !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("expected transport error chain, got %v", err)
	}
}

func TestDiscordSendBackup_Timeout(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	})
	defer fake.Close()

	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))
	n.client.Timeout = 50 * time.Millisecond

	start := time.Now()
	err := n.SendBackup(context.Background(), newSuccessBackup())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestDiscordSendBackup_MalformedBody(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<<<not json>>>"))
	})
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := len(fake.Requests()); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

func TestDiscordSendBackup_BadURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"malformed", "://not a url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			n := NewDiscordNotifier(allEventsWebhook(tc.url))
			if err := n.SendBackup(context.Background(), newSuccessBackup()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// ---- US3: redaction ----

func TestDiscordSendBackup_RedactionBackupError(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	if err := n.SendBackup(context.Background(), newFailureBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	reqs := fake.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request")
	}
	assertNoSentinelLeak(t, reqs[0].Body, reqs[0].Headers)
}

func TestDiscordSendRestore_RedactionRestoreError(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	if err := n.SendRestore(context.Background(), newFailureRestore()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	reqs := fake.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request")
	}
	assertNoSentinelLeak(t, reqs[0].Body, reqs[0].Headers)
}

// ---- US4: short-circuit + single-attempt ----

func TestDiscordSendBackup_Disabled(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	cfg := allEventsWebhook(fake.URL())
	cfg.Enabled = false
	n := NewDiscordNotifier(cfg)

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := len(fake.Requests()); got != 0 {
		t.Fatalf("expected 0 requests, got %d", got)
	}
}

func TestDiscordSendBackup_EventFiltered(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	cfg := allEventsWebhook(fake.URL())
	cfg.Events = []string{"failure"}
	n := NewDiscordNotifier(cfg)

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := len(fake.Requests()); got != 0 {
		t.Fatalf("expected 0 requests, got %d", got)
	}
}

func TestDiscordSendBackup_CancelledContext(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := n.SendBackup(ctx, newSuccessBackup())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := len(fake.Requests()); got != 0 {
		t.Fatalf("expected 0 requests, got %d", got)
	}
}

func TestDiscordSendBackup_SingleAttemptOnSuccess(t *testing.T) {
	fake := newFakeHTTPServer(okHandler())
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := len(fake.Requests()); got != 1 {
		t.Fatalf("expected exactly 1 request, got %d", got)
	}
}
