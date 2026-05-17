package crypto

import (
	"context"
	"log/slog"
)

// Event names for structured warning logs.
const (
	EventNonceCounterOverflow   = "crypto.nonce_counter_overflow"
	EventLegacyEnvelopeDecrypt  = "crypto.legacy_envelope_decrypt"
	EventEnvelopeUnknownVersion = "crypto.envelope_unknown_version"
)

// logCryptoEvent emits a structured WARN log line tagged with the event name.
func logCryptoEvent(ctx context.Context, event string, attrs ...slog.Attr) {
	args := make([]any, 0, 2+2*len(attrs))
	args = append(args, slog.String("event", event))
	for _, a := range attrs {
		args = append(args, a)
	}
	if ctx == nil {
		slog.Default().Warn(event, args...)
		return
	}
	slog.Default().WarnContext(ctx, event, args...)
}
