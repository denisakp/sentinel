package monitor

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time assertion that *Monitor satisfies ports.Recorder.
// Mirrors the pattern established by specs 029 (storage), 030 (crypto),
// and 031 (lock). See ADR 0001.
var _ ports.Recorder = (*Monitor)(nil)
