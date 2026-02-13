package pg_restore

import (
	"testing"
)

func TestValidateRestoreFormat(t *testing.T) {
	tests := []struct {
		format  string
		wantErr bool
	}{
		{"c", false},
		{"d", false},
		{"t", false},
		{"p", false},
		{"", false},
		{"invalid", true},
		{"x", true},
		{"custom", true},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			err := ValidateRestoreFormat(tt.format)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRestoreFormat() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

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
			name: "missing database",
			ra: &RestoreArgs{
				Username:   "user",
				BackupPath: "/path/to/backup",
			},
			wantErr: true,
		},
		{
			name: "missing username",
			ra: &RestoreArgs{
				Database:   "mydb",
				BackupPath: "/path/to/backup",
			},
			wantErr: true,
		},
		{
			name: "missing backup path",
			ra: &RestoreArgs{
				Database:   "mydb",
				Username:   "user",
				BackupPath: "",
			},
			wantErr: true,
		},
		{
			name: "valid args with BackupPath",
			ra: &RestoreArgs{
				Database:   "mydb",
				Username:   "user",
				BackupPath: "/path/to/backup",
			},
			wantErr: false,
		},
		{
			name: "valid args with Storage OutName",
			ra: &RestoreArgs{
				Database: "mydb",
				Username: "user",
				Storage: &struct {
					Type    string
					OutName string
					Handler interface{}
				}{OutName: "backup.sql"},
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
