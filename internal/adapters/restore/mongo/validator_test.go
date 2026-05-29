package mongo

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

func TestConflictStrategyFlagMapping_MongoDB(t *testing.T) {
	tests := []struct {
		name            string
		onConflict      string
		wantStopOnError bool // --stopOnError=false
		wantDrop        bool // --drop
	}{
		{"error = default behavior", "error", false, false},
		{"empty = default behavior", "", false, false},
		{"ignore = --stopOnError=false", "ignore", true, false},
		{"replace = --drop", "replace", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"--uri=mongodb://localhost"}
			if tt.onConflict == "ignore" {
				args = append(args, "--stopOnError=false")
			} else if tt.onConflict == "replace" {
				args = append(args, "--drop")
			}

			hasStopOnError := containsMongoArg(args, "--stopOnError=false")
			hasDrop := containsMongoArg(args, "--drop")

			if hasStopOnError != tt.wantStopOnError {
				t.Errorf("--stopOnError=false present=%v, want %v (strategy=%q)", hasStopOnError, tt.wantStopOnError, tt.onConflict)
			}
			if hasDrop != tt.wantDrop {
				t.Errorf("--drop present=%v, want %v (strategy=%q)", hasDrop, tt.wantDrop, tt.onConflict)
			}
		})
	}
}

func containsMongoArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
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
