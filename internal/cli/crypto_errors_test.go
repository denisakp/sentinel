package cli

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/denisakp/sentinel/internal/crypto"
)

func TestFriendlyDecryptError(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantMsg string
		wantOK  bool
	}{
		{"nil", nil, "", false},
		{"plain", errors.New("something else"), "", false},
		{"eof", io.EOF, "", false},
		{"unexpected_eof", io.ErrUnexpectedEOF, "", false},
		{"chunk_too_large_bare", crypto.ErrChunkTooLarge, "file corrupt or wrong key", true},
		{"chunk_too_large_wrapped", fmt.Errorf("decrypt: %w", crypto.ErrChunkTooLarge), "file corrupt or wrong key", true},
		{"chunk_too_large_double_wrapped", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", crypto.ErrChunkTooLarge)), "file corrupt or wrong key", true},
		{"auth_tag_bare", crypto.ErrAuthTagFailed, "file corrupt or wrong key", true},
		{"auth_tag_wrapped", fmt.Errorf("chunk 3 failed: %w", crypto.ErrAuthTagFailed), "file corrupt or wrong key", true},
		{"legacy_envelope_not_mapped", crypto.ErrLegacyEnvelope, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := FriendlyDecryptError(tc.err)
			if ok != tc.wantOK || msg != tc.wantMsg {
				t.Fatalf("FriendlyDecryptError(%v) = (%q, %v), want (%q, %v)", tc.err, msg, ok, tc.wantMsg, tc.wantOK)
			}
		})
	}
}
