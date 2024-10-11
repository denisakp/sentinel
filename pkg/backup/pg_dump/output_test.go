package pg_dump

import (
	"testing"
)

func Test_setOutName(t *testing.T) {
	tests := []struct {
		name    string
		args    *PgDumpArgs
		wantOut string
		wantErr bool
	}{
		{
			name:    "No outName, no compress, no format",
			args:    &PgDumpArgs{OutName: "", Compress: false, OutFormat: "", Database: "test"},
			wantOut: "test_backup.backup",
			wantErr: false,
		},
		{
			name:    "Compress, no outFormat, adds custom format",
			args:    &PgDumpArgs{OutName: "", Compress: true, OutFormat: "", Database: "test"},
			wantOut: "test_backup.backup",
			wantErr: false,
		},
		{
			name:    "Tar format with compression enabled - error expected",
			args:    &PgDumpArgs{OutName: "test", Compress: true, OutFormat: "t", Database: "test"},
			wantOut: "test.tar",
			wantErr: true,
		},
		{
			name:    "Plain format with compression enabled - error expected",
			args:    &PgDumpArgs{OutName: "test", Compress: true, OutFormat: "p", Database: "test"},
			wantOut: "test.sql",
			wantErr: true,
		},
		{
			name:    "Custom format without compression",
			args:    &PgDumpArgs{OutName: "test", Compress: false, OutFormat: "c", Database: "test"},
			wantOut: "test.backup",
			wantErr: false,
		},
		{
			name:    "Tar format without compression",
			args:    &PgDumpArgs{OutName: "test", Compress: false, OutFormat: "t", Database: "test"},
			wantOut: "test.tar",
			wantErr: false,
		},
		{
			name:    "Plain format without compression",
			args:    &PgDumpArgs{OutName: "test", Compress: false, OutFormat: "p", Database: "test"},
			wantOut: "test.sql",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := setOutName(tt.args); (err != nil) != tt.wantErr {
				t.Errorf("setOutName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
