package crypto

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertions that the concrete adapter types satisfy their
// declared ports. A signature drift on either side breaks the build here
// rather than at distant call sites.
var (
	_ ports.EncryptWriter = (*ChunkEncryptWriter)(nil)
	_ ports.DecryptReader = (*ChunkDecryptReader)(nil)
	_ ports.Hasher        = (*HashingWriter)(nil)
	_ ports.KeyProvider   = (*FileKeyProvider)(nil)
)
