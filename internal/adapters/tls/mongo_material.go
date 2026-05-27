package tls

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/youmark/pkcs8"

	"github.com/denisakp/sentinel/internal/ports"
)

// MongoTLSMaterial holds the on-disk material handed to mongodump/mongorestore.
// When the operator supplied a single combined PEM, Path points at the original
// file and temp is false. When the operator supplied separate cert+key files (or
// an encrypted key requiring decryption), Path points at an ephemeral combined
// PEM created in os.TempDir() with mode 0600; Close() removes it.
type MongoTLSMaterial struct {
	Path  string
	temp  bool
	pid   int
	jobID string
}

// PrepareMongoTLS prepares mTLS material for a single Mongo job and returns the
// CLI args to splice into mongodump/mongorestore. Callers MUST defer
// material.Close() on the non-nil return.
//
// Returns (nil, nil, nil) when TLS is disabled, cfg is nil, or mode is "prefer".
func PrepareMongoTLS(cfg *ports.Config, jobID string) (*MongoTLSMaterial, []string, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil, nil
	}
	mode := cfg.Mode
	if mode == "" {
		mode = "prefer"
	}
	if mode == "prefer" {
		// Existing behaviour: no flags emitted; let the server negotiate.
		return nil, nil, nil
	}

	// Pair-shape pre-flight.
	if cfg.ClientCert == "" && cfg.ClientKey != "" {
		return nil, nil, fmt.Errorf("tls.client_cert and tls.client_key must both be set for mutual TLS")
	}
	if cfg.ClientKeyPasswordEnv != "" && cfg.ClientKey == "" {
		return nil, nil, fmt.Errorf("tls.client_key must be set when tls.client_key_password_env is configured")
	}
	// Note: cfg.ClientCert set with empty ClientKey is allowed iff the cert file
	// is a combined PEM (cert + key). That case is detected inside
	// prepareMongoClientMaterial. Plain-cert-without-key is rejected there too.

	args := []string{"--tls"}
	if cfg.CACertPath != "" {
		args = append(args, fmt.Sprintf("--tlsCAFile=%s", cfg.CACertPath))
	}

	// No client material configured: emit just CA + mode flag.
	if cfg.ClientCert == "" {
		if mode == "require" {
			args = append(args, "--tlsInsecure")
		}
		return nil, args, nil
	}

	material, err := prepareMongoClientMaterial(cfg, jobID)
	if err != nil {
		return nil, nil, err
	}
	args = append(args, fmt.Sprintf("--tlsCertificateKeyFile=%s", material.Path))
	if mode == "require" {
		args = append(args, "--tlsInsecure")
	}
	return material, args, nil
}

// prepareMongoClientMaterial loads the configured cert/key, optionally decrypts
// the key, and returns a *MongoTLSMaterial. When the cert file is already
// combined and ClientKey is unset, no temp file is created.
func prepareMongoClientMaterial(cfg *ports.Config, jobID string) (*MongoTLSMaterial, error) {
	combined, err := isCombinedPEM(cfg.ClientCert)
	if err != nil {
		return nil, fmt.Errorf("read tls.client_cert %q: %w", cfg.ClientCert, err)
	}

	switch {
	case combined && cfg.ClientKey != "":
		return nil, fmt.Errorf("tls.client_cert already contains a private key; tls.client_key must be unset")
	case combined && cfg.ClientKey == "":
		return &MongoTLSMaterial{Path: cfg.ClientCert, temp: false, pid: os.Getpid(), jobID: jobID}, nil
	case !combined && cfg.ClientKey == "":
		return nil, fmt.Errorf("tls.client_cert and tls.client_key must both be set for mutual TLS")
	}

	// Separate cert + key path.
	certBytes, err := os.ReadFile(cfg.ClientCert)
	if err != nil {
		return nil, fmt.Errorf("read tls.client_cert %q: %w", cfg.ClientCert, err)
	}
	if len(bytes.TrimSpace(certBytes)) == 0 {
		return nil, fmt.Errorf("read tls.client_cert %q: file is empty", cfg.ClientCert)
	}

	keyBytes, err := os.ReadFile(cfg.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("read tls.client_key %q: %w", cfg.ClientKey, err)
	}
	if len(bytes.TrimSpace(keyBytes)) == 0 {
		return nil, fmt.Errorf("read tls.client_key %q: file is empty", cfg.ClientKey)
	}

	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return nil, fmt.Errorf("parse tls.client_key %q: no PEM block found", cfg.ClientKey)
	}

	keyEncrypted := isEncryptedKeyBlock(keyBlock)
	if keyEncrypted {
		if cfg.ClientKeyPasswordEnv == "" {
			return nil, fmt.Errorf("tls.client_key is encrypted; set tls.client_key_password_env to name an env var holding the passphrase")
		}
		passphrase := os.Getenv(cfg.ClientKeyPasswordEnv)
		if passphrase == "" {
			return nil, fmt.Errorf("passphrase env var %q is unset or empty (referenced by tls.client_key_password_env)", cfg.ClientKeyPasswordEnv)
		}
		decryptedBlock, derr := decryptKeyBlock(keyBlock, passphrase)
		if derr != nil {
			return nil, fmt.Errorf("decrypt tls.client_key %q: %w", cfg.ClientKey, derr)
		}
		keyBytes = pem.EncodeToMemory(decryptedBlock)
	}

	combinedBuf := bytes.Buffer{}
	combinedBuf.Write(bytes.TrimRight(certBytes, "\n"))
	combinedBuf.WriteByte('\n')
	combinedBuf.Write(keyBytes)

	pattern := fmt.Sprintf("sentinel-mongo-tls-%d-*.pem", os.Getpid())
	f, err := os.CreateTemp(os.TempDir(), pattern)
	if err != nil {
		return nil, fmt.Errorf("create combined-material temp file in %q: %w", os.TempDir(), err)
	}
	tmpPath := f.Name()
	// Defence in depth: enforce 0600 regardless of umask.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("chmod combined-material temp file %q: %w", tmpPath, err)
	}
	if _, err := f.Write(combinedBuf.Bytes()); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("write combined-material temp file %q: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close combined-material temp file %q: %w", tmpPath, err)
	}

	m := &MongoTLSMaterial{Path: tmpPath, temp: true, pid: os.Getpid(), jobID: jobID}
	slog.Info("mongo-tls: prepared combined material",
		"event", "mongo_tls_prepared",
		"job", jobID,
		"path", tmpPath)
	Register(m)
	return m, nil
}

// Close removes the ephemeral combined-material file (if any). Idempotent.
// Safe to call from a signal handler.
func (m *MongoTLSMaterial) Close() error {
	if m == nil || !m.temp || m.Path == "" {
		return nil
	}
	path := m.Path
	m.Path = ""
	Unregister(m)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		slog.Warn("mongo-tls: failed to remove combined material",
			"event", "mongo_tls_cleanup_failed",
			"job", m.jobID,
			"path", path,
			"error", err.Error())
		return fmt.Errorf("remove combined mongo tls material %q: %w", path, err)
	}
	slog.Info("mongo-tls: removed combined material",
		"event", "mongo_tls_removed",
		"job", m.jobID,
		"path", path)
	return nil
}

// isCombinedPEM reports whether the file at path contains both at least one
// CERTIFICATE block and at least one private-key block.
func isCombinedPEM(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	rest := data
	var hasCert, hasKey bool
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		switch blk.Type {
		case "CERTIFICATE":
			hasCert = true
		case "PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY", "ENCRYPTED PRIVATE KEY":
			hasKey = true
		}
		if hasCert && hasKey {
			return true, nil
		}
	}
	return false, nil
}

func isEncryptedKeyBlock(b *pem.Block) bool {
	if b == nil {
		return false
	}
	if b.Type == "ENCRYPTED PRIVATE KEY" {
		return true
	}
	// Legacy PKCS#1 encrypted form carries a DEK-Info header.
	if _, ok := b.Headers["DEK-Info"]; ok {
		return true
	}
	return false
}

// decryptKeyBlock returns an unencrypted PEM block (PRIVATE KEY or RSA PRIVATE KEY)
// derived from an encrypted input block, using the supplied passphrase.
func decryptKeyBlock(b *pem.Block, passphrase string) (*pem.Block, error) {
	switch {
	case b.Type == "ENCRYPTED PRIVATE KEY":
		key, err := pkcs8.ParsePKCS8PrivateKey(b.Bytes, []byte(passphrase))
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		return &pem.Block{Type: "PRIVATE KEY", Bytes: der}, nil
	case b.Headers["DEK-Info"] != "":
		// nolint:staticcheck // DecryptPEMBlock is the only stdlib API for the legacy PKCS#1 shape.
		der, err := x509.DecryptPEMBlock(b, []byte(passphrase))
		if err != nil {
			return nil, err
		}
		out := &pem.Block{Type: b.Type, Bytes: der}
		// Strip the DEK-Info / Proc-Type headers from the output.
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported encrypted key block type %q", b.Type)
	}
}

// materialFilenamePattern is exported for the sweep's use.
const materialFilenamePrefix = "sentinel-mongo-tls-"

// materialGlob returns the glob used to discover (potentially orphan) material files.
func materialGlob(dir string) string {
	return filepath.Join(dir, materialFilenamePrefix+"*-*.pem")
}
