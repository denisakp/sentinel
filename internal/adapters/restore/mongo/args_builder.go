package mongo

import (
	"github.com/denisakp/sentinel/internal/sanitize"
	mongotls "github.com/denisakp/sentinel/internal/adapters/dump/mongo"
	"github.com/denisakp/sentinel/internal/ports"
)

// PrepareTLS returns the MongoDB TLS connection arguments for mongorestore
// together with the prepared material handle. Callers MUST defer
// material.Close() on the non-nil return.
func PrepareTLS(tlsCfg *ports.Config, jobID string) (*mongotls.MongoTLSMaterial, []string, error) {
	material, args, err := mongotls.PrepareMongoTLS(tlsCfg, jobID)
	if err != nil {
		return nil, nil, err
	}
	return material, sanitize.RedactArgs(args), nil
}
