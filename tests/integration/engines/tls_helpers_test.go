//go:build integration

package engines

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// CABundle holds a self-signed CA certificate and private key with convenience
// helpers for signing server and client certificates.
type CABundle struct {
	CertPEM  []byte
	KeyPEM   []byte
	CertFile string // path to PEM-encoded CA certificate on disk
	cert     *x509.Certificate
	key      *ecdsa.PrivateKey
}

// ServerBundle holds a signed server certificate and private key along with the
// path to the CA certificate that signed it.
type ServerBundle struct {
	CertPEM  []byte
	KeyPEM   []byte
	CertFile string // path to PEM-encoded server certificate
	KeyFile  string // path to PEM-encoded server private key
}

// ClientBundle holds a signed client certificate and private key.
type ClientBundle struct {
	CertPEM  []byte
	KeyPEM   []byte
	CertFile string // path to PEM-encoded client certificate
	KeyFile  string // path to PEM-encoded client private key
}

// GenerateSelfSignedCA creates an in-memory ECDSA P-256 CA certificate and
// writes a PEM-encoded copy to t.TempDir(). Cleanup is registered via t.Cleanup.
func GenerateSelfSignedCA(t *testing.T) *CABundle {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: generate serial: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Sentinel Test CA",
			Organization: []string{"Sentinel Integration Tests"},
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: parse certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	dir := t.TempDir()
	certFile := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("GenerateSelfSignedCA: write ca.crt: %v", err)
	}

	return &CABundle{
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		CertFile: certFile,
		cert:     cert,
		key:      key,
	}
}

// GenerateSignedServerCert creates a server certificate signed by the given CA.
// dnsNames are added as Subject Alternative Names; "localhost" is always included.
// Writes cert and key PEM files to t.TempDir(). Cleanup via t.Cleanup.
func GenerateSignedServerCert(t *testing.T, ca *CABundle, dnsNames []string) *ServerBundle {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateSignedServerCert: generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("GenerateSignedServerCert: generate serial: %v", err)
	}

	sans := append([]string{"localhost"}, dnsNames...)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Sentinel Test Server",
			Organization: []string{"Sentinel Integration Tests"},
		},
		DNSNames:    sans,
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("GenerateSignedServerCert: create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("GenerateSignedServerCert: marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("GenerateSignedServerCert: write server.crt: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("GenerateSignedServerCert: write server.key: %v", err)
	}

	return &ServerBundle{
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		CertFile: certFile,
		KeyFile:  keyFile,
	}
}

// GenerateSignedClientCert creates a client certificate signed by the given CA.
// Writes cert and key PEM files to t.TempDir(). Cleanup via t.Cleanup.
func GenerateSignedClientCert(t *testing.T, ca *CABundle) *ClientBundle {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateSignedClientCert: generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("GenerateSignedClientCert: generate serial: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Sentinel Test Client",
			Organization: []string{"Sentinel Integration Tests"},
		},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("GenerateSignedClientCert: create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("GenerateSignedClientCert: marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	dir := t.TempDir()
	certFile := filepath.Join(dir, "client.crt")
	keyFile := filepath.Join(dir, "client.key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("GenerateSignedClientCert: write client.crt: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("GenerateSignedClientCert: write client.key: %v", err)
	}

	return &ClientBundle{
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		CertFile: certFile,
		KeyFile:  keyFile,
	}
}

// GenerateExpiredCA creates a self-signed CA that is already expired.
// Used for TestTLS_ExpiredCert_Aborts to verify that expired certs fail validation.
func GenerateExpiredCA(t *testing.T) *CABundle {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateExpiredCA: generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("GenerateExpiredCA: generate serial: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "Sentinel Expired CA",
		},
		// Already expired
		NotBefore:             time.Now().Add(-48 * time.Hour),
		NotAfter:              time.Now().Add(-24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("GenerateExpiredCA: create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("GenerateExpiredCA: parse certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("GenerateExpiredCA: marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	dir := t.TempDir()
	certFile := filepath.Join(dir, "expired-ca.crt")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("GenerateExpiredCA: write expired-ca.crt: %v", err)
	}

	return &CABundle{
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		CertFile: certFile,
		cert:     cert,
		key:      key,
	}
}
