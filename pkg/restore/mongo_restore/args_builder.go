package mongo_restore

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	internaltls "github.com/denisakp/sentinel/internal/tls"
)

// BuildTLSArgs returns the MongoDB TLS connection arguments for mongorestore.
func BuildTLSArgs(tlsCfg *internaltls.Config) []string {
	return sanitize.RedactArgs(internaltls.BuildTLSArgs("mongodb", tlsCfg))
}
