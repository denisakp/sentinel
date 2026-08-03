package db_probe

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// AssessPostgresCascadeSafetyDSN inspects the target Postgres database (at
// the given DSN) for objects that depend on user tables. Such dependents
// would be implicitly dropped by a DROP TABLE ... CASCADE during a "replace"
// restore.
//
// allowCascade lets the caller pre-authorize CASCADE drops; when true and
// dependents exist, the result's SafeToProceed remains true.
//
// The package-level signature works in terms of a raw DSN + flags so it
// remains reachable from both the Adapter (port-shaped) and any legacy
// caller. The Adapter method (AssessPostgresCascadeSafety) wraps this.
func AssessPostgresCascadeSafetyDSN(ctx context.Context, dsn, conflictStrategy string, allowCascade bool) (*PostgresCascadeReport, error) {
	if conflictStrategy != "replace" {
		return &PostgresCascadeReport{SafeToProceed: true}, nil
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection for cascade check: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to postgres for cascade check: %w", err)
	}

	rows, err := db.QueryContext(ctx, dependentObjectsQuery)
	if err != nil {
		return nil, fmt.Errorf("cascade safety query failed: %w", err)
	}
	defer rows.Close()

	var dependents []string
	for rows.Next() {
		var objName string
		if err := rows.Scan(&objName); err != nil {
			return nil, fmt.Errorf("cascade safety scan failed: %w", err)
		}
		dependents = append(dependents, objName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cascade safety rows error: %w", err)
	}

	if len(dependents) == 0 {
		return &PostgresCascadeReport{SafeToProceed: true}, nil
	}

	if !allowCascade {
		return &PostgresCascadeReport{
			SafeToProceed:    false,
			DependentObjects: dependents,
			BlockingReason: fmt.Sprintf(
				"replace strategy would CASCADE-drop %d dependent object(s); set allow_cascade: true to proceed",
				len(dependents),
			),
		}, nil
	}

	return &PostgresCascadeReport{
		SafeToProceed:    true,
		DependentObjects: dependents,
	}, nil
}

// PostgresCascadeReport is the package-level (DSN-shaped) twin of
// ports.CascadeSafetyResult. Kept as a separate type so the package surface
// doesn't depend on ports for its non-port API. The Adapter method bridges
// between the two.
type PostgresCascadeReport struct {
	SafeToProceed    bool
	DependentObjects []string
	BlockingReason   string
}

// dependentObjectsQuery finds objects in the target DB that depend on user
// tables. These would be implicitly dropped by DROP TABLE ... CASCADE.
const dependentObjectsQuery = `
SELECT DISTINCT
    dep_obj.relname || ' (' || dep_type.relkind || ')'
FROM pg_depend d
JOIN pg_class dep_obj ON d.objid = dep_obj.oid
JOIN pg_class dep_type ON d.refobjid = dep_type.oid
JOIN pg_namespace ns ON dep_type.relnamespace = ns.oid
WHERE dep_type.relkind = 'r'
  AND dep_obj.relkind IN ('v', 'm', 'i')
  AND ns.nspname NOT IN ('pg_catalog', 'information_schema')
  AND d.deptype = 'n'
LIMIT 100
`

// buildPostgresDSN composes a libpq-style DSN from connection parts. Lifted
// verbatim from internal/restore/postgres_conflicts.go (private to this
// package; callers use the Adapter method).
func buildPostgresDSN(host string, port int, user, dbname, password string) string {
	if port == 0 {
		port = 5432
	}
	dsn := fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=disable",
		host, port, user, dbname)
	if password != "" {
		dsn += fmt.Sprintf(" password=%s", password)
	}
	return dsn
}
