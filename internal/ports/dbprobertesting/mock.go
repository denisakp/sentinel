package dbprobertesting

import (
	"context"
	"sync"

	"github.com/denisakp/sentinel/internal/ports"
)

// MockProber is a recording in-memory implementation of ports.DBProber. It
// is safe for concurrent use. Callers seed return values via the
// Set/SeedDatabases/SeedCascade helpers and inspect call history via the
// Calls* methods.
type MockProber struct {
	mu sync.Mutex

	pingErr      error
	listValues   []string
	listErr      error
	cascade      *ports.CascadeSafetyResult
	cascadeErr   error

	pingCalls    []DatabaseCall
	listCalls    []DatabaseCall
	cascadeCalls []CascadeCall
}

// DatabaseCall records one Ping or ListDatabases invocation.
type DatabaseCall struct {
	Conn ports.DatabaseConfig
}

// CascadeCall records one AssessPostgresCascadeSafety invocation.
type CascadeCall struct {
	Conn   ports.DatabaseConfig
	Target ports.CascadeTarget
}

// NewMockProber constructs a MockProber with success defaults: Ping returns
// nil, ListDatabases returns nil, AssessPostgresCascadeSafety returns
// SafeToProceed=true.
func NewMockProber() *MockProber {
	return &MockProber{
		cascade: &ports.CascadeSafetyResult{SafeToProceed: true},
	}
}

// SetPingErr configures the error returned by subsequent Ping calls.
func (m *MockProber) SetPingErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingErr = err
}

// SeedDatabases configures the slice returned by subsequent ListDatabases
// calls. Pass err to override the default nil error.
func (m *MockProber) SeedDatabases(names []string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listValues = names
	m.listErr = err
}

// SeedCascade configures the result returned by subsequent
// AssessPostgresCascadeSafety calls.
func (m *MockProber) SeedCascade(result *ports.CascadeSafetyResult, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cascade = result
	m.cascadeErr = err
}

func (m *MockProber) Ping(_ context.Context, conn ports.DatabaseConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingCalls = append(m.pingCalls, DatabaseCall{Conn: conn})
	return m.pingErr
}

func (m *MockProber) ListDatabases(_ context.Context, conn ports.DatabaseConfig) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls = append(m.listCalls, DatabaseCall{Conn: conn})
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]string, len(m.listValues))
	copy(out, m.listValues)
	return out, nil
}

func (m *MockProber) AssessPostgresCascadeSafety(_ context.Context, conn ports.DatabaseConfig, target ports.CascadeTarget) (*ports.CascadeSafetyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cascadeCalls = append(m.cascadeCalls, CascadeCall{Conn: conn, Target: target})
	if m.cascadeErr != nil {
		return nil, m.cascadeErr
	}
	if m.cascade == nil {
		return &ports.CascadeSafetyResult{SafeToProceed: true}, nil
	}
	clone := *m.cascade
	if m.cascade.DependentObjects != nil {
		clone.DependentObjects = append([]string(nil), m.cascade.DependentObjects...)
	}
	return &clone, nil
}

// PingCalls returns a snapshot of Ping invocations.
func (m *MockProber) PingCalls() []DatabaseCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DatabaseCall(nil), m.pingCalls...)
}

// ListCalls returns a snapshot of ListDatabases invocations.
func (m *MockProber) ListCalls() []DatabaseCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DatabaseCall(nil), m.listCalls...)
}

// CascadeCalls returns a snapshot of AssessPostgresCascadeSafety invocations.
func (m *MockProber) CascadeCalls() []CascadeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]CascadeCall(nil), m.cascadeCalls...)
}
