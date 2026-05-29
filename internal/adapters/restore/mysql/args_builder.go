package mysql

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	internaltls "github.com/denisakp/sentinel/internal/adapters/tls"
	"github.com/denisakp/sentinel/internal/ports"
)

// BuildTLSArgs returns the MySQL TLS connection arguments for mysql restore.
func BuildTLSArgs(tlsCfg *ports.Config) []string {
	return sanitize.RedactArgs(internaltls.BuildTLSArgs("mysql", tlsCfg))
}
