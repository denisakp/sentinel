package tls

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertion that Adapter satisfies ports.Prober.
// Mirrors the pattern established by specs 029 (storage), 030 (crypto),
// 031 (lock), and 032 (monitor). See ADR 0001.
var _ ports.Prober = Adapter{}
