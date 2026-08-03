package pg

import (
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
)

func Test_setOutName(t *testing.T) {

	tests := []struct {
		name    string
		args    *PgDumpArgs
		wantOut string
		wantErr bool
	}{
		{
			name:    "Plain format without compression - adds .sql extension",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "my_backup"}, Compress: false, PgOutFormat: "p", Database: "test"},
			wantOut: "my_backup.sql",
			wantErr: false,
		},
		{
			name:    "Custom format without compression - adds .backup extension",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "my_backup"}, Compress: false, PgOutFormat: "c", Database: "test"},
			wantOut: "my_backup.backup",
			wantErr: false,
		},
		{
			name:    "Tar format with compression enabled - error expected",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "test"}, Compress: true, PgOutFormat: "t", Database: "test"},
			wantOut: "test.tar",
			wantErr: true,
		},
		{
			name:    "Plain format with compression enabled - error expected",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "test"}, Compress: true, PgOutFormat: "p", Database: "test"},
			wantOut: "test.sql",
			wantErr: true,
		},
		{
			name:    "Custom format without compression",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "test"}, Compress: false, PgOutFormat: "c", Database: "test"},
			wantOut: "test.backup",
			wantErr: false,
		},
		{
			name:    "Tar format without compression",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "test"}, Compress: false, PgOutFormat: "t", Database: "test"},
			wantOut: "test.tar",
			wantErr: false,
		},
		{
			name:    "Plain format without compression",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "test"}, Compress: false, PgOutFormat: "p", Database: "test"},
			wantOut: "test.sql",
			wantErr: false,
		},
		{
			name:    "Plain format keeps existing .sql extension",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "test.sql"}, Compress: false, PgOutFormat: "p", Database: "test"},
			wantOut: "test.sql",
			wantErr: false,
		},
		{
			name:    "Custom format keeps existing .backup extension",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "test.backup"}, Compress: false, PgOutFormat: "c", Database: "test"},
			wantOut: "test.backup",
			wantErr: false,
		},
		{
			name:    "Tar format keeps existing .tar extension",
			args:    &PgDumpArgs{Storage: &storage.Params{OutName: "test.tar"}, Compress: false, PgOutFormat: "t", Database: "test"},
			wantOut: "test.tar",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := setOutName(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("setOutName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// Only check outName if no error was expected
			if !tt.wantErr && tt.args.Storage.OutName != tt.wantOut {
				t.Errorf("setOutName() outName = %v, want %v", tt.args.Storage.OutName, tt.wantOut)
			}
		})
	}
}
