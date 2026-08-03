// Package chainassemblertesting provides an in-memory recording fake for
// ports.ChainAssembler, reachable from domain test code without importing
// any adapter. Pattern mirror of dbprobertesting.MockProber.
package chainassemblertesting

import (
	"context"
	"sync"
)

// AssembleCall records one AssemblePostgresChain invocation.
type AssembleCall struct {
	StagingDir    string
	StagedSources []string
	ToolsPath     string
}

// MockAssembler is a recording in-memory implementation of
// ports.ChainAssembler. Safe for concurrent use.
type MockAssembler struct {
	mu sync.Mutex

	result string
	err    error

	calls []AssembleCall
}

// NewMockAssembler constructs a MockAssembler with success defaults: the
// assembled path echoes the first staged source.
func NewMockAssembler() *MockAssembler {
	return &MockAssembler{}
}

// SeedResult configures the (path, err) returned by subsequent calls. An
// empty path with nil err echoes the first staged source.
func (m *MockAssembler) SeedResult(path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.result = path
	m.err = err
}

// AssemblePostgresChain implements ports.ChainAssembler.
func (m *MockAssembler) AssemblePostgresChain(_ context.Context, stagingDir string, stagedSources []string, toolsPath string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, AssembleCall{
		StagingDir:    stagingDir,
		StagedSources: append([]string(nil), stagedSources...),
		ToolsPath:     toolsPath,
	})
	if m.err != nil {
		return "", m.err
	}
	if m.result != "" {
		return m.result, nil
	}
	if len(stagedSources) > 0 {
		return stagedSources[0], nil
	}
	return "", nil
}

// Calls returns a copy of the recorded invocations.
func (m *MockAssembler) Calls() []AssembleCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]AssembleCall(nil), m.calls...)
}
