package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/spf13/cobra"
)

type encryptSecretsFlags struct {
	out     string
	keyEnv  string
	keyFile string
	force   bool
}

func runEncryptSecrets(t *testing.T, arg string, f encryptSecretsFlags) error {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("out", f.out, "")
	cmd.Flags().String("key-env", f.keyEnv, "")
	cmd.Flags().String("key-file", f.keyFile, "")
	cmd.Flags().Bool("force", f.force, "")
	return securityEncryptSecretsFileCmd.RunE(cmd, []string{arg})
}

func writeKeyFile(t *testing.T, dir string) string {
	t.Helper()
	b64, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "master.key")
	if err := os.WriteFile(p, []byte(b64), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEncryptSecretsFile_ProducesDecryptableFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeKeyFile(t, dir)
	inPath := filepath.Join(dir, "db.cnf")
	body := []byte("[client]\nuser=root\npassword=s3cr3t\n")
	if err := os.WriteFile(inPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "db.cnf.enc")

	if err := runEncryptSecrets(t, inPath, encryptSecretsFlags{out: outPath, keyFile: keyPath}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	enc, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !crypto.IsEncryptedSecretsFile(enc) {
		t.Fatal("output is not an SSEC container")
	}

	// Round-trips with the same key (read-path wiring covered in config tests).
	raw := decodeKey(t, keyPath)
	got, err := crypto.DecryptSecretsFile(enc, raw)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

func TestEncryptSecretsFile_InputUntouched(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeKeyFile(t, dir)
	inPath := filepath.Join(dir, "db.cnf")
	body := []byte("[client]\nuser=root\npassword=s3cr3t\n")
	if err := os.WriteFile(inPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	// Success path.
	if err := runEncryptSecrets(t, inPath, encryptSecretsFlags{out: filepath.Join(dir, "ok.enc"), keyFile: keyPath}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if after := readAll(t, inPath); after != string(body) {
		t.Fatalf("input modified after success: %q", after)
	}

	// Error path (bad key file) must also leave the input untouched.
	badKey := filepath.Join(dir, "bad.key")
	_ = os.WriteFile(badKey, []byte("not-base64!!!"), 0o600)
	err := runEncryptSecrets(t, inPath, encryptSecretsFlags{out: filepath.Join(dir, "err.enc"), keyFile: badKey})
	if err == nil {
		t.Fatal("expected error on bad key")
	}
	if after := readAll(t, inPath); after != string(body) {
		t.Fatalf("input modified after error: %q", after)
	}
}

func TestEncryptSecretsFile_RefusesOverwriteInput(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeKeyFile(t, dir)
	inPath := filepath.Join(dir, "db.cnf")
	if err := os.WriteFile(inPath, []byte("[client]\nuser=root\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runEncryptSecrets(t, inPath, encryptSecretsFlags{out: inPath, keyFile: keyPath})
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("expected refusal to overwrite input, got %v", err)
	}
}

func TestEncryptSecretsFile_RefusesExistingOutputWithoutForce(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeKeyFile(t, dir)
	inPath := filepath.Join(dir, "db.cnf")
	outPath := filepath.Join(dir, "out.enc")
	_ = os.WriteFile(inPath, []byte("[client]\nuser=root\n"), 0o600)
	_ = os.WriteFile(outPath, []byte("existing"), 0o600)

	err := runEncryptSecrets(t, inPath, encryptSecretsFlags{out: outPath, keyFile: keyPath})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected refusal without --force, got %v", err)
	}

	if err := runEncryptSecrets(t, inPath, encryptSecretsFlags{out: outPath, keyFile: keyPath, force: true}); err != nil {
		t.Fatalf("expected --force to overwrite, got %v", err)
	}
}

func TestEncryptSecretsFile_RequiresKeyAndOut(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "db.cnf")
	_ = os.WriteFile(inPath, []byte("[client]\nuser=root\n"), 0o600)

	if err := runEncryptSecrets(t, inPath, encryptSecretsFlags{keyFile: writeKeyFile(t, dir)}); err == nil {
		t.Fatal("expected error when --out missing")
	}
	if err := runEncryptSecrets(t, inPath, encryptSecretsFlags{out: filepath.Join(dir, "o.enc")}); err == nil {
		t.Fatal("expected error when no key provided")
	}
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decodeKey(t *testing.T, keyPath string) []byte {
	t.Helper()
	kp := &crypto.FileKeyProvider{FilePath: keyPath}
	raw, err := kp.GetKey()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
