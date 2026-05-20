package tls_test

import (
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/tls"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *tls.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config is valid",
			cfg:     nil,
			wantErr: false,
		},
		{
			name:    "disabled config is valid",
			cfg:     &tls.Config{Enabled: false, Mode: "invalid-mode"},
			wantErr: false,
		},
		{
			name:    "prefer mode no certs",
			cfg:     &tls.Config{Enabled: true, Mode: "prefer"},
			wantErr: false,
		},
		{
			name:    "require mode no certs",
			cfg:     &tls.Config{Enabled: true, Mode: "require"},
			wantErr: false,
		},
		{
			name:    "verify-ca missing ca_cert",
			cfg:     &tls.Config{Enabled: true, Mode: "verify-ca"},
			wantErr: true,
		},
		{
			name:    "verify-ca with ca_cert",
			cfg:     &tls.Config{Enabled: true, Mode: "verify-ca", CACertPath: "/etc/certs/ca.crt"},
			wantErr: false,
		},
		{
			name:    "verify-full missing ca_cert",
			cfg:     &tls.Config{Enabled: true, Mode: "verify-full"},
			wantErr: true,
		},
		{
			name:    "verify-full with ca_cert",
			cfg:     &tls.Config{Enabled: true, Mode: "verify-full", CACertPath: "/etc/certs/ca.crt"},
			wantErr: false,
		},
		{
			name:    "invalid mode",
			cfg:     &tls.Config{Enabled: true, Mode: "allow"},
			wantErr: true,
		},
		{
			// PRD-05 regression guard: half-configured mTLS must be rejected at validation.
			name:    "client cert without key",
			cfg:     &tls.Config{Enabled: true, Mode: "prefer", ClientCert: "/etc/certs/client.crt"},
			wantErr: true,
			errMsg:  "client_cert and tls.client_key must both be set",
		},
		{
			name:    "client key without cert",
			cfg:     &tls.Config{Enabled: true, Mode: "prefer", ClientKey: "/etc/certs/client.key"},
			wantErr: true,
			errMsg:  "client_cert and tls.client_key must both be set",
		},
		{
			name: "mutual TLS both cert and key",
			cfg: &tls.Config{
				Enabled:    true,
				Mode:       "verify-full",
				CACertPath: "/etc/certs/ca.crt",
				ClientCert: "/etc/certs/client.crt",
				ClientKey:  "/etc/certs/client.key",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errMsg != "" && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestBuildTLSArgs_Postgres(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *tls.Config
		wantArgs []string
	}{
		{
			name:     "nil config returns empty",
			cfg:      nil,
			wantArgs: nil,
		},
		{
			name:     "disabled returns empty",
			cfg:      &tls.Config{Enabled: false},
			wantArgs: nil,
		},
		{
			name:     "prefer mode",
			cfg:      &tls.Config{Enabled: true, Mode: "prefer"},
			wantArgs: []string{"--sslmode=prefer"},
		},
		{
			name:     "require mode",
			cfg:      &tls.Config{Enabled: true, Mode: "require"},
			wantArgs: []string{"--sslmode=require"},
		},
		{
			name:     "verify-full with all certs",
			cfg:      &tls.Config{Enabled: true, Mode: "verify-full", CACertPath: "/ca.crt", ClientCert: "/c.crt", ClientKey: "/c.key"},
			wantArgs: []string{"--sslmode=verify-full", "--sslrootcert=/ca.crt", "--sslcert=/c.crt", "--sslkey=/c.key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tls.BuildTLSArgs("postgres", tt.cfg)
			if len(got) != len(tt.wantArgs) {
				t.Errorf("BuildTLSArgs() = %v, want %v", got, tt.wantArgs)
				return
			}
			for i := range got {
				if got[i] != tt.wantArgs[i] {
					t.Errorf("BuildTLSArgs()[%d] = %q, want %q", i, got[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestBuildTLSArgs_MySQL(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		wantFlag string
	}{
		{"prefer", "prefer", "--ssl-mode=PREFERRED"},
		{"require", "require", "--ssl-mode=REQUIRED"},
		{"verify-ca", "verify-ca", "--ssl-mode=VERIFY_CA"},
		{"verify-full", "verify-full", "--ssl-mode=VERIFY_IDENTITY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &tls.Config{Enabled: true, Mode: tt.mode}
			args := tls.BuildTLSArgs("mysql", cfg)
			if len(args) == 0 || args[0] != tt.wantFlag {
				t.Errorf("BuildTLSArgs(mysql, %s) first arg = %v, want %s", tt.mode, args, tt.wantFlag)
			}
		})
	}
}

func TestBuildTLSArgs_MariaDB(t *testing.T) {
	cfg := &tls.Config{Enabled: true, Mode: "verify-full", CACertPath: "/ca.crt"}
	args := tls.BuildTLSArgs("mariadb", cfg)
	hasSSL := false
	hasVerify := false
	for _, a := range args {
		if a == "--ssl" {
			hasSSL = true
		}
		if a == "--ssl-verify-server-cert" {
			hasVerify = true
		}
	}
	if !hasSSL {
		t.Error("MariaDB TLS args missing --ssl flag")
	}
	if !hasVerify {
		t.Error("MariaDB verify-full missing --ssl-verify-server-cert flag")
	}
}

func TestBuildTLSArgs_MongoDB(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		wantTLS      bool
		wantInsecure bool
	}{
		{"prefer returns empty", "prefer", false, false},
		{"require adds insecure", "require", true, true},
		{"verify-full no insecure", "verify-full", true, false},
		{"verify-ca no insecure", "verify-ca", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &tls.Config{Enabled: true, Mode: tt.mode}
			args := tls.BuildTLSArgs("mongodb", cfg)
			hasTLS := false
			hasInsecure := false
			for _, a := range args {
				if a == "--tls" {
					hasTLS = true
				}
				if a == "--tlsInsecure" {
					hasInsecure = true
				}
			}
			if hasTLS != tt.wantTLS {
				t.Errorf("mode %s: --tls present = %v, want %v", tt.mode, hasTLS, tt.wantTLS)
			}
			if hasInsecure != tt.wantInsecure {
				t.Errorf("mode %s: --tlsInsecure present = %v, want %v", tt.mode, hasInsecure, tt.wantInsecure)
			}
		})
	}
}

func TestBuildTLSArgs_MySQL_WithCerts(t *testing.T) {
	cfg := &tls.Config{
		Enabled:    true,
		Mode:       "verify-ca",
		CACertPath: "/etc/certs/ca.crt",
		ClientCert: "/etc/certs/client.crt",
		ClientKey:  "/etc/certs/client.key",
	}
	args := tls.BuildTLSArgs("mysql", cfg)
	hasCA := false
	hasCert := false
	hasKey := false
	for _, a := range args {
		if a == "--ssl-ca=/etc/certs/ca.crt" {
			hasCA = true
		}
		if a == "--ssl-cert=/etc/certs/client.crt" {
			hasCert = true
		}
		if a == "--ssl-key=/etc/certs/client.key" {
			hasKey = true
		}
	}
	if !hasCA {
		t.Error("MySQL TLS args missing --ssl-ca")
	}
	if !hasCert {
		t.Error("MySQL TLS args missing --ssl-cert")
	}
	// PRD-05 cross-engine regression guard: ensures MySQL keeps emitting --ssl-key
	// alongside --ssl-cert, mirroring the MariaDB fix.
	if !hasKey {
		t.Error("MySQL TLS args missing --ssl-key")
	}
}

func TestBuildTLSArgs_MariaDB_WithCerts(t *testing.T) {
	cfg := &tls.Config{
		Enabled:    true,
		Mode:       "verify-full",
		CACertPath: "/etc/certs/ca.crt",
		ClientCert: "/etc/certs/client.crt",
		ClientKey:  "/etc/certs/client.key",
	}
	args := tls.BuildTLSArgs("mariadb", cfg)
	hasSSL := false
	hasCA := false
	hasCert := false
	hasKey := false
	hasVerify := false
	for _, a := range args {
		if a == "--ssl" {
			hasSSL = true
		}
		if a == "--ssl-ca=/etc/certs/ca.crt" {
			hasCA = true
		}
		if a == "--ssl-cert=/etc/certs/client.crt" {
			hasCert = true
		}
		if a == "--ssl-key=/etc/certs/client.key" {
			hasKey = true
		}
		if a == "--ssl-verify-server-cert" {
			hasVerify = true
		}
	}
	if !hasSSL {
		t.Error("MariaDB TLS args missing --ssl")
	}
	if !hasCA {
		t.Error("MariaDB TLS args missing --ssl-ca")
	}
	if !hasCert {
		t.Error("MariaDB TLS args missing --ssl-cert")
	}
	// PRD-05 regression guard: client key was silently dropped before this fix.
	if !hasKey {
		t.Error("MariaDB TLS args missing --ssl-key")
	}
	if !hasVerify {
		t.Error("MariaDB TLS verify-full missing --ssl-verify-server-cert")
	}
}

func TestBuildTLSArgs_MariaDB_NoCertsNoKey(t *testing.T) {
	cfg := &tls.Config{Enabled: true, Mode: "require"}
	args := tls.BuildTLSArgs("mariadb", cfg)
	for _, a := range args {
		if len(a) >= len("--ssl-cert=") && a[:len("--ssl-cert=")] == "--ssl-cert=" {
			t.Errorf("MariaDB TLS args unexpectedly contain --ssl-cert: %v", args)
		}
		if len(a) >= len("--ssl-key=") && a[:len("--ssl-key=")] == "--ssl-key=" {
			t.Errorf("MariaDB TLS args unexpectedly contain --ssl-key: %v", args)
		}
	}
}

func TestBuildTLSArgs_UnknownType(t *testing.T) {
	cfg := &tls.Config{Enabled: true, Mode: "require"}
	args := tls.BuildTLSArgs("oracle", cfg)
	if len(args) != 0 {
		t.Errorf("BuildTLSArgs(unknown) = %v, want empty", args)
	}
}

func TestBuildTLSArgs_Postgres_DefaultMode(t *testing.T) {
	// Empty mode should default to "prefer" in BuildTLSArgs
	cfg := &tls.Config{Enabled: true, Mode: ""}
	args := tls.BuildTLSArgs("postgres", cfg)
	if len(args) == 0 {
		t.Error("BuildTLSArgs(postgres, emptyMode) returned empty args")
	}
}

func TestBuildTLSArgs_Postgres_VerifyCA(t *testing.T) {
	cfg := &tls.Config{Enabled: true, Mode: "verify-ca", CACertPath: "/etc/ca.crt"}
	args := tls.BuildTLSArgs("postgres", cfg)
	hasMode := false
	for _, a := range args {
		if a == "--sslmode=verify-ca" {
			hasMode = true
		}
	}
	if !hasMode {
		t.Errorf("BuildTLSArgs(postgres, verify-ca) = %v, missing --sslmode=verify-ca", args)
	}
}

func TestBuildTLSArgs_MongoDB_WithCACert(t *testing.T) {
	cfg := &tls.Config{Enabled: true, Mode: "verify-full", CACertPath: "/etc/ca.crt", ClientCert: "/etc/client.pem"}
	args := tls.BuildTLSArgs("mongodb", cfg)
	hasCA := false
	hasCert := false
	for _, a := range args {
		if a == "--tlsCAFile=/etc/ca.crt" {
			hasCA = true
		}
		if a == "--tlsCertificateKeyFile=/etc/client.pem" {
			hasCert = true
		}
	}
	if !hasCA {
		t.Error("MongoDB TLS args missing --tlsCAFile")
	}
	if !hasCert {
		t.Error("MongoDB TLS args missing --tlsCertificateKeyFile")
	}
}
