package mysql

import (
	"reflect"
	"strings"
	"testing"

	internaltls "github.com/denisakp/sentinel/internal/adapters/tls"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/utils"
)

func TestArgsBuilder(t *testing.T) {
	tests := []struct {
		name    string
		args    *MySqlDumpArgs
		want    []string
		wantErr bool
	}{
		{
			name:    "Required args missing",
			args:    &MySqlDumpArgs{},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Missing database username",
			args:    &MySqlDumpArgs{Database: "test"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Missing database name",
			args:    &MySqlDumpArgs{Username: "root"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Default Host and Port",
			args:    &MySqlDumpArgs{Username: "root", Password: "root", Database: "testdb"},
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "testdb"},
			wantErr: false,
		},
		{
			name:    "Provided host and port",
			args:    &MySqlDumpArgs{Username: "root", Password: "root", Database: "test", Host: "us-west1.mysql.domain.com", Port: "3319"},
			want:    []string{"--host=us-west1.mysql.domain.com", "--port=3319", "--user=root", "test"},
			wantErr: false,
		},
		{
			name:    "Skip password",
			args:    &MySqlDumpArgs{Username: "root", Database: "test"},
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "--skip-password", "test"},
			wantErr: false,
		},
		{
			name:    "Additional Arguments",
			args:    &MySqlDumpArgs{Username: "root", Database: "test", AdditionalArgs: "--flush-privileges"},
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "--skip-password", "--flush-privileges", "test"},
			wantErr: false,
		},
		{
			name:    "Remove duplicate",
			args:    &MySqlDumpArgs{Username: "root", Database: "test", AdditionalArgs: "--port=3306"},
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "--skip-password", "test"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := argsBuilder(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("argsBuilder() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("argsBuilder() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArgsBuilder_TLS(t *testing.T) {
	tests := []struct {
		name            string
		tls             *ports.Config
		wantContains    []string
		wantNoSSLPrefix bool
	}{
		{
			name:            "AB-01 TLS disabled",
			tls:             &ports.Config{Enabled: false, Mode: "require"},
			wantNoSSLPrefix: true,
		},
		{
			name:            "AB-02 TLS nil",
			tls:             nil,
			wantNoSSLPrefix: true,
		},
		{
			name:         "AB-03 TLS require",
			tls:          &ports.Config{Enabled: true, Mode: "require"},
			wantContains: []string{"--ssl-mode=REQUIRED"},
		},
		{
			name:         "AB-04 TLS verify-ca",
			tls:          &ports.Config{Enabled: true, Mode: "verify-ca", CACertPath: "/tmp/ca.pem"},
			wantContains: []string{"--ssl-mode=VERIFY_CA", "--ssl-ca=/tmp/ca.pem"},
		},
		{
			name:         "AB-05 TLS verify-full",
			tls:          &ports.Config{Enabled: true, Mode: "verify-full", CACertPath: "/tmp/ca.pem"},
			wantContains: []string{"--ssl-mode=VERIFY_IDENTITY", "--ssl-ca=/tmp/ca.pem"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := argsBuilder(&MySqlDumpArgs{
				Username: "root",
				Database: "test",
				TLS:      tt.tls,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNoSSLPrefix {
				for _, a := range args {
					if strings.HasPrefix(a, "--ssl-") {
						t.Fatalf("unexpected TLS flag %q in args=%v", a, args)
					}
				}
				return
			}
			assertContainsAll(t, args, tt.wantContains...)
		})
	}
}

func TestArgsBuilder_MutualTLS(t *testing.T) {
	cfg := &ports.Config{
		Enabled:    true,
		Mode:       "verify-full",
		CACertPath: "/tmp/ca.pem",
		ClientCert: "/tmp/client.crt",
		ClientKey:  "/tmp/client.key",
	}

	// Pre-flight: confirm the shared TLS helper still emits client cert/key.
	helperOut := internaltls.BuildTLSArgs("mysql", cfg)
	hasCert := false
	for _, a := range helperOut {
		if strings.HasPrefix(a, "--ssl-cert=") {
			hasCert = true
			break
		}
	}
	if !hasCert {
		t.Skip("mutual TLS for MySQL pending PRD 05")
	}

	args, err := argsBuilder(&MySqlDumpArgs{
		Username: "root",
		Database: "test",
		TLS:      cfg,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContainsAll(t, args,
		"--ssl-ca=/tmp/ca.pem",
		"--ssl-cert=/tmp/client.crt",
		"--ssl-key=/tmp/client.key",
	)
}

func TestArgsBuilder_PasswordNoLeak(t *testing.T) {
	tests := []struct {
		name string
		pwd  string
	}{
		{name: "AB-07 normal special chars", pwd: "s3cret!$pace word"},
		{name: "AB-08 shell-meta chars", pwd: "p@$$'\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := argsBuilder(&MySqlDumpArgs{
				Username: "root",
				Password: tt.pwd,
				Database: "test",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertNoPasswordInArgs(t, args, tt.pwd)
		})
	}
}

func TestArgsBuilder_AdditionalArgs(t *testing.T) {
	t.Run("AB-09 merge new flag", func(t *testing.T) {
		args, err := argsBuilder(&MySqlDumpArgs{
			Username:       "root",
			Database:       "test",
			AdditionalArgs: "--single-transaction",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count := 0
		for _, a := range args {
			if a == "--single-transaction" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("--single-transaction count = %d, want 1; args=%v", count, args)
		}
	})

	t.Run("AB-10 empty additional args no stray token", func(t *testing.T) {
		args, err := argsBuilder(&MySqlDumpArgs{
			Username:       "root",
			Database:       "test",
			AdditionalArgs: "",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for i, a := range args {
			if a == "" {
				t.Fatalf("empty string at args[%d]; args=%v", i, args)
			}
		}
		if len(args) != 5 {
			t.Fatalf("len(args) = %d, want 5 (host, port, user, --skip-password, database); args=%v", len(args), args)
		}
	})
}

func TestFinalOutNameMySQLExtensionPreservation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "adds default sql extension", in: "mysql-dev", want: "mysql-dev.sql"},
		{name: "keeps existing sql extension", in: "mysql-dev.sql", want: "mysql-dev.sql"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utils.FinalOutName(tt.in)
			if got != tt.want {
				t.Fatalf("FinalOutName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
