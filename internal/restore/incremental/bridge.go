// TODO(spec-038): remove
//
// Spec 037 Sub-PR E introduced this bridge file. Pure chain-resolution +
// assembly + fallback logic relocated to
// internal/domain/restore/incremental/. The legacy `assembler.go` in this
// package owns I/O (os, pgcombine) and stays as a driving helper for
// internal/restore/executor.go.
package incremental

import (
	domainincr "github.com/denisakp/sentinel/internal/domain/restore/incremental"
)

// Re-exports of pure domain types + functions. NO function bodies in this
// file — see specs/037-domain-extraction/research.md R4.

type (
	ChainArtifact    = domainincr.ChainArtifact
	ResolvedChain    = domainincr.ResolvedChain
	FallbackDecision = domainincr.FallbackDecision
)

var (
	ResolveOrderedChain = domainincr.ResolveOrderedChain
	AssembleChain       = domainincr.AssembleChain
	EvaluateFallback    = domainincr.EvaluateFallback
)
