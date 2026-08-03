package chain_assembler

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertion that *Adapter satisfies ports.ChainAssembler.
var _ ports.ChainAssembler = (*Adapter)(nil)
