package ports

import "io"

// CompressWriter abstracts *internal/adapters/compress streaming compressors
// (gzip / zstd). It is the sibling of EncryptWriter: a streaming pipeline
// stage the backup Executor reaches through a Job hook wired by the driving
// factory.
//
// Implementations consume plaintext dump bytes via io.Writer and emit the
// compressed stream to the wrapped writer. Close finalizes the stream
// (flushes the compressor's buffered data + trailer); callers MUST Close
// before reading the produced artifact's final bytes/size.
type CompressWriter interface {
	io.Writer
	// Close flushes any buffered data and writes the compression trailer.
	Close() error
}

// DecompressReader abstracts *internal/adapters/compress streaming
// decompressors (gzip / zstd). It is the sibling of DecryptReader: the
// restore runtime inserts it AFTER decryption, reading the compressed
// artifact and yielding the plaintext dump. The algorithm is read from the
// backup manifest (no operator flag).
type DecompressReader interface {
	io.Reader
	// Close releases decompressor resources.
	Close() error
}
