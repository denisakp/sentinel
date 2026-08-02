// Package compress is the driving adapter for pipeline backup compression
// (PRD 33 / spec 049). It implements ports.CompressWriter / DecompressReader
// over two pure-Go codecs:
//
//   - gzip  — stdlib compress/gzip
//   - zstd  — github.com/klauspost/compress/zstd (no cgo)
//
// The stage is engine-agnostic: it mirrors the AES-256-GCM streaming stage in
// internal/adapters/crypto. Domain code never imports this package — the
// backup Executor reaches compression through a Job hook wired by
// internal/cli/backup_factory.go, and the restore runtime (a composition
// root) constructs the decompressor directly. The algorithm/level are carried
// in the backup manifest (ports.CompressionInfo).
package compress

import (
	"compress/gzip"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"

	"github.com/denisakp/sentinel/internal/ports"
)

// Supported pipeline compression algorithms.
const (
	AlgorithmGzip = "gzip"
	AlgorithmZstd = "zstd"
	// AlgorithmNone is a valid config value meaning "no pipeline compression".
	AlgorithmNone = "none"
)

// Default per-algorithm compression levels applied when the config omits a
// level (level == 0). gzip default mirrors the classic level 6; zstd default
// (3) maps to klauspost's SpeedDefault.
const (
	DefaultGzipLevel = 6
	DefaultZstdLevel = 3
)

// Inclusive per-algorithm level bounds. gzip: 1..9 (compress/gzip). zstd:
// 1..19 (the conventional zstd CLI range; klauspost clamps to its four
// internal speed tiers).
const (
	MinGzipLevel = 1
	MaxGzipLevel = 9
	MinZstdLevel = 1
	MaxZstdLevel = 19
)

// IsSupportedAlgorithm reports whether algorithm is one this adapter can build
// a codec for (gzip or zstd). "none" and "" are NOT supported codecs — callers
// treat those as "compression disabled" before reaching here.
func IsSupportedAlgorithm(algorithm string) bool {
	return algorithm == AlgorithmGzip || algorithm == AlgorithmZstd
}

// DefaultLevel returns the per-algorithm default level (used when config
// omits level). Returns 0 for unknown algorithms.
func DefaultLevel(algorithm string) int {
	switch algorithm {
	case AlgorithmGzip:
		return DefaultGzipLevel
	case AlgorithmZstd:
		return DefaultZstdLevel
	default:
		return 0
	}
}

// NewCompressWriter returns a streaming compressor for algorithm writing the
// compressed stream to w. A zero level selects the per-algorithm default.
func NewCompressWriter(w io.Writer, algorithm string, level int) (ports.CompressWriter, error) {
	switch algorithm {
	case AlgorithmGzip:
		if level == 0 {
			level = DefaultGzipLevel
		}
		gw, err := gzip.NewWriterLevel(w, level)
		if err != nil {
			return nil, fmt.Errorf("compress: invalid gzip level %d: %w", level, err)
		}
		return gw, nil
	case AlgorithmZstd:
		if level == 0 {
			level = DefaultZstdLevel
		}
		zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
		if err != nil {
			return nil, fmt.Errorf("compress: failed to initialise zstd writer: %w", err)
		}
		return zw, nil
	default:
		return nil, fmt.Errorf("compress: unsupported algorithm %q (want gzip or zstd)", algorithm)
	}
}

// NewDecompressReader returns a streaming decompressor for algorithm reading
// the compressed stream from r. The restore runtime selects the algorithm
// from the backup manifest.
func NewDecompressReader(r io.Reader, algorithm string) (ports.DecompressReader, error) {
	switch algorithm {
	case AlgorithmGzip:
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("compress: failed to initialise gzip reader: %w", err)
		}
		return gr, nil
	case AlgorithmZstd:
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("compress: failed to initialise zstd reader: %w", err)
		}
		return &zstdReadCloser{Decoder: zr}, nil
	default:
		return nil, fmt.Errorf("compress: unsupported algorithm %q (want gzip or zstd)", algorithm)
	}
}

// zstdReadCloser adapts *zstd.Decoder (whose Close() returns nothing) to
// ports.DecompressReader (Close() error).
type zstdReadCloser struct {
	*zstd.Decoder
}

// Close releases the decoder. *zstd.Decoder.Close never reports an error.
func (z *zstdReadCloser) Close() error {
	z.Decoder.Close()
	return nil
}
