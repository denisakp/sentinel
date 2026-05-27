package tls

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

// LoadAndValidateCert reads a PEM certificate file, parses it, and returns an
// error if the file is unreadable, contains no valid certificate, or if the
// first certificate in the chain has already expired.
func LoadAndValidateCert(path string) error {
	if path == "" {
		return fmt.Errorf("certificate path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read certificate file %q: %w", path, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("certificate file %q contains no PEM-encoded data", path)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate %q: %w", path, err)
	}

	if time.Now().After(cert.NotAfter) {
		return fmt.Errorf(
			"certificate %q expired on %s",
			path, cert.NotAfter.Format(time.RFC3339),
		)
	}

	return nil
}

// ValidateCerts validates all certificate files referenced in c.
// It is called as a pre-flight check before backup/restore operations.
func ValidateCerts(c *ports.Config) error {
	if c == nil || !c.Enabled {
		return nil
	}

	if c.CACertPath != "" {
		if err := LoadAndValidateCert(c.CACertPath); err != nil {
			return fmt.Errorf("ca_cert: %w", err)
		}
	}
	if c.ClientCert != "" {
		if err := LoadAndValidateCert(c.ClientCert); err != nil {
			return fmt.Errorf("client_cert: %w", err)
		}
	}

	return nil
}
