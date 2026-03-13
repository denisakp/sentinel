package crypto_test

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/denisakp/sentinel/internal/crypto"
)

func TestFileKeyProvider_EnvVar(t *testing.T) {
	// Generate a valid 32-byte base64 key
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	encoded := base64.StdEncoding.EncodeToString(key)

	t.Setenv("TEST_SENTINEL_KEY", encoded)

	p := &crypto.FileKeyProvider{EnvVar: "TEST_SENTINEL_KEY"}
	got, err := p.GetKey()
	if err != nil {
		t.Fatalf("GetKey() error = %v", err)
	}
	if len(got) != 32 {
		t.Errorf("GetKey() len = %d, want 32", len(got))
	}
}

func TestFileKeyProvider_KeyFile(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 10)
	}
	encoded := base64.StdEncoding.EncodeToString(key)

	f, err := os.CreateTemp("", "sentinel-key-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString(encoded)
	f.Close()

	p := &crypto.FileKeyProvider{FilePath: f.Name()}
	got, err := p.GetKey()
	if err != nil {
		t.Fatalf("GetKey() error = %v", err)
	}
	if len(got) != 32 {
		t.Errorf("GetKey() len = %d, want 32", len(got))
	}
}

func TestFileKeyProvider_MissingKey(t *testing.T) {
	p := &crypto.FileKeyProvider{EnvVar: "NONEXISTENT_ENV_VAR_XYZ123"}
	_, err := p.GetKey()
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestFileKeyProvider_EmptyKey(t *testing.T) {
	t.Setenv("TEST_SENTINEL_EMPTY", "")
	p := &crypto.FileKeyProvider{EnvVar: "TEST_SENTINEL_EMPTY"}
	_, err := p.GetKey()
	if err == nil {
		t.Error("expected error for empty key")
	}
}

func TestFileKeyProvider_InvalidBase64(t *testing.T) {
	t.Setenv("TEST_SENTINEL_BADKEY", "this-is-not-base64!!!")
	p := &crypto.FileKeyProvider{EnvVar: "TEST_SENTINEL_BADKEY"}
	_, err := p.GetKey()
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestFileKeyProvider_WrongKeyLength(t *testing.T) {
	// 16 bytes encoded → wrong length
	short := make([]byte, 16)
	encoded := base64.StdEncoding.EncodeToString(short)
	t.Setenv("TEST_SENTINEL_SHORT", encoded)

	p := &crypto.FileKeyProvider{EnvVar: "TEST_SENTINEL_SHORT"}
	_, err := p.GetKey()
	if err == nil {
		t.Error("expected error for wrong key length")
	}
}

func TestFileKeyProvider_EnvPreferredOverFile(t *testing.T) {
	fromEnv := make([]byte, 32)
	for i := range fromEnv {
		fromEnv[i] = byte(i + 1)
	}
	fromFile := make([]byte, 32)
	for i := range fromFile {
		fromFile[i] = byte(i + 2)
	}

	t.Setenv("TEST_SENTINEL_ENV_FIRST", base64.StdEncoding.EncodeToString(fromEnv))

	f, err := os.CreateTemp("", "sentinel-key-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(fromFile)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	p := &crypto.FileKeyProvider{EnvVar: "TEST_SENTINEL_ENV_FIRST", FilePath: f.Name()}
	got, err := p.GetKey()
	if err != nil {
		t.Fatalf("GetKey() error = %v", err)
	}
	if got[0] != fromEnv[0] {
		t.Fatalf("expected env key to be used, got key starting with %d", got[0])
	}
}

func TestFileKeyProvider_FallsBackToFileWhenEnvEmpty(t *testing.T) {
	fromFile := make([]byte, 32)
	for i := range fromFile {
		fromFile[i] = byte(i + 11)
	}

	t.Setenv("TEST_SENTINEL_EMPTY_ENV", "")

	f, err := os.CreateTemp("", "sentinel-key-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(fromFile)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	p := &crypto.FileKeyProvider{EnvVar: "TEST_SENTINEL_EMPTY_ENV", FilePath: f.Name()}
	got, err := p.GetKey()
	if err != nil {
		t.Fatalf("GetKey() error = %v", err)
	}
	if got[0] != fromFile[0] {
		t.Fatalf("expected file fallback key to be used, got key starting with %d", got[0])
	}
}

func TestDeriveKey_Deterministic(t *testing.T) {
	master := make([]byte, 32)
	salt := make([]byte, 32)
	for i := range master {
		master[i] = byte(i)
		salt[i] = byte(i + 100)
	}

	k1 := crypto.DeriveKey(master, salt)
	k2 := crypto.DeriveKey(master, salt)

	if len(k1) != 32 {
		t.Errorf("DeriveKey() len = %d, want 32", len(k1))
	}

	for i := range k1 {
		if k1[i] != k2[i] {
			t.Errorf("DeriveKey() not deterministic at byte %d", i)
			break
		}
	}
}

func TestGenerateKey(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		t.Fatalf("GenerateKey() returned invalid base64: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("GenerateKey() decoded length = %d, want 32", len(decoded))
	}
	// Keys should be random — generate two and they should differ
	key2, _ := crypto.GenerateKey()
	if key == key2 {
		t.Error("GenerateKey() returned identical keys on consecutive calls")
	}
}

func TestGenerateSalt(t *testing.T) {
	salt, err := crypto.GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt() error = %v", err)
	}
	if len(salt) != 32 {
		t.Errorf("GenerateSalt() length = %d, want 32", len(salt))
	}
	// Salts should be random
	salt2, _ := crypto.GenerateSalt()
	allSame := true
	for i := range salt {
		if salt[i] != salt2[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("GenerateSalt() returned identical salts on consecutive calls")
	}
}
