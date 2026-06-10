package tls

import (
	"context"

	"github.com/denisakp/sentinel/internal/ports"
)

// Adapter is a zero-state wrapper that exposes ProbeTLSConnection as a method
// so it can satisfy ports.Prober. Introduced by spec 028 (FR-004 clause c)
// because the port interface needs a method receiver to be a conformance
// target.
type Adapter struct{}

// Probe satisfies ports.Prober by delegating to the package-level
// ProbeTLSConnection function. No additional logic.
func (Adapter) Probe(ctx context.Context, cfg ports.DatabaseConfig) (ports.ProbeResult, error) {
	return ProbeTLSConnection(ctx, cfg)
}
