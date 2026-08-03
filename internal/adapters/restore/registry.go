// Package restore hosts the cross-engine restore-args factory registry,
// mirroring internal/adapters/dump.NewArgsFactory and
// internal/adapters/storage.NewBackend. It is the only place that imports every
// restore engine sub-package; engine packages never import each other (ADR 0001
// axis isolation).
package restore

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/adapters/restore/mariadb"
	"github.com/denisakp/sentinel/internal/adapters/restore/mongo"
	"github.com/denisakp/sentinel/internal/adapters/restore/mysql"
	"github.com/denisakp/sentinel/internal/adapters/restore/pg"
	"github.com/denisakp/sentinel/internal/ports"
)

// NewArgsFactory returns the ports.RestoreArgsFactory for the given engine type
// (matching config RestoreJob.Type values), or an error for an unsupported engine.
func NewArgsFactory(engine string) (ports.RestoreArgsFactory, error) {
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
