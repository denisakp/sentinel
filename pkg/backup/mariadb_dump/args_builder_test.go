package mariadb_dump

import (
	"reflect"
	"testing"
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
			want:    []string{"--host=127.0.0.1", "--port=3306", "--user=root", "--password=root", "test"},
			wantErr: false,
		},
		{
			name:    "Provided host and port",
			args:    &MariaDBDumpArgs{Username: "root", Password: "root", Database: "test", Host: "us-west1.mysql.domain.com", Port: "3319"},
			want:    []string{"--host=us-west1.mysql.domain.com", "--port=3319", "--user=root", "--password=root", "test"},
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
