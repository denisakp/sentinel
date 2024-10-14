package mariadb_dump

import "testing"

func Test_validateRequiredArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    *MariaDBDumpArgs
		wantErr bool
	}{
		{
			name:    "Missing required args",
			args:    &MariaDBDumpArgs{},
			wantErr: true,
		},
		{
			name:    "Missing database name",
			args:    &MariaDBDumpArgs{Username: "root"},
			wantErr: true,
		},
		{
			name:    "Missing username",
			args:    &MariaDBDumpArgs{Database: "test"},
			wantErr: true,
		},
		{
			name:    "Valid args",
			args:    &MariaDBDumpArgs{Username: "root", Database: "test"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateRequiredArgs(tt.args); (err != nil) != tt.wantErr {
				t.Errorf("validateRequiredArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
