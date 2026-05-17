package crypto

import (
	"context"
	"log/slog"
	"sync"
)

// recordingHandler is a test slog handler that counts records by event name.
type recordingHandler struct {
	mu     sync.Mutex
	events map[string]int
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.events == nil {
		h.events = make(map[string]int)
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "event" {
			h.events[a.Value.String()]++
		}
		return true
	})
	return nil
}

func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *recordingHandler) eventsFor(name string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events[name]
}
