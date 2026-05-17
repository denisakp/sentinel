package crypto

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestEnvelopeHeader_V2RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := writeHeader(&buf); err != nil {
		t.Fatalf("writeHeader() error = %v", err)
	}
	if buf.Len() != headerSize {
		t.Fatalf("header size = %d, want %d", buf.Len(), headerSize)
	}
	v, legacy, lead, err := readAndClassifyHeader(&buf)
	if err != nil {
		t.Fatalf("readAndClassifyHeader() error = %v", err)
	}
	if legacy || lead != nil {
		t.Fatalf("v2 header misclassified as legacy")
	}
	if v != EnvelopeVersionV2 {
		t.Errorf("version = 0x%02X, want 0x%02X", v, EnvelopeVersionV2)
	}
}

func TestEnvelopeHeader_UnknownVersion(t *testing.T) {
	r := bytes.NewReader([]byte{'S', 'E', 'N', 'C', 0xFF})
	_, _, _, err := readAndClassifyHeader(r)
	var unsupp ErrUnsupportedEnvelopeVersion
	if !errors.As(err, &unsupp) {
		t.Fatalf("err = %v, want ErrUnsupportedEnvelopeVersion", err)
	}
	if unsupp.Version != 0xFF {
		t.Errorf("Version = 0x%02X, want 0xFF", unsupp.Version)
	}
}

func TestEnvelopeHeader_ShortRead(t *testing.T) {
	r := bytes.NewReader([]byte{'S', 'E', 'N'})
	_, _, _, err := readAndClassifyHeader(r)
	if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.ErrUnexpectedEOF or io.EOF", err)
	}
}

func TestEnvelopeHeader_LegacyShaped(t *testing.T) {
	// Legacy v1 stream begins with a uint32-le chunk length (e.g. 64KB+16 = 0x00010010 = "10 00 01 00").
	legacy := []byte{0x10, 0x00, 0x01, 0x00, 0xAB}
	r := bytes.NewReader(legacy)
	v, isLegacy, lead, err := readAndClassifyHeader(r)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !isLegacy {
		t.Errorf("isLegacy=false, want true")
	}
	if v != 0 {
		t.Errorf("version = 0x%02X, want 0", v)
	}
	if !bytes.Equal(lead, legacy) {
		t.Errorf("lead bytes mismatch: got %x want %x", lead, legacy)
	}
}
