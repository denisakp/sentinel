package cli

import "os"

// legacyEnvelopeEnvDefault returns true when SENTINEL_ALLOW_LEGACY_ENVELOPE is
// set to a truthy value ("1", "true", "yes"); used as the default for the
// --allow-legacy-envelope flag on restore/backup-verify commands.
func legacyEnvelopeEnvDefault() bool {
	v := os.Getenv("SENTINEL_ALLOW_LEGACY_ENVELOPE")
	switch v {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	}
	return false
}

// LegacyEnvelopeRefusalMsg is the operator-facing error string emitted when a
// legacy (pre-v2) artifact is refused at the CLI boundary.
const LegacyEnvelopeRefusalMsg = "refusing to decrypt legacy (pre-v2) envelope for backup %q from %q — re-encrypt from source, or pass --allow-legacy-envelope to proceed at your own risk"

// LegacyEnvelopeOptInWarnMsg is the WARNING line emitted (stderr + log) when a
// legacy artifact is decrypted via the explicit opt-in.
const LegacyEnvelopeOptInWarnMsg = "WARNING: decrypting legacy (pre-v2) envelope for backup %q from %q — this artifact was produced with a flawed nonce scheme; treat plaintext as recovered, not as safe to keep"
