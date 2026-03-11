package mariadb_restore

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	internaltls "github.com/denisakp/sentinel/internal/tls"
)

// BuildTLSArgs returns the MariaDB TLS connection arguments for mariadb restore.
func BuildTLSArgs(tlsCfg *internaltls.Config) []string {
	return sanitize.RedactArgs(internaltls.BuildTLSArgs("mariadb", tlsCfg))
}
