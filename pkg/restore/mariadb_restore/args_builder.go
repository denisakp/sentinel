package mariadb_restore

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	internaltls "github.com/denisakp/sentinel/internal/tls"
	"github.com/denisakp/sentinel/internal/ports"
)

// BuildTLSArgs returns the MariaDB TLS connection arguments for mariadb restore.
func BuildTLSArgs(tlsCfg *ports.Config) []string {
	return sanitize.RedactArgs(internaltls.BuildTLSArgs("mariadb", tlsCfg))
}
