// Package dump hosts the cross-engine dump-args factory registry, mirroring
// internal/adapters/storage.NewBackend. It is the only place that imports every
// dump engine sub-package; engine packages never import each other (ADR 0001
// axis isolation).
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

// NewBuilder returns the ports.DumpBuilder for the given engine, wired with the
// injected connectivity prober, or an error for an unsupported engine.
func NewBuilder(engine string, prober ports.DBProber) (ports.DumpBuilder, error) {
	switch engine {
	case "postgres":
		return pg.NewBuilder(prober), nil
	case "mysql":
		return mysql.NewBuilder(prober), nil
	case "mariadb":
		return mariadb.NewBuilder(prober), nil
	case "mongodb":
		return mongo.NewBuilder(prober), nil
	default:
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}
}
