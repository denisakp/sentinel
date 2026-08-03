package tls_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/adapters/tls"
)

// generateSelfSignedCert creates a temporary self-signed cert PEM file.
// notBefore and notAfter control the validity window.
func generateSelfSignedCert(t *testing.T, notBefore, notAfter time.Time) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sentinel-test"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	f, err := os.CreateTemp("", "sentinel-cert-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })

	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("failed to write PEM: %v", err)
	}
	f.Close()

	return f.Name()
}

func TestLoadAndValidateCert(t *testing.T) {
	now := time.Now()

	validPath := generateSelfSignedCert(t, now.Add(-time.Hour), now.Add(24*time.Hour))
	expiredPath := generateSelfSignedCert(t, now.Add(-48*time.Hour), now.Add(-time.Hour))

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid cert",
			path:    validPath,
			wantErr: false,
		},
		{
			name:    "expired cert",
			path:    expiredPath,
			wantErr: true,
		},
		{
			name:    "missing file",
			path:    "/nonexistent/path/cert.pem",
			wantErr: true,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tls.LoadAndValidateCert(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadAndValidateCert(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestLoadAndValidateCert_NoPEMData(t *testing.T) {
	f, err := os.CreateTemp("", "not-a-cert-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString("this is not a PEM certificate")
	f.Close()

	err = tls.LoadAndValidateCert(f.Name())
	if err == nil {
		t.Error("expected error for non-PEM content, got nil")
	}
}

func TestValidateCerts(t *testing.T) {
	now := time.Now()
	validPath := generateSelfSignedCert(t, now.Add(-time.Hour), now.Add(24*time.Hour))

	tests := []struct {
		name    string
		cfg     *ports.Config
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: false,
		},
		{
			name:    "disabled",
			cfg:     &ports.Config{Enabled: false, CACertPath: "/nonexistent"},
			wantErr: false,
		},
		{
			name:    "valid ca cert",
			cfg:     &ports.Config{Enabled: true, CACertPath: validPath},
			wantErr: false,
		},
		{
			name:    "missing ca cert",
			cfg:     &ports.Config{Enabled: true, CACertPath: "/does/not/exist.crt"},
			wantErr: true,
		},
		{
			name:    "valid client cert",
			cfg:     &ports.Config{Enabled: true, ClientCert: validPath},
			wantErr: false,
		},
		{
			name:    "invalid client cert",
			cfg:     &ports.Config{Enabled: true, ClientCert: "/does/not/exist.crt"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tls.ValidateCerts(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCerts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
