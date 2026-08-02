package compress

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// sampleData returns a compressible payload (repetitive SQL-like text).
func sampleData() []byte {
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("INSERT INTO t (a, b, c) VALUES (1, 'hello world', 'compress me');\n")
	}
	return []byte(b.String())
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		algorithm string
		level     int
	}{
		{AlgorithmGzip, 0}, // default
		{AlgorithmGzip, 1},
		{AlgorithmGzip, 9},
		{AlgorithmZstd, 0}, // default
		{AlgorithmZstd, 1},
		{AlgorithmZstd, 19},
	}

	original := sampleData()
	for _, tc := range cases {
		t.Run(tc.algorithm, func(t *testing.T) {
			var compressed bytes.Buffer
			cw, err := NewCompressWriter(&compressed, tc.algorithm, tc.level)
			if err != nil {
				t.Fatalf("NewCompressWriter() error = %v", err)
			}
			if _, err := cw.Write(original); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if err := cw.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}

			if compressed.Len() >= len(original) {
				t.Fatalf("compressed size %d not smaller than original %d", compressed.Len(), len(original))
			}

			dr, err := NewDecompressReader(bytes.NewReader(compressed.Bytes()), tc.algorithm)
			if err != nil {
				t.Fatalf("NewDecompressReader() error = %v", err)
			}
			defer dr.Close()

			got, err := io.ReadAll(dr)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(original))
			}
		})
	}
}

func TestNewCompressWriterUnsupportedAlgorithm(t *testing.T) {
	if _, err := NewCompressWriter(&bytes.Buffer{}, "lz4", 0); err == nil {
		t.Fatal("expected error for unsupported compress algorithm")
	}
	if _, err := NewCompressWriter(&bytes.Buffer{}, AlgorithmNone, 0); err == nil {
		t.Fatal("expected error for 'none' algorithm (callers must gate this out)")
	}
}

func TestNewDecompressReaderUnsupportedAlgorithm(t *testing.T) {
	if _, err := NewDecompressReader(bytes.NewReader(nil), "lz4"); err == nil {
		t.Fatal("expected error for unsupported decompress algorithm")
	}
}

func TestNewCompressWriterInvalidGzipLevel(t *testing.T) {
	// gzip level 42 is out of the stdlib range; the writer constructor rejects it.
	if _, err := NewCompressWriter(&bytes.Buffer{}, AlgorithmGzip, 42); err == nil {
		t.Fatal("expected error for out-of-range gzip level")
	}
}

func TestIsSupportedAlgorithm(t *testing.T) {
	for _, a := range []string{AlgorithmGzip, AlgorithmZstd} {
		if !IsSupportedAlgorithm(a) {
			t.Errorf("IsSupportedAlgorithm(%q) = false, want true", a)
		}
	}
	for _, a := range []string{AlgorithmNone, "", "lz4"} {
		if IsSupportedAlgorithm(a) {
			t.Errorf("IsSupportedAlgorithm(%q) = true, want false", a)
		}
	}
}

func TestDefaultLevel(t *testing.T) {
	if got := DefaultLevel(AlgorithmGzip); got != DefaultGzipLevel {
		t.Errorf("DefaultLevel(gzip) = %d, want %d", got, DefaultGzipLevel)
	}
	if got := DefaultLevel(AlgorithmZstd); got != DefaultZstdLevel {
		t.Errorf("DefaultLevel(zstd) = %d, want %d", got, DefaultZstdLevel)
	}
	if got := DefaultLevel("none"); got != 0 {
		t.Errorf("DefaultLevel(none) = %d, want 0", got)
	}
}

// TestCrossAlgorithmMismatchFails ensures a zstd stream cannot be decoded as
// gzip (and gives a clear error rather than silent corruption).
func TestCrossAlgorithmMismatchFails(t *testing.T) {
	var compressed bytes.Buffer
	cw, err := NewCompressWriter(&compressed, AlgorithmZstd, 0)
	if err != nil {
		t.Fatalf("NewCompressWriter() error = %v", err)
	}
	if _, err := cw.Write(sampleData()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := cw.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// gzip reader construction reads/validates the header eagerly and should fail.
	if _, err := NewDecompressReader(bytes.NewReader(compressed.Bytes()), AlgorithmGzip); err == nil {
		t.Fatal("expected error decoding a zstd stream as gzip")
	}
}
