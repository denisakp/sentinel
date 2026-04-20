package restore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/denisakp/sentinel/internal/config"
	_ "github.com/lib/pq"
)

// CascadeSafetyResult reports whether a PostgreSQL replace restore is safe
// to execute with DROP ... CASCADE.
type CascadeSafetyResult struct {
	Safe             bool
	DependentObjects []string
	BlockingReason   string
}

// AssessPostgresCascadeSafety inspects the target database for objects that
// depend on tables present in the staged backup. If dependent objects exist
// and AllowCascade is false, the restore should be blocked.
//
// This function only runs when ConflictStrategy="replace" for a postgres job.
// It returns a result describing whether cascading drops are safe to proceed.
func AssessPostgresCascadeSafety(ctx context.Context, job config.RestoreJob, password string) (*CascadeSafetyResult, error) {
	if job.Type != "postgres" {
		return nil, fmt.Errorf("cascade safety check is only applicable to postgres restores")
	}
	if job.ConflictStrategy != "replace" {
		return &CascadeSafetyResult{Safe: true}, nil
	}

	dsn := buildPostgresDSN(job, password)
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
		return &CascadeSafetyResult{Safe: true}, nil
	}

	if !job.AllowCascade {
		return &CascadeSafetyResult{
			Safe:             false,
			DependentObjects: dependents,
			BlockingReason:   fmt.Sprintf("replace strategy would CASCADE-drop %d dependent object(s); set allow_cascade: true to proceed", len(dependents)),
		}, nil
	}

	return &CascadeSafetyResult{
		Safe:             true,
		DependentObjects: dependents,
	}, nil
}

// dependentObjectsQuery finds objects in the target DB that depend on user tables.
// These would be implicitly dropped by DROP TABLE ... CASCADE.
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

func buildPostgresDSN(job config.RestoreJob, password string) string {
	host := job.Host
	port := job.Port
	if port == 0 {
		port = 5432
	}
	dsn := fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=disable",
		host, port, job.Username, job.Database)
	if password != "" {
		dsn += fmt.Sprintf(" password=%s", password)
	}
	return dsn
}
