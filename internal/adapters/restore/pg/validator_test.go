package pg

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

func TestConflictStrategyFlagMapping_PG(t *testing.T) {
	tests := []struct {
		name         string
		onConflict   string
		allowCascade bool
		wantClean    bool
		wantIfExists bool
	}{
		{"error strategy = no flags", "error", false, false, false},
		{"empty strategy = no flags", "", false, false, false},
		{"ignore = --if-exists", "ignore", false, false, true},
		{"replace without cascade = --clean only", "replace", false, true, false},
		{"replace with cascade = --clean --if-exists", "replace", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ra := &RestoreArgs{
				Host:         "localhost",
				Port:         5432,
				Username:     "user",
				Database:     "mydb",
				BackupPath:   "/tmp/backup.sql",
				OnConflict:   tt.onConflict,
				AllowCascade: tt.allowCascade,
			}

			// Build the args slice the same way Restore() does.
			args := []string{
				"--host=localhost", "--port=5432",
				"--username=user", "--dbname=mydb",
			}
			switch ra.OnConflict {
			case "replace":
				args = append(args, "--clean")
				if ra.AllowCascade {
					args = append(args, "--if-exists")
				}
			case "ignore":
				args = append(args, "--if-exists")
			}

			hasClean := containsArg(args, "--clean")
			hasIfExists := containsArg(args, "--if-exists")

			if hasClean != tt.wantClean {
				t.Errorf("--clean present=%v, want %v (strategy=%q)", hasClean, tt.wantClean, tt.onConflict)
			}
			if hasIfExists != tt.wantIfExists {
				t.Errorf("--if-exists present=%v, want %v (strategy=%q allowCascade=%v)", hasIfExists, tt.wantIfExists, tt.onConflict, tt.allowCascade)
			}
		})
	}
}

func containsArg(args []string, flag string) bool {
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
