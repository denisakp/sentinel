package mariadb

import (
	"reflect"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/utils"
)

func TestArgsBuilder(t *testing.T) {
	tests := []struct {
		name    string
		args    *MariaDBDumpArgs
		want    []string
		wantErr bool
	}{
		{
			name:    "Required args missing",
			args:    &MariaDBDumpArgs{},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Missing database username",
			args:    &MariaDBDumpArgs{Database: "test"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Missing database name",
			args:    &MariaDBDumpArgs{Username: "root"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Default Host and Port",
			args:    &MariaDBDumpArgs{Username: "root", Password: "root", Database: "test"},
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "test"},
			wantErr: false,
		},
		{
			name:    "Provided host and port",
			args:    &MariaDBDumpArgs{Username: "root", Password: "root", Database: "test", Host: "us-west1.mysql.domain.com", Port: "3319"},
			want:    []string{"--host=us-west1.mysql.domain.com", "--port=3319", "--user=root", "test"},
			wantErr: false,
		},
		{
			name:    "Empty password",
			args:    &MariaDBDumpArgs{Username: "root", Database: "test"},
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "test"},
			wantErr: false,
		},
		{
			name:    "Additional Arguments",
			args:    &MariaDBDumpArgs{Username: "root", Database: "test", AdditionalArgs: "--flush-privileges"},
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "--flush-privileges", "test"},
			wantErr: false,
		},
		{
			name:    "Remove duplicate",
			args:    &MariaDBDumpArgs{Username: "root", Database: "test", AdditionalArgs: "--port=3306"},
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "test"},
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

func TestArgsBuilderNeverContainsPassword(t *testing.T) {
	cases := []*MariaDBDumpArgs{
		{Username: "root", Database: "test"},
		{Username: "root", Database: "test", Password: "secret"},
		{Username: "root", Database: "test", Password: "secret", AdditionalArgs: "--skip-lock-tables"},
		{Username: "root", Database: "test", Password: "secret", Host: "h", Port: "3306"},
	}
	for i, c := range cases {
		got, err := argsBuilder(c)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		for _, a := range got {
			if strings.HasPrefix(a, "--password") {
				t.Errorf("case %d: argv leaked --password*: %q in %v", i, a, got)
			}
			if strings.Contains(a, "password=") {
				t.Errorf("case %d: argv leaked password=: %q in %v", i, a, got)
			}
		}
	}
}

func TestFinalOutNameMariaDBExtensionPreservation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "adds default sql extension", in: "mariadb-dev", want: "mariadb-dev.sql"},
		{name: "keeps existing sql extension", in: "mariadb-dev.sql", want: "mariadb-dev.sql"},
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
