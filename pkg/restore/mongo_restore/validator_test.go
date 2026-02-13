package mongo_restore

import (
	"testing"
)

func TestValidateOnConflict(t *testing.T) {
	tests := []struct {
		strategy string
		wantErr  bool
	}{
		{"ignore", false},
		{"replace", false},
		{"error", false},
		{"", false},
		{"skip", true},
		{"abort", true},
		{"IGNORE", true},
	}

	for _, tt := range tests {
		t.Run(tt.strategy, func(t *testing.T) {
			err := ValidateOnConflict(tt.strategy)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOnConflict() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRequiredArgs(t *testing.T) {
	tests := []struct {
		name    string
		ra      *RestoreArgs
		wantErr bool
	}{
		{
			name:    "nil args",
			ra:      nil,
			wantErr: true,
		},
		{
			name: "missing URI",
			ra: &RestoreArgs{
				BackupPath: "/path/to/backup",
			},
			wantErr: true,
		},
		{
			name: "missing backup path",
			ra: &RestoreArgs{
				URI:        "mongodb://localhost/mydb",
				BackupPath: "",
			},
			wantErr: true,
		},
		{
			name: "valid args with BackupPath",
			ra: &RestoreArgs{
				URI:        "mongodb://localhost/mydb",
				BackupPath: "/path/to/backup",
			},
			wantErr: false,
		},
		{
			name: "valid args with Storage OutName",
			ra: &RestoreArgs{
				URI: "mongodb://localhost/mydb",
				Storage: &struct {
					Type    string
					OutName string
					Handler interface{}
				}{OutName: "backup.archive"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequiredArgs(tt.ra)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRequiredArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
