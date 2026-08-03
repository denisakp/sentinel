package mariadb

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	internaltls "github.com/denisakp/sentinel/internal/adapters/tls"
	"github.com/denisakp/sentinel/internal/ports"
)

// BuildTLSArgs returns the MariaDB TLS connection arguments for mariadb restore.
func BuildTLSArgs(tlsCfg *ports.Config) []string {
	return sanitize.RedactArgs(internaltls.BuildTLSArgs("mariadb", tlsCfg))
}
