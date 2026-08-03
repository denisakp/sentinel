package pg

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	internaltls "github.com/denisakp/sentinel/internal/adapters/tls"
	"github.com/denisakp/sentinel/internal/ports"
)

// BuildTLSArgs returns the PostgreSQL TLS connection arguments for pg_restore.
// Returns nil when tlsCfg is nil or TLS is disabled.
func BuildTLSArgs(tlsCfg *ports.Config) []string {
	return sanitize.RedactArgs(internaltls.BuildTLSArgs("postgres", tlsCfg))
}
