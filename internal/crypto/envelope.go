package crypto

import (
	"errors"
	"fmt"
	"io"
)

// MagicV2 is the 4-byte ASCII prefix that marks an envelope-v2 encrypted stream.
var MagicV2 = [4]byte{'S', 'E', 'N', 'C'}

// EnvelopeVersionV2 is the current on-disk envelope version byte.
const EnvelopeVersionV2 uint8 = 0x02

// headerSize is the fixed byte size of the v2 envelope header (4 magic + 1 version).
const headerSize = 5

// ErrShortNonce indicates the AEAD's nonce size is too small to host the 8-byte counter region.
var ErrShortNonce = errors.New("crypto: AEAD nonce shorter than 8-byte counter region")

// ErrChunkCounterOverflow indicates the per-stream chunk counter would overflow uint64.
var ErrChunkCounterOverflow = errors.New("crypto: chunk counter would overflow uint64 — rotate the encryption key and re-encrypt from source")

// ErrLegacyEnvelope indicates a stream lacks the v2 magic header and the caller did not opt in.
var ErrLegacyEnvelope = errors.New("crypto: legacy (pre-v2) envelope detected — re-encrypt from source, or pass --allow-legacy-envelope to proceed at your own risk")

// ErrUnsupportedEnvelopeVersion indicates the stream header carries an unknown version byte.
type ErrUnsupportedEnvelopeVersion struct {
	Version uint8
}

func (e ErrUnsupportedEnvelopeVersion) Error() string {
	return fmt.Sprintf("crypto: unsupported envelope version 0x%02X — upgrade Sentinel", e.Version)
}

// writeHeader emits the 5-byte v2 envelope header (magic + version) to w.
func writeHeader(w io.Writer) error {
	hdr := [headerSize]byte{MagicV2[0], MagicV2[1], MagicV2[2], MagicV2[3], EnvelopeVersionV2}
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("crypto: failed to write envelope header: %w", err)
	}
	return nil
}

// readAndClassifyHeader reads exactly 5 bytes from r and classifies the stream:
//   - magic match + known version → (version, false, nil, nil)
//   - magic match + unknown version → (version, false, nil, ErrUnsupportedEnvelopeVersion{Version})
//   - magic mismatch → (0, true, raw5bytes, nil)
//   - short read → (0, false, nil, wrapped io.ErrUnexpectedEOF)
func readAndClassifyHeader(r io.Reader) (version uint8, isLegacy bool, leadBytes []byte, err error) {
	buf := make([]byte, headerSize)
	if _, rerr := io.ReadFull(r, buf); rerr != nil {
		if errors.Is(rerr, io.ErrUnexpectedEOF) || errors.Is(rerr, io.EOF) {
			return 0, false, nil, fmt.Errorf("crypto: short envelope header: %w", rerr)
		}
		return 0, false, nil, fmt.Errorf("crypto: failed to read envelope header: %w", rerr)
	}

	if buf[0] != MagicV2[0] || buf[1] != MagicV2[1] || buf[2] != MagicV2[2] || buf[3] != MagicV2[3] {
		return 0, true, buf, nil
	}

	v := buf[4]
	if v != EnvelopeVersionV2 {
		return v, false, nil, ErrUnsupportedEnvelopeVersion{Version: v}
	}
	return v, false, nil, nil
}
