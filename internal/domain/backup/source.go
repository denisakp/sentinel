package backup

// Source-list resolution for auto-discovery ("database: *") jobs.
// Relocated from internal/cli/backup.go::listDatabases by spec 038 Sub-PR K;
// SQL enumeration goes through ports.DBProber. Mongo enumeration stays in
// the driving adapter (the prober port's DatabaseConfig carries no URI).

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// ListSQLSources enumerates the databases visible to the job's credentials
// for SQL engines. mariadb reuses the mysql protocol probe.
func ListSQLSources(ctx context.Context, prober ports.DBProber, job Job) ([]string, error) {
	if prober == nil {
		return nil, fmt.Errorf("database prober is not configured")
	}
	conn := job.DBConn
	switch job.Engine {
	case "postgres":
		conn.Type = "postgres"
	case "mysql", "mariadb":
		conn.Type = "mysql"
	default:
		return nil, fmt.Errorf("unsupported database type '%s'", job.Engine)
	}
	return prober.ListDatabases(ctx, conn)
}

// FilterExcludedSources returns names minus the excluded set, preserving
// order. Pure.
func FilterExcludedSources(names, excluded []string) []string {
	if len(excluded) == 0 {
		return names
	}
	skip := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		skip[name] = struct{}{}
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if _, drop := skip[name]; drop {
			continue
		}
		kept = append(kept, name)
	}
	return kept
}
