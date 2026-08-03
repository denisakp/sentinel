package db_probe

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time port assertion.
var _ ports.DBProber = (*Adapter)(nil)
