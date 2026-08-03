package chainassemblertesting_test

import (
	"context"
	"errors"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/chainassemblertesting"
)

// Compile-time port assertion.
var _ ports.ChainAssembler = (*chainassemblertesting.MockAssembler)(nil)

func TestMockAssemblerRecordsCallsAndEchoesFirstSource(t *testing.T) {
	m := chainassemblertesting.NewMockAssembler()
	got, err := m.AssemblePostgresChain(context.Background(), "/stage", []string{"/stage/base", "/stage/incr1"}, "")
	if err != nil {
		t.Fatalf("AssemblePostgresChain() error = %v", err)
	}
	if got != "/stage/base" {
		t.Fatalf("result = %q, want /stage/base", got)
	}
	calls := m.Calls()
	if len(calls) != 1 || calls[0].StagingDir != "/stage" || len(calls[0].StagedSources) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestMockAssemblerSeededError(t *testing.T) {
	m := chainassemblertesting.NewMockAssembler()
	m.SeedResult("", errors.New("boom"))
	if _, err := m.AssemblePostgresChain(context.Background(), "/stage", []string{"a", "b"}, ""); err == nil {
		t.Fatal("expected seeded error")
	}
}
