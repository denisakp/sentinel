// Package dump hosts the cross-engine dump-args factory registry (spec 040 /
// PRD 27), mirroring internal/adapters/storage.NewBackend. It is the only place
// that imports every dump engine sub-package; engine packages never import each
// other (ADR 0001 axis isolation).
package dump

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/adapters/dump/mariadb"
	"github.com/denisakp/sentinel/internal/adapters/dump/mongo"
	"github.com/denisakp/sentinel/internal/adapters/dump/mysql"
	"github.com/denisakp/sentinel/internal/adapters/dump/pg"
	"github.com/denisakp/sentinel/internal/ports"
)

// NewArgsFactory returns the ports.DumpArgsFactory for the given engine type
// (matching config BackupJob.Type values), or an error for an unsupported engine.
func NewArgsFactory(engine string) (ports.DumpArgsFactory, error) {
	switch engine {
	case "postgres":
		return pg.ArgsFactory{}, nil
	case "mysql":
		return mysql.ArgsFactory{}, nil
	case "mariadb":
		return mariadb.ArgsFactory{}, nil
	case "mongodb":
		return mongo.ArgsFactory{}, nil
	default:
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}
}
