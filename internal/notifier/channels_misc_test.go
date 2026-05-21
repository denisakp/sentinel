package notifier

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
)

// ---- Type / IsEnabled accessors ----

func TestChannel_TypeAndIsEnabled(t *testing.T) {
	cfg := allEventsWebhook("http://example.invalid")
	ecfg := allEventsEmail()

	cases := []struct {
		name     string
		notifier interface {
			Type() string
			IsEnabled() bool
		}
		wantType string
	}{
		{"slack", NewSlackNotifier(cfg), "slack"},
		{"discord", NewDiscordNotifier(cfg), "discord"},
		{"webhook", NewWebhookNotifier(cfg), "webhook"},
		{"email", NewEmailNotifier(ecfg), "email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.notifier.Type(); got != tc.wantType {
				t.Fatalf("Type() = %q, want %q", got, tc.wantType)
			}
			if !tc.notifier.IsEnabled() {
				t.Fatalf("IsEnabled() = false, want true")
			}
		})
	}
}

// ---- Constructor defaults (TimeoutSeconds=0 → 10, SMTPPort=0 → 587) ----

func TestNew_DefaultsApplied(t *testing.T) {
	wcfg := &WebhookNotificationConfig{WebhookURL: "http://x", Events: []string{"success"}, Enabled: true}
	NewSlackNotifier(wcfg)
	if wcfg.TimeoutSeconds != 10 {
		t.Fatalf("slack default timeout: got %d", wcfg.TimeoutSeconds)
	}

	wcfg2 := &WebhookNotificationConfig{WebhookURL: "http://x", Events: []string{"success"}, Enabled: true}
	NewDiscordNotifier(wcfg2)
	if wcfg2.TimeoutSeconds != 10 {
		t.Fatalf("discord default timeout: got %d", wcfg2.TimeoutSeconds)
	}

	wcfg3 := &WebhookNotificationConfig{WebhookURL: "http://x", Events: []string{"success"}, Enabled: true}
	NewWebhookNotifier(wcfg3)
	if wcfg3.TimeoutSeconds != 10 {
		t.Fatalf("webhook default timeout: got %d", wcfg3.TimeoutSeconds)
	}

	ecfg := &EmailNotificationConfig{Enabled: true}
	NewEmailNotifier(ecfg)
	if ecfg.SMTPPort != 587 {
		t.Fatalf("email default port: got %d", ecfg.SMTPPort)
	}
}

// ---- SendRestore failure-mode coverage ----

func TestSlackSendRestore_Status500(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	defer fake.Close()
	n := NewSlackNotifier(allEventsWebhook(fake.URL()))
	err := n.SendRestore(context.Background(), newSuccessRestore())
	if err == nil || !errors.Is(err, ErrNon2xxResponse) {
		t.Fatalf("expected ErrNon2xxResponse, got %v", err)
	}
}

func TestDiscordSendRestore_Status500(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))
	err := n.SendRestore(context.Background(), newSuccessRestore())
	if err == nil || !errors.Is(err, ErrNon2xxResponse) {
		t.Fatalf("expected ErrNon2xxResponse, got %v", err)
	}
}

func TestWebhookSendRestore_Status500(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	defer fake.Close()
	n := NewWebhookNotifier(allEventsWebhook(fake.URL()))
	err := n.SendRestore(context.Background(), newSuccessRestore())
	if err == nil || !errors.Is(err, ErrNon2xxResponse) {
		t.Fatalf("expected ErrNon2xxResponse, got %v", err)
	}
}

func TestSlackSendRestore_Status429RetryAfter(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	})
	defer fake.Close()
	n := NewSlackNotifier(allEventsWebhook(fake.URL()))
	err := n.SendRestore(context.Background(), newSuccessRestore())
	if err == nil || !errors.Is(err, ErrNon2xxResponse) {
		t.Fatalf("expected ErrNon2xxResponse, got %v", err)
	}
	for _, s := range []string{"429", "30", "slow down"} {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("err missing %q: %q", s, err.Error())
		}
	}
}

func TestDiscordSendRestore_Status429RetryAfter(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	})
	defer fake.Close()
	n := NewDiscordNotifier(allEventsWebhook(fake.URL()))
	err := n.SendRestore(context.Background(), newSuccessRestore())
	if err == nil || !errors.Is(err, ErrNon2xxResponse) {
		t.Fatalf("expected ErrNon2xxResponse, got %v", err)
	}
}

func TestWebhookSendRestore_Status429RetryAfter(t *testing.T) {
	fake := newFakeHTTPServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	})
	defer fake.Close()
	n := NewWebhookNotifier(allEventsWebhook(fake.URL()))
	err := n.SendRestore(context.Background(), newSuccessRestore())
	if err == nil || !errors.Is(err, ErrNon2xxResponse) {
		t.Fatalf("expected ErrNon2xxResponse, got %v", err)
	}
}

func TestSlackSendRestore_BadURL(t *testing.T) {
	n := NewSlackNotifier(allEventsWebhook("://not a url"))
	if err := n.SendRestore(context.Background(), newSuccessRestore()); err == nil {
		t.Fatal("expected error")
	}
}

func TestDiscordSendRestore_BadURL(t *testing.T) {
	n := NewDiscordNotifier(allEventsWebhook("://not a url"))
	if err := n.SendRestore(context.Background(), newSuccessRestore()); err == nil {
		t.Fatal("expected error")
	}
}

func TestWebhookSendRestore_BadURL(t *testing.T) {
	n := NewWebhookNotifier(allEventsWebhook("://not a url"))
	if err := n.SendRestore(context.Background(), newSuccessRestore()); err == nil {
		t.Fatal("expected error")
	}
}

// ---- smtpDialSend coverage via closed-listener dial failure ----

func TestEmailSmtpDialSend_DialError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	n := NewEmailNotifier(allEventsEmail())
	// invoke default smtpDialSend directly via the seam (don't override)
	err = n.smtpDialSend(addr, "from@x", []string{"to@x"}, []byte("msg"), false, "host", "", "")
	if err == nil {
		t.Fatal("expected dial error")
	}
	if !strings.Contains(err.Error(), "failed to connect to SMTP server") {
		t.Fatalf("unexpected err: %v", err)
	}
}

// ---- Email failure: SendRestore goes through full path with seam error ----

func TestEmailSendRestore_SendStepError(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	sentinel := errors.New("failed to write email body")
	n.send = func(addr, from string, to []string, msg []byte, useTLS bool, host, user, pass string) error {
		return sentinel
	}
	err := n.SendRestore(context.Background(), newSuccessRestore())
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel via errors.Is, got %v", err)
	}
}

// ---- Email warning-status path (covers ShouldNotify default branches) ----

func TestEmailSendBackup_WarningStatus(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	recs, _ := captureSend(n)
	b := newSuccessBackup()
	b.Status = StatusWarning
	if err := n.SendBackup(context.Background(), b); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 1 {
		t.Fatalf("expected 1 send")
	}
}
