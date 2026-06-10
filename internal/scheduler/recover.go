package scheduler

import (
	"fmt"
	"runtime/debug"
	"unicode/utf8"
)

const maxPanicMsgBytes = 512

// HandlePanic is the exported alias of handlePanic for use by CLI-level
// per-attempt recover blocks (FR-008). See handlePanic for usage rules.
func HandlePanic(r any) (panicErr error, stack []byte) { return handlePanic(r) }

// handlePanic converts the result of a `recover()` call into an error suitable
// for the monitor failure path plus a captured stack trace. Returns (nil, nil)
// when r is nil (no panic in flight).
//
// Idiomatic usage — `recover()` MUST be called directly inside a deferred
// function per the Go spec:
//
//	defer func() {
//	    if pErr, stack := handlePanic(recover()); pErr != nil {
//	        // log and/or set result.Error
//	    }
//	}()
func handlePanic(r any) (panicErr error, stack []byte) {
	if r == nil {
		return nil, nil
	}
	return wrapPanic(r), debug.Stack()
}

// wrapPanic converts a recovered panic value into an error suitable for the
// monitor failure path. The rendered message is rune-safely truncated to
// maxPanicMsgBytes. If r is itself an error, the result wraps it with %w so
// errors.Is / errors.As keep working (only when no truncation occurs).
func wrapPanic(r any) error {
	if r == nil {
		return fmt.Errorf("worker panic: <nil>")
	}
	if e, ok := r.(error); ok {
		truncated := truncateRuneSafe(e.Error(), maxPanicMsgBytes)
		if truncated == e.Error() {
			return fmt.Errorf("worker panic: %w", e)
		}
		return fmt.Errorf("worker panic: %s", truncated)
	}
	msg := truncateRuneSafe(fmt.Sprintf("%v", r), maxPanicMsgBytes)
	return fmt.Errorf("worker panic: %s", msg)
}

// truncateRuneSafe returns s truncated to at most maxBytes bytes, never
// splitting a multi-byte UTF-8 rune.
func truncateRuneSafe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	out := s[:maxBytes]
	for len(out) > 0 {
		r, size := utf8.DecodeLastRuneInString(out)
		if r != utf8.RuneError || size > 1 {
			return out
		}
		out = out[:len(out)-1]
	}
	return out
}
