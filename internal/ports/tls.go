package ports

import (
	"context"
	"fmt"
	"slices"
)

// ValidModes lists the supported TLS connection modes.
var ValidModes = []string{"require", "verify-ca", "verify-full", "prefer"}

// Config holds TLS settings for a single database connection.
//
// Relocated from internal/adapters/tls/config.go (single source of truth per
// spec 028 FR-003a; adapter relocation completed in spec 033). It mirrors the config.TLSConfig YAML struct but is an
// independent domain type to avoid coupling the tls package to config
// parsing.
type Config struct {
	Enabled              bool
	Mode                 string // require | verify-ca | verify-full | prefer
	CACertPath           string
	ClientCert           string
	ClientKey            string
	ClientKeyPasswordEnv string // env-var name holding passphrase for an encrypted ClientKey
}

// Validate checks that the TLS configuration is internally consistent.
// It returns a descriptive error for each invalid combination.
func (c *Config) Validate() error {
	if c == nil || !c.Enabled {
		return nil
	}

	mode := c.Mode
	if mode == "" {
		mode = "prefer"
	}

	if !isValidMode(mode) {
		return fmt.Errorf(
			"invalid tls.mode %q: must be one of require, verify-ca, verify-full, prefer",
			mode,
		)
	}

	// verify-ca and verify-full require a CA cert
	if (mode == "verify-ca" || mode == "verify-full") && c.CACertPath == "" {
		return fmt.Errorf("tls.ca_cert is required when mode is %q", mode)
	}

	// client cert and key must be provided together
	if (c.ClientCert != "") != (c.ClientKey != "") {
		return fmt.Errorf("tls.client_cert and tls.client_key must both be set for mutual TLS")
	}

	// passphrase env var requires a client key
	if c.ClientKeyPasswordEnv != "" && c.ClientKey == "" {
		return fmt.Errorf("tls.client_key must be set when tls.client_key_password_env is configured")
	}

	return nil
}

func isValidMode(mode string) bool {
	return slices.Contains(ValidModes, mode)
}

// ProbeResult is returned by a TLS connection probe.
type ProbeResult struct {
	// TLSActive is true if the database connection uses TLS.
	TLSActive bool

	// FallbackOccurred is true when mode is "prefer" but TLS is not available.
	FallbackOccurred bool
}

// DatabaseConfig carries the minimal connection parameters needed for probing.
type DatabaseConfig struct {
	Type     string // postgres | mysql | mariadb | mongodb
	Host     string
	Port     int
	Username string
	Password string
	TLS      *Config
	Database string // spec 043: target database for a database-scoped ping (SQL dump path); "" = server-level
	URI      string // spec 043: connection URI for the mongodb ping
}

// Prober abstracts the TLS connection probe (current concrete implementation:
// internal/adapters/tls.Adapter, which delegates to the package-level
// ProbeTLSConnection helper).
type Prober interface {
	Probe(ctx context.Context, cfg DatabaseConfig) (ProbeResult, error)
}
