package mysql_dump

import (
	"reflect"
	"testing"
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
			want:    []string{"--host=localhost", "--port=3306", "--user=root", "testdb"},
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
			want:    []string{"--host=localhost", "--port=3306", "--user=root", "--skip-password", "test"},
			wantErr: false,
		},
		{
			name:    "Additional Arguments",
			args:    &MySqlDumpArgs{Username: "root", Database: "test", AdditionalArgs: "--flush-privileges"},
			want:    []string{"--host=localhost", "--port=3306", "--user=root", "--skip-password", "--flush-privileges", "test"},
			wantErr: false,
		},
		{
			name:    "Remove duplicate",
			args:    &MySqlDumpArgs{Username: "root", Database: "test", AdditionalArgs: "--port=3306"},
			want:    []string{"--host=localhost", "--port=3306", "--user=root", "--skip-password", "test"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ArgsBuilder(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ArgsBuilder() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ArgsBuilder() got = %v, want %v", got, tt.want)
			}
		})
	}
}
