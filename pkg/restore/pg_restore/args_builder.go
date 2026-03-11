package pg_restore

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	internaltls "github.com/denisakp/sentinel/internal/tls"
)

// BuildTLSArgs returns the PostgreSQL TLS connection arguments for pg_restore.
// Returns nil when tlsCfg is nil or TLS is disabled.
func BuildTLSArgs(tlsCfg *internaltls.Config) []string {
	return sanitize.RedactArgs(internaltls.BuildTLSArgs("postgres", tlsCfg))
}
