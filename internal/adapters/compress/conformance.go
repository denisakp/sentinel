package compress

import (
	"compress/gzip"

	"github.com/klauspost/compress/zstd"

	"github.com/denisakp/sentinel/internal/ports"
)

// Compile-time assertions that the concrete codec types satisfy their declared
// ports. A signature drift on either side breaks the build here rather than at
// distant call sites (mirrors internal/adapters/crypto/conformance.go).
var (
	_ ports.CompressWriter   = (*gzip.Writer)(nil)
	_ ports.CompressWriter   = (*zstd.Encoder)(nil)
	_ ports.DecompressReader = (*gzip.Reader)(nil)
	_ ports.DecompressReader = (*zstdReadCloser)(nil)
)
