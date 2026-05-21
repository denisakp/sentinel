package notifier

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
)

// captureSend installs a send seam on n that records each invocation into the returned slice.
func captureSend(n *EmailNotifier) (*[]recordedSMTP, *sync.Mutex) {
	var mu sync.Mutex
	var recs []recordedSMTP
	n.send = func(addr, from string, to []string, msg []byte, useTLS bool, host, user, pass string) error {
		mu.Lock()
		defer mu.Unlock()
		recs = append(recs, recordedSMTP{
			Addr: addr, From: from, To: to, Msg: msg,
			UseTLS: useTLS, Host: host, Username: user, Password: pass,
		})
		return nil
	}
	return &recs, &mu
}

// ---- US1: happy path ----

func TestEmailSendBackup_Success(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	recs, _ := captureSend(n)

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 1 {
		t.Fatalf("expected 1 send, got %d", len(*recs))
	}
	msg := string((*recs)[0].Msg)
	for _, h := range []string{"From:", "To:", "Subject:", "Date:", "Content-Type:"} {
		if !strings.Contains(msg, h) {
			t.Fatalf("msg missing header %q: %s", h, msg)
		}
	}
	if !strings.Contains(msg, "Backup Execution Report") {
		t.Fatalf("msg missing backup body")
	}
}

func TestEmailSendRestore_Success(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	recs, _ := captureSend(n)

	if err := n.SendRestore(context.Background(), newSuccessRestore()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 1 {
		t.Fatalf("expected 1 send")
	}
	if !strings.Contains(string((*recs)[0].Msg), "Restore Execution Report") {
		t.Fatalf("msg missing restore body")
	}
}

func TestEmailSend_Deprecated(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	recs, _ := captureSend(n)

	if err := n.Send(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 1 {
		t.Fatalf("expected 1 send")
	}
}

// ---- US2: failure modes ----

func TestEmailSendBackup_TransportError(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	transportErr := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	n.send = func(addr, from string, to []string, msg []byte, useTLS bool, host, user, pass string) error {
		return transportErr
	}

	err := n.SendBackup(context.Background(), newSuccessBackup())
	if err == nil {
		t.Fatal("expected error")
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected *net.OpError via errors.As, got %v", err)
	}
}

func TestEmailSendBackup_SendStepError(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	sentinel := errors.New("failed to set sender address")
	n.send = func(addr, from string, to []string, msg []byte, useTLS bool, host, user, pass string) error {
		return sentinel
	}

	err := n.SendBackup(context.Background(), newSuccessBackup())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected errors.Is to sentinel, got %v", err)
	}
}

// ---- US3: redaction ----

func TestEmailSendBackup_RedactionBackupError(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	recs, _ := captureSend(n)

	if err := n.SendBackup(context.Background(), newFailureBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 1 {
		t.Fatalf("expected 1 send")
	}
	assertNoSentinelLeak(t, (*recs)[0].Msg)
}

func TestEmailSendRestore_RedactionRestoreError(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	recs, _ := captureSend(n)

	if err := n.SendRestore(context.Background(), newFailureRestore()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 1 {
		t.Fatalf("expected 1 send")
	}
	assertNoSentinelLeak(t, (*recs)[0].Msg)
}

// ---- US4: short-circuit + single-attempt ----

func TestEmailSendBackup_Disabled(t *testing.T) {
	cfg := allEventsEmail()
	cfg.Enabled = false
	n := NewEmailNotifier(cfg)
	recs, _ := captureSend(n)

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 0 {
		t.Fatalf("expected 0 sends, got %d", len(*recs))
	}
}

func TestEmailSendBackup_EventFiltered(t *testing.T) {
	cfg := allEventsEmail()
	cfg.Events = []string{"failure"}
	n := NewEmailNotifier(cfg)
	recs, _ := captureSend(n)

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 0 {
		t.Fatalf("expected 0 sends")
	}
}

func TestEmailSendBackup_SingleAttemptOnSuccess(t *testing.T) {
	n := NewEmailNotifier(allEventsEmail())
	recs, _ := captureSend(n)

	if err := n.SendBackup(context.Background(), newSuccessBackup()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*recs) != 1 {
		t.Fatalf("expected exactly 1 send, got %d", len(*recs))
	}
}
