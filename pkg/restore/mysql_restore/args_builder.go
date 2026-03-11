package mysql_restore

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	internaltls "github.com/denisakp/sentinel/internal/tls"
)

// BuildTLSArgs returns the MySQL TLS connection arguments for mysql restore.
func BuildTLSArgs(tlsCfg *internaltls.Config) []string {
	return sanitize.RedactArgs(internaltls.BuildTLSArgs("mysql", tlsCfg))
}
