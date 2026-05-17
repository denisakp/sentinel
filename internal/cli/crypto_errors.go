package cli

import (
	"errors"

	"github.com/denisakp/sentinel/internal/crypto"
)

const friendlyDecryptMessage = "file corrupt or wrong key"

// FriendlyDecryptError maps decrypt-path errors to a uniform operator-facing
// message. Returns (msg, true) when err is a wrap of ErrChunkTooLarge or
// ErrAuthTagFailed; otherwise ("", false). The underlying error is preserved
// for logging via %w wrapping at the call site.
func FriendlyDecryptError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	if errors.Is(err, crypto.ErrChunkTooLarge) || errors.Is(err, crypto.ErrAuthTagFailed) {
		return friendlyDecryptMessage, true
	}
	return "", false
}
