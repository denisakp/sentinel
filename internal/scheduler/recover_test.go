package scheduler

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHandlePanic_NoPanicReturnsNil(t *testing.T) {
	pErr, stack := handlePanic(nil)
	if pErr != nil || stack != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", pErr, stack)
	}
}

func TestHandlePanic_StringPanic(t *testing.T) {
	var got error
	var gotStack []byte
	func() {
		defer func() {
			got, gotStack = handlePanic(recover())
		}()
		panic("boom")
	}()
	if got == nil || !strings.HasPrefix(got.Error(), "worker panic: ") {
		t.Fatalf("expected prefixed worker panic error, got %v", got)
	}
	if !strings.Contains(got.Error(), "boom") {
		t.Fatalf("expected message contains 'boom', got %q", got.Error())
	}
	if len(gotStack) == 0 {
		t.Errorf("expected non-empty stack")
	}
}

func TestWrapPanic_Variants(t *testing.T) {
	sentinel := errors.New("kaboom")
	tests := []struct {
		name      string
		in        any
		wantSub   string
		wantIsErr bool
	}{
		{"string", "boom", "worker panic: boom", false},
		{"error", sentinel, "worker panic: kaboom", true},
		{"struct", struct{ V int }{42}, "worker panic: {42}", false},
		{"nil", nil, "worker panic: <nil>", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := wrapPanic(tc.in)
			if err == nil {
				t.Fatalf("wrapPanic returned nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("got %q, want substring %q", err.Error(), tc.wantSub)
			}
			if tc.wantIsErr && !errors.Is(err, sentinel) {
				t.Errorf("expected errors.Is to find sentinel; got false")
			}
		})
	}
}

func TestWrapPanic_TruncatesAtRuneBoundary(t *testing.T) {
	// Build a 2 KB payload of multi-byte runes (3-byte each: '世').
	big := strings.Repeat("世", 700) // 2100 bytes, all 3-byte runes
	err := wrapPanic(big)
	msg := err.Error()
	// Strip prefix "worker panic: " — 14 ASCII bytes.
	const prefix = "worker panic: "
	if !strings.HasPrefix(msg, prefix) {
		t.Fatalf("missing prefix: %q", msg)
	}
	body := msg[len(prefix):]
	if len(body) > maxPanicMsgBytes {
		t.Errorf("body len=%d exceeds %d", len(body), maxPanicMsgBytes)
	}
	if !utf8.ValidString(body) {
		t.Errorf("truncated body has invalid UTF-8: %q", body)
	}
}

func TestWrapPanic_ErrorTruncatedDropsWrap(t *testing.T) {
	sentinel := errors.New(strings.Repeat("x", 1000))
	err := wrapPanic(sentinel)
	if len(err.Error()) > len("worker panic: ")+maxPanicMsgBytes {
		t.Errorf("truncated error too long: %d", len(err.Error()))
	}
	// Per implementation, %w wrap is dropped when truncation occurs.
	if errors.Is(err, sentinel) {
		t.Errorf("expected truncated error to no longer wrap sentinel")
	}
}

func TestWrapPanic_ErrorPreservesWrapWhenShort(t *testing.T) {
	sentinel := errors.New("short")
	err := wrapPanic(sentinel)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected errors.Is to find sentinel for short error")
	}
}
