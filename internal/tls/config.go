// Package tls provides TLS configuration validation and engine-specific
// argument building for Sentinel's database backup/restore operations.
package tls

import (
	"fmt"
	"slices"
)

// ValidModes lists the supported TLS connection modes.
var ValidModes = []string{"require", "verify-ca", "verify-full", "prefer"}

// Config holds TLS settings for a single database connection.
// It mirrors the config.TLSConfig YAML struct but is an independent
// domain type to avoid coupling the tls package to config parsing.
type Config struct {
	Enabled    bool
	Mode       string // require | verify-ca | verify-full | prefer
	CACertPath string
	ClientCert string
	ClientKey  string
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

	return nil
}

// BuildTLSArgs returns the engine-specific CLI arguments needed to enable TLS.
// Returns an empty slice when TLS is disabled or nil.
func BuildTLSArgs(engine string, c *Config) []string {
	if c == nil || !c.Enabled {
		return nil
	}

	mode := c.Mode
	if mode == "" {
		mode = "prefer"
	}

	switch engine {
	case "postgres":
		return buildPostgresTLSArgs(mode, c)
	case "mysql":
		return buildMySQLTLSArgs(mode, c)
	case "mariadb":
		return buildMariaDBTLSArgs(mode, c)
	case "mongodb":
		return buildMongoDBTLSArgs(mode, c)
	default:
		return nil
	}
}

func buildPostgresTLSArgs(mode string, c *Config) []string {
	// PostgreSQL accepts sslmode as a connection string parameter
	args := []string{fmt.Sprintf("--sslmode=%s", pgSSLMode(mode))}
	if c.CACertPath != "" {
		args = append(args, fmt.Sprintf("--sslrootcert=%s", c.CACertPath))
	}
	if c.ClientCert != "" {
		args = append(args, fmt.Sprintf("--sslcert=%s", c.ClientCert))
	}
	if c.ClientKey != "" {
		args = append(args, fmt.Sprintf("--sslkey=%s", c.ClientKey))
	}
	return args
}

func buildMySQLTLSArgs(mode string, c *Config) []string {
	args := []string{fmt.Sprintf("--ssl-mode=%s", mysqlSSLMode(mode))}
	if c.CACertPath != "" {
		args = append(args, fmt.Sprintf("--ssl-ca=%s", c.CACertPath))
	}
	if c.ClientCert != "" {
		args = append(args, fmt.Sprintf("--ssl-cert=%s", c.ClientCert))
	}
	if c.ClientKey != "" {
		args = append(args, fmt.Sprintf("--ssl-key=%s", c.ClientKey))
	}
	return args
}

func buildMariaDBTLSArgs(mode string, c *Config) []string {
	args := []string{"--ssl"}
	if c.CACertPath != "" {
		args = append(args, fmt.Sprintf("--ssl-ca=%s", c.CACertPath))
	}
	if c.ClientCert != "" {
		args = append(args, fmt.Sprintf("--ssl-cert=%s", c.ClientCert))
	}
	if mode == "verify-full" {
		args = append(args, "--ssl-verify-server-cert")
	}
	return args
}

func buildMongoDBTLSArgs(mode string, c *Config) []string {
	if mode == "prefer" {
		// MongoDB does not have a native "prefer" mode via CLI flags;
		// omit TLS flags and let the server negotiate.
		return nil
	}
	args := []string{"--tls"}
	if c.CACertPath != "" {
		args = append(args, fmt.Sprintf("--tlsCAFile=%s", c.CACertPath))
	}
	if c.ClientCert != "" {
		// MongoDB expects cert+key combined in one PEM file; pass the cert path
		args = append(args, fmt.Sprintf("--tlsCertificateKeyFile=%s", c.ClientCert))
	}
	if mode == "require" {
		// require means TLS on but skip cert validation
		args = append(args, "--tlsInsecure")
	}
	return args
}

// pgSSLMode maps generic mode names to PostgreSQL sslmode values.
func pgSSLMode(mode string) string {
	switch mode {
	case "require":
		return "require"
	case "verify-ca":
		return "verify-ca"
	case "verify-full":
		return "verify-full"
	default: // prefer
		return "prefer"
	}
}

// mysqlSSLMode maps generic mode names to MySQL --ssl-mode values.
func mysqlSSLMode(mode string) string {
	switch mode {
	case "require":
		return "REQUIRED"
	case "verify-ca":
		return "VERIFY_CA"
	case "verify-full":
		return "VERIFY_IDENTITY"
	default: // prefer
		return "PREFERRED"
	}
}

func isValidMode(mode string) bool {
	return slices.Contains(ValidModes, mode)
}
