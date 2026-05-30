package db_probe

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// Adapter satisfies ports.DBProber. Construct via NewAdapter().
//
// The package also exposes function-shaped helpers (PingSqlDatabase,
// ListDatabases, CheckConnectivity, AssessPostgresCascadeSafetyDSN) reachable
// without the Adapter; those preserve compatibility for legacy callers
// (notably the dump engine adapters that previously imported
// internal/backup/sql).
type Adapter struct{}

// NewAdapter constructs a zero-config DBProber. The adapter holds no state;
// every method opens a fresh connection.
func NewAdapter() *Adapter { return &Adapter{} }

// Ping implements ports.DBProber.
func (a *Adapter) Ping(ctx context.Context, conn ports.DatabaseConfig) error {
	_ = ctx // ping helpers use blocking sql.Open; ctx-aware variant is future work
	ok, err := CheckConnectivity(conn.Type, conn.Host, portStr(conn.Port), conn.Username, conn.Password, "")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("db_probe: ping reported unhealthy without error")
	}
	return nil
}

// ListDatabases implements ports.DBProber.
func (a *Adapter) ListDatabases(_ context.Context, conn ports.DatabaseConfig) ([]string, error) {
	return ListDatabases(conn.Type, conn.Host, portStr(conn.Port), conn.Username, conn.Password)
}

// AssessPostgresCascadeSafety implements ports.DBProber. Non-Postgres engines
// return ports.ErrUnsupportedEngine.
func (a *Adapter) AssessPostgresCascadeSafety(ctx context.Context, conn ports.DatabaseConfig, target ports.CascadeTarget) (*ports.CascadeSafetyResult, error) {
	if conn.Type != "postgres" {
		return nil, ports.ErrUnsupportedEngine
	}
	dsn := buildPostgresDSN(conn.Host, conn.Port, conn.Username, "" /* dbname unknown via DatabaseConfig */, conn.Password)
	report, err := AssessPostgresCascadeSafetyDSN(ctx, dsn, target.ConflictStrategy, target.AllowCascade)
	if err != nil {
		return nil, err
	}
	return &ports.CascadeSafetyResult{
		SafeToProceed:    report.SafeToProceed,
		DependentObjects: report.DependentObjects,
		BlockingReason:   report.BlockingReason,
	}, nil
}

func portStr(p int) string {
	if p == 0 {
		return ""
	}
	return fmt.Sprintf("%d", p)
}
