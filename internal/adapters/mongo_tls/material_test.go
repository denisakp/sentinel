package mongo_tls

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/youmark/pkcs8"

	"github.com/denisakp/sentinel/internal/ports"
)

// --- fixtures --------------------------------------------------------------

type pemPair struct {
	certPath string
	keyPath  string
	caPath   string
	// raw bytes for combined-PEM construction
	certPEM []byte
	keyPEM  []byte
}

func genPEMPair(t *testing.T) pemPair {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sentinel-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("key marshal: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	caPath := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return pemPair{certPath: certPath, keyPath: keyPath, caPath: caPath, certPEM: certPEM, keyPEM: keyPEM}
}

func combinedFile(t *testing.T, p pemPair) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "combined.pem")
	buf := append(bytes.TrimRight(p.certPEM, "\n"), '\n')
	buf = append(buf, p.keyPEM...)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func encryptedPKCS8File(t *testing.T, p pemPair, passphrase string) string {
	t.Helper()
	// Re-parse the unencrypted key and re-encrypt with passphrase.
	block, _ := pem.Decode(p.keyPEM)
	if block == nil {
		t.Fatal("decode key")
	}
	priv, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	encDER, err := pkcs8.MarshalPrivateKey(priv, []byte(passphrase), nil)
	if err != nil {
		t.Fatal(err)
	}
	encPEM := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: encDER})
	path := filepath.Join(t.TempDir(), "client.enc.key")
	if err := os.WriteFile(path, encPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// snapshotMaterialFiles returns the set of currently-existing material files in
// os.TempDir() so tests can assert no leaks.
func snapshotMaterialFiles(t *testing.T) map[string]struct{} {
	t.Helper()
	out := map[string]struct{}{}
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "sentinel-mongo-tls-*-*.pem"))
	for _, m := range matches {
		out[m] = struct{}{}
	}
	return out
}

// --- T011: separate cert + key happy path -----------------------------------

func TestPrepareMongoTLS_SeparateCertKey_Plain(t *testing.T) {
	p := genPEMPair(t)
	cfg := &ports.Config{
		Enabled:    true,
		Mode:       "verify-full",
		CACertPath: p.caPath,
		ClientCert: p.certPath,
		ClientKey:  p.keyPath,
	}
	pre := snapshotMaterialFiles(t)

	material, args, err := PrepareMongoTLS(cfg, "job-1")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	t.Cleanup(func() { _ = material.Close() })

	// (a) args contain --tls, --tlsCAFile=, --tlsCertificateKeyFile=<material.Path>
	wantTLS := false
	wantCA := false
	wantCertKeyFile := false
	for _, a := range args {
		switch {
		case a == "--tls":
			wantTLS = true
		case strings.HasPrefix(a, "--tlsCAFile="):
			if a != "--tlsCAFile="+p.caPath {
				t.Errorf("tlsCAFile = %q", a)
			}
			wantCA = true
		case strings.HasPrefix(a, "--tlsCertificateKeyFile="):
			if a != "--tlsCertificateKeyFile="+material.Path {
				t.Errorf("tlsCertificateKeyFile = %q, want %q", a, "--tlsCertificateKeyFile="+material.Path)
			}
			wantCertKeyFile = true
		}
	}
	if !wantTLS || !wantCA || !wantCertKeyFile {
		t.Fatalf("missing args: tls=%v ca=%v certKey=%v args=%v", wantTLS, wantCA, wantCertKeyFile, args)
	}

	// (b) material.Path exists; (c) mode 0600; (d) one CERT + one PRIVATE KEY block
	info, err := os.Stat(material.Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %v, want 0600", perm)
	}
	data, err := os.ReadFile(material.Path)
	if err != nil {
		t.Fatal(err)
	}
	var hasCert, hasKey bool
	rest := data
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		switch blk.Type {
		case "CERTIFICATE":
			hasCert = true
		case "PRIVATE KEY":
			hasKey = true
		}
	}
	if !hasCert || !hasKey {
		t.Errorf("combined file missing block(s): hasCert=%v hasKey=%v", hasCert, hasKey)
	}

	// no orphan in temp dir (only our material file is new)
	post := snapshotMaterialFiles(t)
	delete(post, material.Path)
	for f := range post {
		if _, was := pre[f]; !was {
			t.Errorf("unexpected new temp file: %s", f)
		}
	}
}

// --- T012: combined-PEM does not create a temp file -------------------------

func TestPrepareMongoTLS_CombinedPEM_NoTempFile(t *testing.T) {
	p := genPEMPair(t)
	combined := combinedFile(t, p)
	cfg := &ports.Config{
		Enabled:    true,
		Mode:       "verify-full",
		CACertPath: p.caPath,
		ClientCert: combined,
	}
	pre := snapshotMaterialFiles(t)
	material, args, err := PrepareMongoTLS(cfg, "job-combined")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer material.Close()

	if material.Path != combined {
		t.Errorf("material.Path = %q, want %q (no temp file for combined PEM)", material.Path, combined)
	}
	// No new file should appear in temp dir.
	post := snapshotMaterialFiles(t)
	for f := range post {
		if _, was := pre[f]; !was {
			t.Errorf("unexpected new temp file: %s", f)
		}
	}
	// args still reference the original combined path
	found := false
	for _, a := range args {
		if a == "--tlsCertificateKeyFile="+combined {
			found = true
		}
	}
	if !found {
		t.Errorf("expected combined path in args, got %v", args)
	}
}

// --- T013: encrypted PKCS#8 key happy path ----------------------------------

func TestPrepareMongoTLS_EncryptedKey_PKCS8(t *testing.T) {
	p := genPEMPair(t)
	encKey := encryptedPKCS8File(t, p, "hunter2")
	t.Setenv("SENTINEL_TEST_KEY_PW", "hunter2")
	cfg := &ports.Config{
		Enabled:              true,
		Mode:                 "verify-full",
		CACertPath:           p.caPath,
		ClientCert:           p.certPath,
		ClientKey:            encKey,
		ClientKeyPasswordEnv: "SENTINEL_TEST_KEY_PW",
	}
	material, _, err := PrepareMongoTLS(cfg, "job-enc")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer material.Close()

	data, err := os.ReadFile(material.Path)
	if err != nil {
		t.Fatal(err)
	}
	rest := data
	var hasUnencrypted bool
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		if blk.Type == "PRIVATE KEY" || blk.Type == "RSA PRIVATE KEY" {
			hasUnencrypted = true
		}
		if blk.Type == "ENCRYPTED PRIVATE KEY" {
			t.Errorf("combined file still contains ENCRYPTED PRIVATE KEY block")
		}
	}
	if !hasUnencrypted {
		t.Errorf("combined file missing unencrypted private key")
	}
}

// --- T014/T022 (combined): error cases --------------------------------------

func TestPrepareMongoTLS_ErrorCases(t *testing.T) {
	p := genPEMPair(t)

	tests := []struct {
		name      string
		cfgFn     func(*pemPair) *ports.Config
		envName   string
		envValue  string
		wantSubst string
	}{
		{
			name: "cert without key",
			cfgFn: func(p *pemPair) *ports.Config {
				return &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath}
			},
			wantSubst: "both be set for mutual TLS",
		},
		{
			name: "key without cert",
			cfgFn: func(p *pemPair) *ports.Config {
				return &ports.Config{Enabled: true, Mode: "verify-full", ClientKey: p.keyPath}
			},
			wantSubst: "both be set for mutual TLS",
		},
		{
			name: "combined cert + separate key",
			cfgFn: func(p *pemPair) *ports.Config {
				combined := combinedFile(t, *p)
				return &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: combined, ClientKey: p.keyPath}
			},
			wantSubst: "tls.client_cert already contains a private key",
		},
		{
			name: "encrypted key but no password env field",
			cfgFn: func(p *pemPair) *ports.Config {
				encKey := encryptedPKCS8File(t, *p, "x")
				return &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath, ClientKey: encKey}
			},
			wantSubst: "tls.client_key is encrypted",
		},
		{
			name: "password env field set but env unset",
			cfgFn: func(p *pemPair) *ports.Config {
				encKey := encryptedPKCS8File(t, *p, "x")
				return &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath, ClientKey: encKey, ClientKeyPasswordEnv: "SENTINEL_NOT_SET_XYZ"}
			},
			wantSubst: `passphrase env var "SENTINEL_NOT_SET_XYZ" is unset or empty`,
		},
		{
			name: "wrong passphrase",
			cfgFn: func(p *pemPair) *ports.Config {
				encKey := encryptedPKCS8File(t, *p, "right")
				return &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath, ClientKey: encKey, ClientKeyPasswordEnv: "SENTINEL_WRONG_PW"}
			},
			envName:   "SENTINEL_WRONG_PW",
			envValue:  "wrong",
			wantSubst: "decrypt tls.client_key",
		},
		{
			name: "key file empty",
			cfgFn: func(p *pemPair) *ports.Config {
				empty := filepath.Join(t.TempDir(), "empty.key")
				_ = os.WriteFile(empty, []byte(""), 0o600)
				return &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath, ClientKey: empty}
			},
			wantSubst: "file is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envName != "" {
				t.Setenv(tt.envName, tt.envValue)
			}
			pre := snapshotMaterialFiles(t)
			cfg := tt.cfgFn(&p)
			material, _, err := PrepareMongoTLS(cfg, "job-err")
			if material != nil {
				defer material.Close()
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSubst)
			}
			if !strings.Contains(err.Error(), tt.wantSubst) {
				t.Errorf("error = %q; want substring %q", err.Error(), tt.wantSubst)
			}
			// No new orphan files left behind.
			post := snapshotMaterialFiles(t)
			for f := range post {
				if _, was := pre[f]; !was {
					t.Errorf("unexpected leaked temp file after error: %s", f)
				}
			}
		})
	}
}

// --- T026: Close idempotence + cleanup --------------------------------------

func TestPrepareMongoTLS_CleanupOnSuccess(t *testing.T) {
	p := genPEMPair(t)
	cfg := &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath, ClientKey: p.keyPath}
	material, _, err := PrepareMongoTLS(cfg, "job-c")
	if err != nil {
		t.Fatal(err)
	}
	path := material.Path
	if err := material.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after Close: stat err = %v", err)
	}
	// idempotent
	if err := material.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// --- T038: concurrent jobs share zero state ---------------------------------

func TestPrepareMongoTLS_ConcurrentJobs(t *testing.T) {
	p := genPEMPair(t)
	pre := snapshotMaterialFiles(t)

	const N = 8
	cfg := &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath, ClientKey: p.keyPath}

	paths := make([]string, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, _, err := PrepareMongoTLS(cfg, "concur")
			if err != nil {
				t.Errorf("prep %d: %v", i, err)
				return
			}
			paths[i] = m.Path
			// Verify file exists and is 0600 while the "job" is "running".
			info, err := os.Stat(m.Path)
			if err != nil {
				t.Errorf("stat %d: %v", i, err)
			} else if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("perm %d = %v want 0600", i, perm)
			}
			_ = m.Close()
		}(i)
	}
	wg.Wait()

	// pairwise distinct
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if seen[p] {
			t.Errorf("duplicate material path across goroutines: %s", p)
		}
		seen[p] = true
	}
	// post-cleanup: no new orphan files relative to pre snapshot
	post := snapshotMaterialFiles(t)
	for f := range post {
		if _, was := pre[f]; !was {
			t.Errorf("orphan after concurrent test: %s", f)
		}
	}
}
